// Command ipfeed-collector fetches cloud/CDN/SaaS IP-range feeds defined as
// one YAML file per source, normalizes them into a single schema, writes a
// timestamped Parquet file, and uploads it to S3. It emits OpenTelemetry
// metrics/traces, structured slog logs, and an end-of-run summary reporting
// files and records processed with positive/negative boundaries.
//
// It runs in two modes: single-shot (the default — one collection cycle, then
// exit) and daemon (-daemon — run immediately, then repeat every -interval,
// serving /healthz and /readyz when -http-addr is set). Every flag also reads
// an IPFEED_* environment variable when the flag is not given, so the daemon
// is easy to configure from a systemd unit or container.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/asnmap"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/combine"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/config"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/fetch"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/health"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/output"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/parse"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/s3"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/summary"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/telemetry"
)

type flags struct {
	sourcesDir  string
	outDir      string
	outFile     string
	concurrency int
	timeout     time.Duration
	maxAttempts int
	backoffBase time.Duration
	backoffCap  time.Duration
	minSuccess  int
	noUpload    bool
	verbose     bool
	debug       bool

	daemon      bool
	interval    time.Duration
	httpAddr    string
	healthcheck bool
	version     bool

	s3              s3.Config
	s3SecretKeyFile string
}

// env fallback helpers: an unset flag defaults to its IPFEED_* env var when
// present, else the built-in default. An invalid env value falls back to the
// built-in default rather than failing. A flag given on the command line
// always overrides both (that is how flag defaults work).
func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func parseFlags(args []string) (flags, error) {
	var f flags
	fs := flag.NewFlagSet("ipfeed-collector", flag.ContinueOnError)
	fs.StringVar(&f.sourcesDir, "sources-dir", envStr("IPFEED_SOURCES_DIR", "./sources"), "directory of per-source YAML files")
	fs.StringVar(&f.outDir, "out-dir", envStr("IPFEED_OUT_DIR", os.TempDir()), "local directory for the Parquet file (ignored when -out-file is set)")
	fs.StringVar(&f.outFile, "out-file", envStr("IPFEED_OUT_FILE", ""), "exact local path for the Parquet file; overrides -out-dir and its timestamped name")
	fs.IntVar(&f.concurrency, "concurrency", envInt("IPFEED_CONCURRENCY", 8), "max concurrent source fetches")
	fs.DurationVar(&f.timeout, "timeout", envDur("IPFEED_TIMEOUT", 30*time.Second), "per-request HTTP timeout")
	fs.IntVar(&f.maxAttempts, "max-attempts", envInt("IPFEED_MAX_ATTEMPTS", 10), "max fetch attempts per source")
	fs.DurationVar(&f.backoffBase, "backoff-base", envDur("IPFEED_BACKOFF_BASE", time.Second), "base backoff window")
	fs.DurationVar(&f.backoffCap, "backoff-cap", envDur("IPFEED_BACKOFF_CAP", time.Hour), "max backoff window")
	fs.IntVar(&f.minSuccess, "min-successful-sources", envInt("IPFEED_MIN_SUCCESSFUL_SOURCES", 1), "minimum OK sources required to write/upload")
	fs.BoolVar(&f.noUpload, "no-upload", envBool("IPFEED_NO_UPLOAD", false), "write Parquet locally but skip S3 upload")
	fs.BoolVar(&f.verbose, "v", envBool("IPFEED_VERBOSE", false), "verbose (debug) logging")
	fs.BoolVar(&f.debug, "debug", envBool("IPFEED_DEBUG", false), "debug logging (alias of -v)")

	fs.BoolVar(&f.daemon, "daemon", envBool("IPFEED_DAEMON", false), "run continuously, repeating every -interval")
	fs.DurationVar(&f.interval, "interval", envDur("IPFEED_INTERVAL", 6*time.Hour), "daemon collection interval")
	fs.StringVar(&f.httpAddr, "http-addr", envStr("IPFEED_HTTP_ADDR", ""), "daemon health endpoint address, e.g. :8080 (empty disables)")
	fs.BoolVar(&f.healthcheck, "healthcheck", envBool("IPFEED_HEALTHCHECK", false), "probe a running daemon's /readyz and exit 0 (ready) or 1; used as the container HEALTHCHECK")
	fs.BoolVar(&f.version, "version", false, "print build version and exit")

	fs.StringVar(&f.s3.Endpoint, "s3-endpoint", envStr("IPFEED_S3_ENDPOINT", ""), "S3 endpoint (may include scheme)")
	fs.StringVar(&f.s3.Bucket, "s3-bucket", envStr("IPFEED_S3_BUCKET", ""), "S3 bucket")
	// Credentials/region fall back to the standard AWS_* names after the
	// IPFEED_S3_* ones, so an AWS SDK/CLI environment works out of the box.
	// Precedence: flag > IPFEED_S3_* > AWS_* > built-in default.
	fs.StringVar(&f.s3.Region, "s3-region", envStr("IPFEED_S3_REGION", envStr("AWS_REGION", "us-east-1")), "S3 region")
	fs.StringVar(&f.s3.Prefix, "s3-prefix", envStr("IPFEED_S3_PREFIX", ""), "S3 key prefix")
	fs.StringVar(&f.s3.AccessKey, "s3-access-key", envStr("IPFEED_S3_ACCESS_KEY", envStr("AWS_ACCESS_KEY_ID", "")), "S3 access key")
	fs.StringVar(&f.s3.SecretKey, "s3-secret-key", envStr("IPFEED_S3_SECRET_KEY", envStr("AWS_SECRET_ACCESS_KEY", "")), "S3 secret key (prefer -s3-secret-key-file)")
	fs.StringVar(&f.s3SecretKeyFile, "s3-secret-key-file", envStr("IPFEED_S3_SECRET_KEY_FILE", ""), "path to a file holding the S3 secret key")
	fs.BoolVar(&f.s3.SkipBucketProbe, "s3-skip-bucket-probe", envBool("IPFEED_S3_SKIP_BUCKET_PROBE", false), "skip the BucketExists probe")

	if err := fs.Parse(args); err != nil {
		return flags{}, err
	}
	if f.daemon && f.interval <= 0 {
		return flags{}, fmt.Errorf("-interval must be > 0 in daemon mode")
	}
	return f, nil
}

// readyzURL turns a health-server bind address into the loopback /readyz URL a
// self-probe should hit. An empty addr defaults to :8080 (the image's exposed
// port convention); a wildcard/empty host becomes 127.0.0.1 since the probe
// runs inside the same container.
func readyzURL(addr string) string {
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Not host:port (e.g. a bare port) — treat the whole thing as the port.
		host, port = "", addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz"
}

// healthcheck issues a GET to url and returns nil iff the response status is
// 200. Any transport error, timeout, or non-200 status is an error. It is a
// standalone function (URL in, error out) so it is unit-testable with httptest.
func healthcheck(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: status %d", resp.StatusCode)
	}
	return nil
}

// Build metadata, injected via -ldflags "-X main.commit=... -X main.date=...
// -X main.version=..." by the Nix mkGoBinary derivation.
var (
	commit  string
	date    string
	version string
)

func main() {
	f, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	if f.version {
		fmt.Printf("ipfeed-collector version=%s commit=%s date=%s\n", version, commit, date)
		os.Exit(0)
	}

	// Healthcheck mode: probe a running daemon's /readyz and exit, without
	// starting telemetry or a collection cycle. This is the container
	// HEALTHCHECK entrypoint, so a scratch image needs no shell or curl.
	if f.healthcheck {
		if err := healthcheck(readyzURL(f.httpAddr), 5*time.Second); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	level := slog.LevelInfo
	if f.verbose || f.debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(context.Background(), f, log); err != nil {
		log.Error("run failed", "err", err)
		os.Exit(1)
	}
}

// deps bundles the long-lived collaborators shared across collection cycles.
type deps struct {
	f      flags
	tel    *telemetry.Telemetry
	client *fetch.Client
	log    *slog.Logger
}

// sourceOutcome bundles a source's valid records with its summary row.
type sourceOutcome struct {
	valid  []model.Record
	result summary.SourceResult
}

func run(rootCtx context.Context, f flags, log *slog.Logger) error {
	tel, err := telemetry.Setup(rootCtx, "ipfeed-collector")
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() { //nolint:contextcheck // shutdown must not inherit the already-canceled rootCtx
		// Shutdown must not inherit rootCtx, which is typically already canceled
		// by the time this defer runs.
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tel.Shutdown(sctx); err != nil {
			log.Warn("telemetry shutdown", "err", err)
		}
	}()

	client := fetch.NewClient(fetch.Options{
		MaxAttempts: f.maxAttempts,
		BackoffBase: f.backoffBase,
		BackoffCap:  f.backoffCap,
		Timeout:     f.timeout,
	})
	d := deps{f: f, tel: tel, client: client, log: log}

	// Cancel work cleanly on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(rootCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One cycle, wrapped to record the cycle-outcome metric. Used by both modes.
	collect := func(ctx context.Context) error {
		err := collectOnce(ctx, d)
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		tel.Cycles.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
		return err
	}

	if !f.daemon {
		return collect(ctx)
	}

	// Daemon: optional health endpoint, then loop.
	var ready func()
	if f.httpAddr != "" {
		hs := health.NewServer(f.httpAddr)
		if err := hs.Start(ctx); err != nil {
			return fmt.Errorf("health server: %w", err)
		}
		log.Info("health server listening", "addr", f.httpAddr)
		defer func() { //nolint:contextcheck // shutdown must not inherit the already-canceled daemon ctx
			// Fresh ctx: the daemon ctx is already canceled during shutdown.
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := hs.Shutdown(sctx); err != nil {
				log.Warn("health server shutdown", "err", err)
			}
		}()
		ready = hs.SetReady
	}
	log.Info("daemon started", "interval", f.interval)
	return runDaemon(ctx, f.interval, ready, collect, log)
}

// runDaemon runs collect immediately, then on every interval tick, until ctx
// is canceled. A failed cycle is logged but does not stop the loop; ready is
// invoked after each successful cycle (nil ready is a no-op). It is kept free
// of telemetry/HTTP so it is straightforward to test with fakes.
func runDaemon(ctx context.Context, interval time.Duration, ready func(), collect func(context.Context) error, log *slog.Logger) error {
	runCycle := func() {
		if err := collect(ctx); err != nil {
			log.Error("collection cycle failed", "err", err)
			return
		}
		if ready != nil {
			ready()
		}
	}

	runCycle()
	if ctx.Err() != nil {
		return nil
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("daemon stopping")
			return nil
		case <-t.C:
			// select may pick a ready tick even when ctx is already done
			// (Go makes no priority guarantee between ready cases), so
			// re-check before starting another cycle.
			if ctx.Err() != nil {
				log.Info("daemon stopping")
				return nil
			}
			runCycle()
		}
	}
}

// collectOnce performs one full collection cycle: (re)load sources, fetch and
// parse them concurrently, validate, write the Parquet file, and upload it.
// Sources are reloaded each call so files added/removed under -sources-dir are
// picked up without restarting the daemon.
func collectOnce(ctx context.Context, d deps) error {
	f, tel, client, log := d.f, d.tel, d.client, d.log

	sources, err := config.LoadDir(f.sourcesDir)
	if err != nil {
		return fmt.Errorf("load sources: %w", err)
	}
	if len(sources) == 0 {
		return fmt.Errorf("no enabled sources found in %s", f.sourcesDir)
	}
	log.Info("loaded sources", "count", len(sources), "dir", f.sourcesDir)

	ctx, span := tel.Tracer.Start(ctx, "cycle")
	defer span.End()

	// Fan out over sources with a bounded worker pool.
	outcomes := make([]sourceOutcome, len(sources))
	sem := make(chan struct{}, max(1, f.concurrency))
	var wg sync.WaitGroup
	for i := range sources {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			outcomes[i] = processSource(ctx, client, tel, log, sources[i])
		})
	}
	wg.Wait()

	// Aggregate.
	var sum summary.Summary
	var combined []model.Record
	for _, o := range outcomes {
		sum.Add(o.result)
		combined = append(combined, o.valid...)
	}
	tel.SourcesSucceeded.Record(ctx, int64(sum.OKCount()))

	// Enrich each record with a representative ASN derived from its owner /
	// provider, so the artifact carries prefix -> {asn, network_owner, …} for
	// downstream IP->ASN lookups (see internal/ipfeed/asnmap).
	asnmap.Annotate(combined)

	if sum.OKCount() < f.minSuccess {
		sum.Print(os.Stdout, "", 0)
		return fmt.Errorf("only %d/%d sources succeeded (min %d); not writing dataset",
			sum.OKCount(), len(sources), f.minSuccess)
	}

	// Stable ordering for reproducible output.
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].Prefix != combined[j].Prefix {
			return combined[i].Prefix < combined[j].Prefix
		}
		return combined[i].SourceName < combined[j].SourceName
	})

	// -out-file names the exact output path; otherwise write a timestamped file
	// into -out-dir. The basename is reused as the S3 object name on upload.
	now := time.Now().UTC()
	filename := output.Filename(now)
	outPath := filepath.Join(f.outDir, filename)
	if f.outFile != "" {
		outPath = f.outFile
		filename = filepath.Base(f.outFile)
	}
	size, err := output.WriteParquet(outPath, combined)
	if err != nil {
		return fmt.Errorf("write parquet: %w", err)
	}
	log.Info("wrote parquet", "path", outPath, "records", len(combined), "bytes", size)

	uploadURL := ""
	if !f.noUpload {
		uploadURL, err = uploadResult(ctx, f, tel, log, outPath, filename, size)
		if err != nil {
			return fmt.Errorf("upload: %w", err)
		}
	}

	sum.Print(os.Stdout, uploadURL, size)
	return nil
}

// processSource discovers, fetches, parses, and validates one source.
func processSource(ctx context.Context, client *fetch.Client, tel *telemetry.Telemetry, log *slog.Logger, src config.Source) sourceOutcome {
	attrs := metric.WithAttributes(
		attribute.String("source", src.Name),
		attribute.String("provider", src.Provider),
	)
	ctx, span := tel.Tracer.Start(ctx, "source", trace.WithAttributes(
		attribute.String("source", src.Name)))
	defer span.End()

	res := summary.SourceResult{Name: src.Name}
	start := time.Now()

	url, err := client.Discover(ctx, src.Discover, src.URL)
	if err != nil {
		tel.FetchFailures.Add(ctx, 1, attrs)
		res.Note = "discover: " + err.Error()
		res.Duration = time.Since(start)
		log.Error("discover failed", "source", src.Name, "err", err)
		return sourceOutcome{result: res}
	}

	fr, err := client.Get(ctx, url, fetch.Conditional{})
	res.HTTPStatus = fr.Status
	tel.FetchAttempts.Add(ctx, int64(fr.Attempts), attrs)
	res.FetchedBytes = int64(len(fr.Body))
	tel.FetchBytes.Add(ctx, res.FetchedBytes, attrs)
	tel.FetchDuration.Record(ctx, time.Since(start).Seconds(), attrs)
	if err != nil {
		tel.FetchFailures.Add(ctx, 1, attrs)
		res.Note = err.Error()
		res.Duration = time.Since(start)
		log.Error("fetch failed", "source", src.Name, "url", url, "err", err)
		return sourceOutcome{result: res}
	}

	parser, ok := parse.Get(src.Parser)
	if !ok { // validated at load, but guard anyway
		res.Note = "unknown parser " + src.Parser
		res.Duration = time.Since(start)
		return sourceOutcome{result: res}
	}
	retrievedAt := time.Now().UTC().Format(time.RFC3339)
	pstart := time.Now()
	records, err := parser.Parse(fr.Body, src.Meta(), retrievedAt)
	tel.ParseDuration.Record(ctx, time.Since(pstart).Seconds(), attrs)
	if err != nil {
		res.Note = "parse: " + err.Error()
		res.Duration = time.Since(start)
		log.Error("parse failed", "source", src.Name, "err", err)
		return sourceOutcome{result: res}
	}
	res.Parsed = len(records)

	seen := make(map[string]struct{}, len(records))
	cr := combine.Validate(records, seen)
	res.Valid = cr.ValidCount()
	res.Rejected = cr.RejectedCount()
	tel.RecordsValid.Add(ctx, int64(res.Valid), attrs)
	tel.RecordsInvalid.Add(ctx, int64(res.Rejected), attrs)

	// Per the update strategy: an empty/invalid response must not be treated
	// as success.
	if res.Valid == 0 {
		res.Note = "no valid records"
		res.Duration = time.Since(start)
		log.Warn("source produced no valid records", "source", src.Name)
		return sourceOutcome{result: res}
	}

	res.OK = true
	res.Duration = time.Since(start)
	log.Info("source ok", "source", src.Name, "valid", res.Valid, "rejected", res.Rejected,
		"http", res.HTTPStatus, "bytes", res.FetchedBytes)
	return sourceOutcome{valid: cr.Valid, result: res}
}

// uploadResult builds the S3 client and uploads the Parquet file.
func uploadResult(ctx context.Context, f flags, tel *telemetry.Telemetry, log *slog.Logger, path, filename string, size int64) (string, error) {
	cfg := f.s3
	secret, err := s3.SecretFromFile(f.s3SecretKeyFile)
	if err != nil {
		return "", err
	}
	if secret != "" {
		cfg.SecretKey = secret
	}
	up, err := s3.New(ctx, cfg)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	key := cfg.Key(filename)
	ctx, span := tel.Tracer.Start(ctx, "upload", trace.WithAttributes(attribute.String("key", key)))
	defer span.End()

	url, err := up.Put(ctx, key, file, size)
	if err != nil {
		return "", err
	}
	tel.UploadBytes.Add(ctx, size)
	log.Info("uploaded", "url", url, "bytes", size)
	return url, nil
}
