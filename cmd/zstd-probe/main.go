// Command zstd-probe measures in-process zstd decompression.
//
// It exists to compare embedding zstd decoding in xtcp2 against shipping a
// /bin/zstd executable in OCI images. By default decompressed bytes are
// discarded, so the measurements focus on decoder CPU, memory, and throughput.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// fileLabelCst is the Prometheus label carrying the input file's base name. It
// is a constant because every metric below is partitioned by it, and goconst
// correctly objects to the same label name being spelled out seven times.
const fileLabelCst = "file"

type config struct {
	listen      string
	metricsPath string
	outDir      string
	repeat      int
	hold        time.Duration
	files       []string
}

type result struct {
	file              string
	compressedBytes   int64
	decompressedBytes int64
	duration          time.Duration
}

type probeMetrics struct {
	runs              *prometheus.CounterVec
	compressedBytes   *prometheus.CounterVec
	decompressedBytes *prometheus.CounterVec
	duration          *prometheus.HistogramVec
	lastCompressed    *prometheus.GaugeVec
	lastDecompressed  *prometheus.GaugeVec
	lastDuration      *prometheus.GaugeVec
}

func parseArgs(args []string) (config, error) {
	var c config
	fs := flag.NewFlagSet("zstd-probe", flag.ContinueOnError)
	fs.StringVar(&c.listen, "listen", "", "optional metrics listener address, e.g. :9099")
	fs.StringVar(&c.metricsPath, "metrics-path", "/metrics", "Prometheus metrics path")
	fs.StringVar(&c.outDir, "out-dir", "", "optional directory for decompressed output; empty discards bytes")
	fs.IntVar(&c.repeat, "repeat", 1, "number of decompression passes per input file")
	fs.DurationVar(&c.hold, "hold", 0, "after work completes, keep serving metrics for this duration")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if c.repeat < 1 {
		return config{}, fmt.Errorf("-repeat must be >= 1")
	}
	c.files = fs.Args()
	if len(c.files) == 0 {
		return config{}, fmt.Errorf("usage: zstd-probe [flags] <file.zst> [more.zst...]")
	}
	return c, nil
}

func newMetrics(reg *prometheus.Registry) *probeMetrics {
	m := &probeMetrics{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zstd_probe_decompress_total",
			Help: "Number of zstd decompression attempts.",
		}, []string{fileLabelCst, "status"}),
		compressedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zstd_probe_compressed_bytes_total",
			Help: "Compressed bytes read by zstd-probe.",
		}, []string{fileLabelCst}),
		decompressedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zstd_probe_decompressed_bytes_total",
			Help: "Decompressed bytes produced by zstd-probe.",
		}, []string{fileLabelCst}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "zstd_probe_decompress_duration_seconds",
			Help:    "Wall-clock duration of one zstd decompression attempt.",
			Buckets: prometheus.DefBuckets,
		}, []string{fileLabelCst, "status"}),
		lastCompressed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "zstd_probe_last_compressed_bytes",
			Help: "Compressed input bytes from the most recent successful attempt.",
		}, []string{fileLabelCst}),
		lastDecompressed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "zstd_probe_last_decompressed_bytes",
			Help: "Decompressed output bytes from the most recent successful attempt.",
		}, []string{fileLabelCst}),
		lastDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "zstd_probe_last_decompress_duration_seconds",
			Help: "Wall-clock duration of the most recent successful attempt.",
		}, []string{fileLabelCst}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.runs,
		m.compressedBytes,
		m.decompressedBytes,
		m.duration,
		m.lastCompressed,
		m.lastDecompressed,
		m.lastDuration,
	)
	return m
}

// startMetrics binds addr and serves the Prometheus registry plus a /readyz
// probe.
//
// ctx bounds the bind, but only as far as Go carries it: the net package
// consults ctx during name resolution and not around the bind syscall, so a
// canceled ctx with a hostname addr opens no listener while a literal IP:port
// still binds. TestStartMetrics pins both halves of that.
func startMetrics(ctx context.Context, addr, path string, reg *prometheus.Registry) (*http.Server, string, error) {
	if addr == "" {
		return nil, "", nil
	}
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, "", err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server error: %v", err)
		}
	}()
	return srv, ln.Addr().String(), nil
}

func decompressFile(path, outDir string) (result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return result{}, err
	}
	in, err := os.Open(path)
	if err != nil {
		return result{}, err
	}
	defer in.Close()

	decoder, err := zstd.NewReader(in)
	if err != nil {
		return result{}, err
	}
	defer decoder.Close()

	// out is io.Writer, not io.Discard's concrete type: io.Discard is declared
	// as `var Discard Writer`, so the inferred type is already the interface
	// and outFile can be assigned into it below without a conversion.
	out := io.Discard
	var outFile *os.File
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o750); err != nil {
			return result{}, err
		}
		name := strings.TrimSuffix(filepath.Base(path), ".zst")
		if name == filepath.Base(path) {
			name += ".out"
		}
		outFile, err = os.Create(filepath.Join(outDir, name))
		if err != nil {
			return result{}, err
		}
		defer outFile.Close()
		out = outFile
	}

	start := time.Now()
	n, err := io.Copy(out, decoder)
	if err != nil {
		return result{}, err
	}
	if outFile != nil {
		if err := outFile.Sync(); err != nil {
			return result{}, err
		}
	}
	return result{
		file:              path,
		compressedBytes:   info.Size(),
		decompressedBytes: n,
		duration:          time.Since(start),
	}, nil
}

func recordSuccess(m *probeMetrics, r result) {
	label := filepath.Base(r.file)
	seconds := r.duration.Seconds()
	m.runs.WithLabelValues(label, "ok").Inc()
	m.compressedBytes.WithLabelValues(label).Add(float64(r.compressedBytes))
	m.decompressedBytes.WithLabelValues(label).Add(float64(r.decompressedBytes))
	m.duration.WithLabelValues(label, "ok").Observe(seconds)
	m.lastCompressed.WithLabelValues(label).Set(float64(r.compressedBytes))
	m.lastDecompressed.WithLabelValues(label).Set(float64(r.decompressedBytes))
	m.lastDuration.WithLabelValues(label).Set(seconds)
}

func recordFailure(m *probeMetrics, file string, d time.Duration) {
	label := filepath.Base(file)
	m.runs.WithLabelValues(label, "error").Inc()
	m.duration.WithLabelValues(label, "error").Observe(d.Seconds())
}

func run(c config, m *probeMetrics) error {
	var failed bool
	for i := 0; i < c.repeat; i++ {
		for _, file := range c.files {
			start := time.Now()
			r, err := decompressFile(file, c.outDir)
			if err != nil {
				failed = true
				recordFailure(m, file, time.Since(start))
				log.Printf("decompress file=%s pass=%d error=%v", file, i+1, err)
				continue
			}
			recordSuccess(m, r)
			mb := float64(r.decompressedBytes) / 1024 / 1024
			rate := mb / r.duration.Seconds()
			fmt.Printf("%s pass=%d compressed=%d decompressed=%d duration=%s throughput=%.2f MiB/s\n",
				file, i+1, r.compressedBytes, r.decompressedBytes, r.duration, rate)
		}
	}
	if failed {
		return fmt.Errorf("one or more decompressions failed")
	}
	return nil
}

func main() {
	c, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx := context.Background()
	reg := prometheus.NewRegistry()
	metrics := newMetrics(reg)
	srv, actualAddr, err := startMetrics(ctx, c.listen, c.metricsPath, reg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "metrics listen:", err)
		os.Exit(1)
	}
	if actualAddr != "" {
		log.Printf("metrics listening on %s%s", actualAddr, c.metricsPath)
	}

	err = run(c, metrics)
	if c.hold > 0 {
		log.Printf("holding metrics endpoint for %s", c.hold)
		time.Sleep(c.hold)
	}
	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
