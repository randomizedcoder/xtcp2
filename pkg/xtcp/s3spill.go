//go:build dest_s3parquet

package xtcp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// s3SpillMaxBytesDefaultCst is the spill directory's byte bound when
// s3_spill_max_bytes is unset (0).
const s3SpillMaxBytesDefaultCst = 1 << 30

// Spill file layout: magic, u32 key length, key, u64 rows, Parquet object.
// Integers are big-endian. Names are <seq>.xsp; a write goes to <seq>.tmp
// first and is renamed once synced, so a crash leaves only a .tmp, which
// openS3Spill deletes.
const (
	s3SpillMagic     = "XTCPSPL1"
	s3SpillExt       = ".xsp"
	s3SpillTmpExt    = ".tmp"
	s3SpillMaxKeyCst = 1024
	s3SpillSeqDigits = 20
)

// Results, the result label of xtcp_s3_spill_total.
const (
	s3SpillWritten  = "written"   // object written to the spill dir
	s3SpillUploaded = "uploaded"  // spilled object uploaded and deleted
	s3SpillEvicted  = "evicted"   // oldest object deleted to make room
	s3SpillTooLarge = "too_large" // object over s3_spill_max_bytes, dropped
	s3SpillError    = "error"     // write failed, or a corrupt file deleted
)

var errSpillTooLarge = errors.New("s3 spill: object larger than s3_spill_max_bytes")

// s3SpillFile is one spilled object on disk.
type s3SpillFile struct {
	seq  uint64
	size int64
}

// s3Spill keeps Parquet objects that failed to upload in a directory,
// oldest first, within maxBytes. All file access goes through an os.Root,
// so names can't escape the directory. Safe for concurrent use: the worker
// writes, the re-uploader reads and removes.
type s3Spill struct {
	root     *os.Root
	dir      string
	maxBytes int64
	metrics  *s3SpillMetrics

	mu    sync.Mutex
	files []s3SpillFile // ascending seq
	bytes int64
	next  uint64
}

// openS3Spill creates dir (0700) if needed, deletes leftover .tmp files and
// indexes the spilled objects already there, evicting the oldest past
// maxBytes. maxBytes <= 0 means s3SpillMaxBytesDefaultCst.
func openS3Spill(dir string, maxBytes int64, metrics *s3SpillMetrics) (*s3Spill, error) {
	if maxBytes <= 0 {
		maxBytes = s3SpillMaxBytesDefaultCst
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("s3 spill dir: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("s3 spill dir: %w", err)
	}
	s := &s3Spill{root: root, dir: dir, maxBytes: maxBytes, metrics: metrics, next: 1}
	if err := s.scan(); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	s.mu.Lock()
	s.evictLocked(0)
	s.mu.Unlock()
	s.observe()
	return s, nil
}

// scan indexes <seq>.xsp regular files and deletes <seq>.tmp leftovers.
// Anything else in the directory is left alone.
func (s *s3Spill) scan() error {
	d, err := s.root.Open(".")
	if err != nil {
		return fmt.Errorf("s3 spill dir: %w", err)
	}
	entries, err := d.ReadDir(-1)
	if err = errors.Join(err, d.Close()); err != nil {
		return fmt.Errorf("s3 spill dir: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() {
			continue
		}
		if _, ok := parseSpillName(name, s3SpillTmpExt); ok {
			s.removeFile(name)
			continue
		}
		seq, ok := parseSpillName(name, s3SpillExt)
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		s.files = append(s.files, s3SpillFile{seq: seq, size: info.Size()})
		s.bytes += info.Size()
		s.next = max(s.next, seq+1)
	}
	sort.Slice(s.files, func(i, j int) bool { return s.files[i].seq < s.files[j].seq })
	return nil
}

// parseSpillName returns the sequence number of a <seq><ext> name.
func parseSpillName(name, ext string) (uint64, bool) {
	digits, ok := strings.CutSuffix(name, ext)
	if !ok || len(digits) != s3SpillSeqDigits {
		return 0, false
	}
	seq, err := strconv.ParseUint(digits, 10, 64)
	return seq, err == nil
}

func spillName(seq uint64, ext string) string {
	return fmt.Sprintf("%0*d%s", s3SpillSeqDigits, seq, ext)
}

// spillHeader encodes the header that precedes the object.
func spillHeader(key string, rows int) []byte {
	h := make([]byte, 0, len(s3SpillMagic)+4+len(key)+8)
	h = append(h, s3SpillMagic...)
	h = binary.BigEndian.AppendUint32(h, uint32(len(key)))
	h = append(h, key...)
	return binary.BigEndian.AppendUint64(h, uint64(max(rows, 0)))
}

// write spills one object, evicting the oldest objects to make room. An
// object larger than maxBytes on its own is refused (errSpillTooLarge).
func (s *s3Spill) write(key string, object []byte, rows int) error {
	if key == "" || len(key) > s3SpillMaxKeyCst {
		s.count(s3SpillError)
		return fmt.Errorf("s3 spill: key length %d", len(key))
	}
	header := spillHeader(key, rows)
	size := int64(len(header) + len(object))
	if size > s.maxBytes {
		s.count(s3SpillTooLarge)
		return errSpillTooLarge
	}
	s.mu.Lock()
	seq := s.next
	s.next++
	s.mu.Unlock()

	tmp, final := spillName(seq, s3SpillTmpExt), spillName(seq, s3SpillExt)
	if err := s.writeFile(tmp, header, object); err != nil {
		s.removeFile(tmp)
		s.count(s3SpillError)
		return err
	}

	s.mu.Lock()
	s.evictLocked(size)
	err := s.root.Rename(tmp, final)
	if err == nil {
		s.files = append(s.files, s3SpillFile{seq: seq, size: size})
		s.bytes += size
	}
	s.mu.Unlock()
	if err != nil {
		s.removeFile(tmp)
		s.count(s3SpillError)
		return fmt.Errorf("s3 spill: %w", err)
	}
	s.count(s3SpillWritten)
	s.observe()
	return nil
}

// writeFile creates name (0600, never an existing file), writes header and
// object and syncs it.
func (s *s3Spill) writeFile(name string, header, object []byte) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("s3 spill: %w", err)
	}
	_, err = f.Write(header)
	if err == nil {
		_, err = f.Write(object)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("s3 spill: %w", err)
	}
	return nil
}

// evictLocked deletes the oldest objects until incoming more bytes fit.
func (s *s3Spill) evictLocked(incoming int64) {
	for len(s.files) > 0 && s.bytes+incoming > s.maxBytes {
		f := s.files[0]
		s.files = s.files[1:]
		s.bytes -= f.size
		s.removeFile(spillName(f.seq, s3SpillExt))
		s.count(s3SpillEvicted)
	}
}

// s3SpilledObject is an open spilled object: Body reads the Parquet bytes.
type s3SpilledObject struct {
	seq  uint64
	Key  string
	Rows int
	Size int64
	Body io.ReadSeeker
	f    *os.File
}

// Close closes the underlying file.
func (o *s3SpilledObject) Close() error { return o.f.Close() }

// oldest opens the oldest spilled object. ok is false when the spill is
// empty. A file that can't be read or has a bad header is deleted, counted
// as an error and skipped.
func (s *s3Spill) oldest() (*s3SpilledObject, bool) {
	for {
		s.mu.Lock()
		if len(s.files) == 0 {
			s.mu.Unlock()
			return nil, false
		}
		f := s.files[0]
		s.mu.Unlock()

		obj, err := s.open(f)
		if err == nil {
			return obj, true
		}
		// Gone already (evicted while we looked) is not an error.
		if !errors.Is(err, fs.ErrNotExist) {
			s.count(s3SpillError)
		}
		s.remove(f.seq)
	}
}

// open reads and checks the header of one spilled object.
func (s *s3Spill) open(sf s3SpillFile) (*s3SpilledObject, error) {
	f, err := s.root.OpenFile(spillName(sf.seq, s3SpillExt), os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	obj, err := readSpillHeader(f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	obj.seq = sf.seq
	obj.f = f
	return obj, nil
}

// readSpillHeader parses the header at the start of f and returns the
// object with Body positioned over the Parquet bytes that follow.
func readSpillHeader(f *os.File) (*s3SpilledObject, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var fixed [len(s3SpillMagic) + 4]byte
	if _, err := io.ReadFull(f, fixed[:]); err != nil {
		return nil, fmt.Errorf("s3 spill header: %w", err)
	}
	if string(fixed[:len(s3SpillMagic)]) != s3SpillMagic {
		return nil, errors.New("s3 spill header: bad magic")
	}
	keyLen := int64(binary.BigEndian.Uint32(fixed[len(s3SpillMagic):]))
	if keyLen == 0 || keyLen > s3SpillMaxKeyCst {
		return nil, fmt.Errorf("s3 spill header: key length %d", keyLen)
	}
	rest := make([]byte, keyLen+8)
	if _, err := io.ReadFull(f, rest); err != nil {
		return nil, fmt.Errorf("s3 spill header: %w", err)
	}
	key := string(rest[:keyLen])
	if !utf8.ValidString(key) || strings.ContainsFunc(key, isSpillKeyControl) {
		return nil, errors.New("s3 spill header: bad key")
	}
	rows := binary.BigEndian.Uint64(rest[keyLen:])
	off := int64(len(fixed)) + keyLen + 8
	size := info.Size() - off
	if size <= 0 {
		return nil, errors.New("s3 spill: empty object")
	}
	return &s3SpilledObject{
		Key:  key,
		Rows: int(min(rows, uint64(1<<31))),
		Size: size,
		Body: io.NewSectionReader(f, off, size),
	}, nil
}

func isSpillKeyControl(r rune) bool { return r < 0x20 || r == 0x7f }

// remove deletes the spilled object seq (after a successful upload).
func (s *s3Spill) remove(seq uint64) {
	s.mu.Lock()
	for i, f := range s.files {
		if f.seq == seq {
			s.files = append(s.files[:i], s.files[i+1:]...)
			s.bytes -= f.size
			break
		}
	}
	s.mu.Unlock()
	s.removeFile(spillName(seq, s3SpillExt))
	s.observe()
}

// removeFile deletes name from the spill dir. Already gone is fine; any
// other failure is logged and counted.
func (s *s3Spill) removeFile(name string) {
	if err := s.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("destS3Parquet spill: %v", err)
		s.count(s3SpillError)
	}
}

// stats returns the spilled object count and bytes.
func (s *s3Spill) stats() (objects int, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.files), s.bytes
}

// close releases the directory handle; spilled objects stay for the next
// start.
func (s *s3Spill) close() error { return s.root.Close() }

func (s *s3Spill) count(result string) {
	if s.metrics != nil {
		s.metrics.total.WithLabelValues(result).Inc()
	}
}

func (s *s3Spill) observe() {
	if s.metrics == nil {
		return
	}
	n, b := s.stats()
	s.metrics.objects.Set(float64(n))
	s.metrics.bytes.Set(float64(b))
}

// s3SpillMetrics are the spill directory's counters and gauges.
type s3SpillMetrics struct {
	total   *prometheus.CounterVec
	objects prometheus.Gauge
	bytes   prometheus.Gauge
}

func newS3SpillMetrics(reg prometheus.Registerer) *s3SpillMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)
	return &s3SpillMetrics{
		total: f.NewCounterVec(prometheus.CounterOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "s3_spill_total",
			Help:      "Parquet objects through the s3parquet spill directory (-s3SpillDir): result=written, uploaded, evicted (oldest deleted past -s3SpillMaxBytes), too_large, error.",
		}, []string{"result"}),
		objects: f.NewGauge(prometheus.GaugeOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "s3_spill_objects",
			Help:      "Parquet objects waiting in the s3parquet spill directory.",
		}),
		bytes: f.NewGauge(prometheus.GaugeOpts{
			Subsystem: promSubsystemXTCP,
			Name:      "s3_spill_bytes",
			Help:      "Bytes in the s3parquet spill directory.",
		}),
	}
}

// s3SpillRetryCapMinCst floors the reupload backoff cap, so a fleet whose
// derived s3_upload_backoff_cap is 1 s doesn't probe a dead S3 every second
// per host. A successful worker upload still wakes reupload at once.
const s3SpillRetryCapMinCst = 30 * time.Second

// startSpill attaches spill and starts the reupload goroutine. Objects left
// by an earlier run get an immediate try.
func (d *s3ParquetDest) startSpill(ctx context.Context, spill *s3Spill) {
	d.spill = spill
	d.spillKick = make(chan struct{}, 1)
	d.spillNew = make(chan struct{}, 1)
	d.spillDone = make(chan struct{})
	if n, _ := spill.stats(); n > 0 {
		d.signalSpill(d.spillKick)
	}
	go d.reupload(ctx)
}

// signalSpill does a non-blocking send on ch; a nil ch (no spill) is a no-op.
func (d *s3ParquetDest) signalSpill(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// reupload uploads spilled objects oldest first. spillKick (S3 just took an
// upload) starts a pass at once; spillNew (an object was spilled) arms the
// backoff timer if it isn't armed. After a failed pass it waits a
// full-jitter window that doubles up to max(backoffCap, 30s). It stops on
// Close or ctx; what's left stays on disk for the next start.
func (d *s3ParquetDest) reupload(ctx context.Context) {
	defer close(d.spillDone)
	capDur := max(d.backoffCap, s3SpillRetryCapMinCst)
	failures := 0
	var retry <-chan time.Time
	stop := func() bool { return false }
	defer func() { stop() }()
	arm := func() {
		retry, stop = d.newTimer(d.jitterDur(backoffWindow(s3UploadBackoffBaseCst, capDur, failures+1)))
	}
	for {
		select {
		case <-d.closedCh:
			return
		case <-ctx.Done():
			return
		case <-d.spillNew:
			if retry == nil {
				arm()
			}
			continue
		case <-d.spillKick:
		case <-retry:
		}
		stop()
		retry = nil
		if d.drainSpill(ctx) {
			failures = 0
			continue
		}
		failures++
		arm()
	}
}

// drainSpill uploads spilled objects until the spill is empty (true) or an
// upload fails (false).
func (d *s3ParquetDest) drainSpill(ctx context.Context) bool {
	for {
		select {
		case <-d.closedCh:
			return true
		default:
		}
		obj, ok := d.spill.oldest()
		if !ok {
			return true
		}
		uploadCtx, cancel := context.WithTimeout(ctx, s3SpillUploadTimeoutCst)
		uploaded := d.uploadWithRetry(uploadCtx, obj.Key, obj.Body, obj.Size, obj.Rows, 1)
		cancel()
		if err := obj.Close(); err != nil {
			log.Printf("destS3Parquet spill: %v", err)
		}
		if !uploaded {
			return false
		}
		d.spill.count(s3SpillUploaded)
		d.spill.remove(obj.seq)
	}
}

// s3SpillUploadTimeoutCst bounds one re-upload, like the worker's 60 s.
const s3SpillUploadTimeoutCst = 60 * time.Second
