package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

const exceptionEnvironment = "GO_LINK_MONITOR_MAX_SPEED_EXCEPTIONS"

type commandOptions struct {
	monitor linkmonitor.Config
	address string
	version bool
}

func parseOptions(args []string, output io.Writer, getenv func(string) string) (commandOptions, error) {
	opts := commandOptions{monitor: linkmonitor.DefaultConfig()}
	flags := flag.NewFlagSet("go-link-monitor", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.address, "metrics-addr", ":9101", "HTTP listen address")
	flags.StringVar(&opts.monitor.BaselineFile, "baseline-file", opts.monitor.BaselineFile, "Persistent baseline file")
	flags.DurationVar(&opts.monitor.Settle, "settle", opts.monitor.Settle, "Initial stable interval (nonnegative)")
	flags.DurationVar(&opts.monitor.Resync, "resync", opts.monitor.Resync, "Full reconciliation interval (positive)")
	flags.DurationVar(&opts.monitor.StatsInterval, "stats-interval", opts.monitor.StatsInterval, "Statistics interval (positive)")
	flags.StringVar(&opts.monitor.StatsInclude, "stats-include", opts.monitor.StatsInclude, "Driver/PHY names to include (regexp)")
	flags.StringVar(&opts.monitor.StatsExclude, "stats-exclude", opts.monitor.StatsExclude, "Driver/PHY names to exclude (regexp)")
	flags.StringVar(&opts.monitor.NetstatFields, "collector.netstat.fields", opts.monitor.NetstatFields, "Host protocol fields (regexp)")
	var exceptions string
	flags.StringVar(&exceptions, "max-speed-exceptions", "", "Comma-separated exact selectors; overrides "+exceptionEnvironment)
	backend := string(opts.monitor.IOBackend)
	flags.StringVar(&backend, "io-backend", backend, "Socket backend: poller or io_uring (requires available implementation)")
	flags.BoolVar(&opts.version, "version", false, "Print build identity and exit")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected positional arguments: %q", flags.Args())
	}
	opts.monitor.IOBackend = linkmonitor.IOBackend(backend)
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "max-speed-exceptions" {
			explicit = true
		}
	})
	if !explicit {
		exceptions = getenv(exceptionEnvironment)
	}
	if exceptions != "" {
		opts.monitor.MaxSpeedExceptions = strings.Split(exceptions, ",")
	}
	return opts, nil
}
