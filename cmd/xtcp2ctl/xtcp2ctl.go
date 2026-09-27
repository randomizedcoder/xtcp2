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
//	set-envelope-flush   change the envelope flush row/byte caps live
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
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
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
	case "set-envelope-flush":
		return cmdSetEnvelopeFlush(ctx, args[1:], stdout, stderr)
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
  set-envelope-flush   Change the envelope flush row / byte caps live (no restart)
  reconfigure          Apply a full config from a file or stdin (soft restart)

Common flags (all commands):
  -network string  gRPC network: tcp or unix (default "tcp")
  -target string   daemon hostname (default "localhost")
  -port string     daemon gRPC port (default "8889", must match -grpcPort)
  -unixSocket path daemon gRPC Unix socket path when -network unix
  -d uint          debug level (default 0)

Run 'xtcp2ctl <command> -h' for per-command flags.
`)
}

// commonFlags registers the flags every command shares and returns accessors.
func commonFlags(fs *flag.FlagSet) {
	fs.String("network", listener.NetworkTCP, "daemon gRPC network: tcp or unix")
	fs.String("target", targetHostnameCst, "daemon hostname")
	fs.String("port", grpcPortCst, "daemon gRPC port (must match the daemon's -grpcPort)")
	fs.String("unixSocket", "", "daemon gRPC Unix socket path when -network unix")
	fs.String("auth-token", "", "raw bearer token; prefer XTCP2_GRPC_AUTH_TOKEN or _FILE")
	fs.String("auth-token-file", "", "file containing the raw bearer token")
	fs.String("auth-hmac-shared-key", "", "HMAC shared key; prefer XTCP2_GRPC_HMAC_SHARED_KEY or _FILE")
	fs.String("auth-hmac-shared-key-file", "", "file containing the HMAC shared key")
}

// dialFunc is the connection factory, overridable in tests (bufconn).
var dialFunc = dial

func dial(network, target string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if network == listener.NetworkUnix {
		return grpc.NewClient("unix://"+target, opts...)
	}
	return grpc.NewClient(
		target,
		opts...,
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
	networkValue := fs.Lookup("network").Value.String()
	network, err := listener.ParseNetwork(networkValue)
	if err != nil {
		fmt.Fprintf(stderr, "xtcp2ctl: %v\n", err)
		return 2
	}
	networkName, err := listener.NetworkString(network, listener.NetworkTCP)
	if err != nil {
		fmt.Fprintf(stderr, "xtcp2ctl: %v\n", err)
		return 2
	}
	dialTarget := target + ":" + port
	auth, err := listenerauth.ClientAuthFromEnvAndFlags(
		fs.Lookup("auth-token").Value.String(), fs.Lookup("auth-token-file").Value.String(),
		fs.Lookup("auth-hmac-shared-key").Value.String(), fs.Lookup("auth-hmac-shared-key-file").Value.String(),
		"XTCP2_GRPC_AUTH_TOKEN", "XTCP2_GRPC_AUTH_TOKEN_FILE",
		"XTCP2_GRPC_HMAC_SHARED_KEY", "XTCP2_GRPC_HMAC_SHARED_KEY_FILE")
	if err != nil {
		fmt.Fprintf(stderr, "xtcp2ctl: auth configuration: %v\n", err)
		return 2
	}
	displayTarget := dialTarget
	if networkName == listener.NetworkUnix {
		dialTarget = fs.Lookup("unixSocket").Value.String()
		displayTarget = "unix:" + dialTarget
		if dialTarget == "" {
			fmt.Fprintln(stderr, "xtcp2ctl: -unixSocket is required when -network unix")
			return 2
		}
	}

	conn, err := dialFunc(networkName, dialTarget)
	if err != nil {
		fmt.Fprintf(stderr, "xtcp2ctl: connect %s: %v\n", displayTarget, err)
		return 1
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			fmt.Fprintf(stderr, "xtcp2ctl: conn close: %v\n", cerr)
		}
	}()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	callCtx = listenerauth.ContextWithClientAuth(callCtx, auth, time.Now())
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

func cmdSetEnvelopeFlush(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set-envelope-flush", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commonFlags(fs)
	thresholdBytes := fs.Uint("threshold-bytes", 0, "new envelope uncompressed byte cap (0 = daemon default 768 KiB)")
	thresholdRows := fs.Uint("threshold-rows", 0, "new envelope row-count cap (0 = daemon default 10000)")
	return withClient(ctx, fs, args, stderr, func(ctx context.Context, c xtcp_config.ConfigServiceClient) int {
		// Only send fields the operator explicitly set. Empty request violates
		// the server's "at least one" rule, so catch it locally.
		req := &xtcp_config.SetEnvelopeFlushRequest{}
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		if set["threshold-bytes"] {
			req.EnvelopeFlushThresholdBytes = uint32(*thresholdBytes)
		}
		if set["threshold-rows"] {
			req.EnvelopeFlushThresholdRows = uint32(*thresholdRows)
		}
		if !set["threshold-bytes"] && !set["threshold-rows"] {
			fmt.Fprintln(stderr, "xtcp2ctl set-envelope-flush: set -threshold-bytes and/or -threshold-rows")
			return 2
		}
		if _, err := c.SetEnvelopeFlush(ctx, req); err != nil {
			fmt.Fprintf(stderr, "xtcp2ctl set-envelope-flush: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "ok: envelope flush thresholds updated")
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
