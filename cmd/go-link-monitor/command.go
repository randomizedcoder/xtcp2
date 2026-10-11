package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

const buildInfoName = "go_link_monitor_build_info"

func command(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	opts, err := parseOptions(args, stdout, getenv)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if err != nil {
		logger.Error("invalid command", "error", err)
		return 2
	}
	if opts.version {
		if _, err := fmt.Fprintf(stdout, "go-link-monitor version=%s revision=%s date=%s\n", version, commit, date); err != nil {
			logger.Error("write version", "error", err)
			return 1
		}
		return 0
	}
	m, err := linkmonitor.New(opts.monitor, linkmonitor.Options{Logger: logger})
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
	core, err := opts.exposure.coreMatcher(opts.address)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
	coreRegistry := commandRegistry(monitorprom.NewCollector(m, monitorprom.WithMetricFilter(core)), core)
	allRegistry := commandRegistry(monitorprom.NewCollector(m), nil)
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(signals)
	var lc net.ListenConfig
	served := endpoints(opts.address, opts.exposure, coreRegistry, allRegistry)
	if err := serve(context.Background(), m, logger, signals, lc.Listen, served); err != nil {
		logger.Error("monitor stopped", "error", err)
		return 1
	}
	return 0
}

// commandRegistry adds build identity unless match rejects its name; a nil
// match keeps it.
func commandRegistry(collector prometheus.Collector, match func(string) bool) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	if match != nil && !match(buildInfoName) {
		return registry
	}
	identity := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: buildInfoName, Help: "Executable build identity.",
		ConstLabels: prometheus.Labels{"version": version, "revision": commit},
	})
	identity.Set(1)
	registry.MustRegister(identity)
	return registry
}
