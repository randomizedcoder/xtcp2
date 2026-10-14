package linkmonitor

import (
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeRDMAFiles reads real files and counts every open, read and close by
// group/file. It injects errors a temporary directory cannot produce: per
// file on open or read, and fail for any file holding rdmaReadError, as the
// fleet port answered.
type fakeRDMAFiles struct {
	mu                   sync.Mutex
	paths                map[int]string
	opens, reads, closes map[string]int
	openErr, readErr     map[string]error
	fail                 error
	// onOpen and onRead run before each open and read, outside the lock.
	onOpen, onRead func(key string)
	log            []string
}

func newFakeRDMAFiles() *fakeRDMAFiles {
	return &fakeRDMAFiles{paths: map[int]string{}, opens: map[string]int{}, reads: map[string]int{},
		closes: map[string]int{}, openErr: map[string]error{}, readErr: map[string]error{}}
}

func rdmaFileKey(path string) string {
	return filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
}

func (f *fakeRDMAFiles) open(path string) (int, error) {
	key := rdmaFileKey(path)
	if f.onOpen != nil {
		f.onOpen(key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens[key]++
	f.log = append(f.log, "open "+key)
	if err := f.openErr[key]; err != nil {
		return -1, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	fd, err := sysfsFiles{}.open(path)
	if err == nil {
		f.paths[fd] = path
	}
	return fd, err
}

func (f *fakeRDMAFiles) pread(fd int, buf []byte) (int, error) {
	f.mu.Lock()
	key := rdmaFileKey(f.paths[fd])
	f.reads[key]++
	f.log = append(f.log, "read "+key)
	err, hook := f.readErr[key], f.onRead
	f.mu.Unlock()
	if hook != nil {
		hook(key)
	}
	if err != nil {
		return 0, err
	}
	n, err := sysfsFiles{}.pread(fd, buf)
	if err == nil && f.fail != nil && strings.TrimSpace(string(buf[:n])) == rdmaReadError {
		return 0, f.fail
	}
	return n, err
}

func (f *fakeRDMAFiles) close(fd int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := rdmaFileKey(f.paths[fd])
	f.closes[key]++
	f.log = append(f.log, "close "+key)
	delete(f.paths, fd)
	return sysfsFiles{}.close(fd)
}

// held is the number of descriptors open now.
func (f *fakeRDMAFiles) held() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

// count totals counts over the keys with prefix, such as "counters".
func (f *fakeRDMAFiles) count(counts map[string]int, prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for key, v := range counts {
		if strings.HasPrefix(key, prefix) {
			n += v
		}
	}
	return n
}

// sequence is the logged operations of one kind ("open", "read" or
// "close") on keys with prefix, in order.
func (f *fakeRDMAFiles) sequence(op, prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, entry := range f.log {
		if key, ok := strings.CutPrefix(entry, op+" "); ok && strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out
}

func (f *fakeRDMAFiles) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.opens)
	clear(f.reads)
	clear(f.closes)
	f.log = f.log[:0]
}

// useRDMAFiles gives c private descriptors read through files; the test's
// cleanup checks every one is closed with the collector.
func useRDMAFiles(t testing.TB, c *rdmaOptionalCollector, files *fakeRDMAFiles) {
	t.Helper()
	if err := c.handles.release(); err != nil {
		t.Fatal(err)
	}
	c.handles = newRDMAHandles(files).retain()
	t.Cleanup(func() {
		if err := c.handles.release(); err != nil {
			t.Error(err)
		}
		if n := files.held(); n != 0 {
			t.Errorf("%d descriptors left open", n)
		}
	})
}
