// Command xtcp2ctl is the operator control CLI for a running xtcp2 daemon.
//
// Where cmd/xtcp2client streams TCP stats out of the XTCPFlatRecordService,
// xtcp2ctl drives the ConfigService: it changes poll cadence, triggers polls,
// tunes the S3 upload timing, and applies an arbitrary new config via a
// soft restart — all without redeploying the container. It's a thin,
// dependency-light wrapper over the generated ConfigService client; anything
// it does is equally reachable with grpcurl (see docs/grpc-api.md).
//
// Usage:
//
//	xtcp2ctl <command> [flags]
//
// Commands:
//
//	get                  print the daemon's current config (S3 creds redacted)
//	set-poll-frequency   change the poll frequency + timeout live
//	trigger-poll         trigger a single poll now
//	poll-burst           trigger N polls spaced I apart (incident snapshots)
//	set-s3               change the s3parquet flush timer and/or byte cap live
//	reconfigure          apply a full config from a file/stdin (soft restart)
//
// Every command accepts -target/-port/-d. Run `xtcp2ctl <command> -h` for
// per-command flags.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	targetHostnameCst = "localhost"
	// grpcPortCst MUST match the xtcp2 daemon's default (cmd/xtcp2
	// grpcPortCst, currently 8889). Out-of-step ports turn every call into a
	// silent connection refused.
	grpcPortCst = "8889"

	// callTimeout bounds a single unary control RPC. reconfigure returns
	// before the daemon actually re-execs (the daemon delays its shutdown so
	// the response flushes), so this is comfortably long enough.
	callTimeout = 15 * time.Second
)

var (
	// Passed by "go build -ldflags" for -v.
	commit  string
	date    string
	version string
)

func main() {
	misc.DieIfNotLinux()
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain dispatches the subcommand. Extracted so tests can drive it with
// synthetic args + writers against a bufconn server.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "-v", "--version", "version":
		fmt.Fprintf(stdout, "xtcp2ctl commit:%s\tdate(UTC):%s\tversion:%s\n", commit, date, version)
		return 0
	case "-h", "-help", "--help", "help":
		usage(stdout)
		return 0
	case "get":
		return cmdGet(ctx, args[1:], stdout, stderr)
	case "set-poll-frequency":
		return cmdSetPollFrequency(ctx, args[1:], stdout, stderr)
	case "trigger-poll":
		return cmdTriggerPoll(ctx, args[1:], stdout, stderr)
	case "poll-burst":
		return cmdPollBurst(ctx, args[1:], stdout, stderr)
	case "set-s3":
		return cmdSetS3(ctx, args[1:], stdout, stderr)
	case "reconfigure":
		return cmdReconfigure(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "xtcp2ctl: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `xtcp2ctl — operator control CLI for a running xtcp2 daemon

Usage:
  xtcp2ctl <command> [flags]

Commands:
  get                  Print the daemon's current config (S3 creds redacted)
  set-poll-frequency   Change poll frequency + timeout live (no restart)
  trigger-poll         Trigger a single poll immediately (no restart)
  poll-burst           Trigger N polls spaced I apart, e.g. incident snapshots
  set-s3               Change the s3parquet flush timer / byte cap live (no restart)
  reconfigure          Apply a full config from a file or stdin (soft restart)

Common flags (all commands):
  -target string   daemon hostname (default "localhost")
  -port string     daemon gRPC port (default "8889", must match -grpcPort)
  -d uint          debug level (default 0)

Run 'xtcp2ctl <command> -h' for per-command flags.
`)
}

// commonFlags registers the flags every command shares and returns accessors.
func commonFlags(fs *flag.FlagSet) (target, port *string) {
	target = fs.String("target", targetHostnameCst, "daemon hostname")
	port = fs.String("port", grpcPortCst, "daemon gRPC port (must match the daemon's -grpcPort)")
	return target, port
}

// dialFunc is the connection factory, overridable in tests (bufconn).
var dialFunc = dial

func dial(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// withClient parses the shared flags, dials, and hands a ready ConfigService
// client to fn. Centralizes conn lifecycle + error formatting so each command
// stays a few lines.
func withClient(ctx context.Context, fs *flag.FlagSet, args []string, stderr io.Writer,
	fn func(context.Context, xtcp_config.ConfigServiceClient) int) int {

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target := fs.Lookup("target").Value.String()
	port := fs.Lookup("port").Value.String()

	conn, err := dialFunc(target + ":" + port)
	if err != nil {
		fmt.Fprintf(stderr, "xtcp2ctl: connect %s:%s: %v\n", target, port, err)
		return 1
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			fmt.Fprintf(stderr, "xtcp2ctl: conn close: %v\n", cerr)
		}
	}()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return fn(callCtx, xtcp_config.NewConfigServiceClient(conn))
}

func cmdGet(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		resp, err := c.Get(ctx, &xtcp_config.GetRequest{})
		if err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl get: %v\n", err)
			return 1
		}
		return printConfig(stdout, stderr, resp.GetConfig())
	})
}

func cmdSetPollFrequency(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set-poll-frequency", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	frequency := fs.Duration("frequency", 0, "new poll frequency, e.g. 30s (required)")
	timeout := fs.Duration("timeout", 0, "new per-namespace poll timeout, must be < frequency (required)")
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		if *frequency <= 0 || *timeout <= 0 {
			fmt.Fprintln(stderr, "xtcp2ctl set-poll-frequency: -frequency and -timeout are required (>0)")
			return 2
		}
		if _, err := c.SetPollFrequency(ctx, &xtcp_config.SetPollFrequencyRequest{
			PollFrequency: durationpb.New(*frequency),
			PollTimeout:   durationpb.New(*timeout),
		}); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl set-poll-frequency: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "ok: poll frequency=%s timeout=%s\n", *frequency, *timeout)
		return 0
	})
}

func cmdTriggerPoll(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trigger-poll", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		if _, err := c.TriggerPoll(ctx, &xtcp_config.TriggerPollRequest{}); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl trigger-poll: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok: poll triggered")
		return 0
	})
}

func cmdPollBurst(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("poll-burst", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	count := fs.Uint("count", 6, "number of polls in the burst (1-1000)")
	interval := fs.Duration("interval", 10*time.Second, "spacing between polls; must exceed the daemon's poll timeout")
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		if _, err := c.TriggerPollBurst(ctx, &xtcp_config.TriggerPollBurstRequest{
			Count:    uint32(*count),
			Interval: durationpb.New(*interval),
		}); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl poll-burst: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "ok: burst of %d polls scheduled, %s apart\n", *count, *interval)
		return 0
	})
}

func cmdSetS3(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set-s3", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	flushInterval := fs.Duration("flush-interval", 0, "new s3parquet staleness-flush timer, e.g. 5s")
	thresholdBytes := fs.Uint("threshold-bytes", 0, "new s3parquet byte cap before finalize+upload")
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		// Only send fields the operator explicitly set. Empty request violates
		// the server's "at least one" rule, so catch it locally.
		req := &xtcp_config.SetS3UploadRequest{}
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		if set["flush-interval"] {
			req.S3FlushInterval = durationpb.New(*flushInterval)
		}
		if set["threshold-bytes"] {
			req.S3ParquetFlushThresholdBytes = uint32(*thresholdBytes)
		}
		if !set["flush-interval"] && !set["threshold-bytes"] {
			fmt.Fprintln(stderr, "xtcp2ctl set-s3: set -flush-interval and/or -threshold-bytes")
			return 2
		}
		if _, err := c.SetS3Upload(ctx, req); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl set-s3: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok: s3 upload settings updated")
		return 0
	})
}

func cmdReconfigure(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reconfigure", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	file := fs.String("file", "-", `config JSON file to apply; "-" reads stdin (default)`)
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		raw, err := readConfigInput(*file)
		if err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl reconfigure: read %s: %v\n", *file, err)
			return 1
		}
		cfg := &xtcp_config.XtcpConfig{}
		if err := protojson.Unmarshal(raw, cfg); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl reconfigure: parse config JSON: %v\n", err)
			return 1
		}
		if _, err := c.Set(ctx, &xtcp_config.SetRequest{Config: cfg}); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl reconfigure: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok: reconfigure accepted — daemon is soft-restarting (gRPC/metrics blip for a few seconds)")
		return 0
	})
}

// readConfigInput returns the bytes of the config file, or stdin when path is
// "-" (or empty). stdinReader is overridable in tests.
var stdinReader io.Reader = os.Stdin

func readConfigInput(path string) ([]byte, error) {
	if path == "-" || path == "" {
		return io.ReadAll(stdinReader)
	}
	return os.ReadFile(path)
}

// printConfig writes cfg as indented protojson — the exact shape reconfigure
// consumes, so `get | edit | reconfigure` round-trips.
func printConfig(stdout, stderr io.Writer, cfg *xtcp_config.XtcpConfig) int {
	if cfg == nil {
		fmt.Fprintln(stderr, "xtcp2ctl: daemon returned no config")
		return 1
	}
	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "xtcp2ctl: marshal config: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}
