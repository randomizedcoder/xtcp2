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
	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorhttp"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

const buildInfoName = "go_link_monitor_build_info"

func command(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) > 0 && args[0] == ctlCommand {
		return ctl(args[1:], stdout, stderr, getenv)
	}
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
	access, err := opts.admin.resolve()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
	coreRegistry := commandRegistry(monitorprom.NewSummaryCollector(m), nil)
	if opts.exposure.summary == summaryCore {
		coreRegistry = commandRegistry(monitorprom.NewCollector(m, monitorprom.WithMetricFilter(core)), core)
	}
	detail, err := linkmonitorhttp.NewDetail(m, opts.exposure.categories, opts.exposure.interfacePaths, identity())
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
	adminRegistries := detail.Admin
	var admins []*adminEndpoint
	if access.endpoint != nil {
		// Admin request metrics are served on the collection and full expositions.
		admin, err := newAdminEndpoint(*access.endpoint, opts.hops, access.policy, m, logger, adminRegistries)
		if err != nil {
			logger.Error("invalid configuration", "error", err)
			return 2
		}
		admins = append(admins, admin)
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(signals)
	lc := net.ListenConfig{Control: opts.hops.control()}
	served, err := collectionMode(endpoints(opts.address, opts.exposure, coreRegistry, detail.All, detail.Categories, detail.Views), m, opts, logger, adminRegistries)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
	if opts.exposure.ipPaths {
		served, err = withIPPaths(served, goiphttp.Options{MaxBytes: opts.exposure.ipMaxBytes})
		if err != nil {
			logger.Error("invalid configuration", "error", err)
			return 2
		}
	}
	served = protectRoutes(served, access.policy)
	if err := serve(context.Background(), m, logger, signals, lc.Listen, served, admins...); err != nil {
		logger.Error("monitor stopped", "error", err)
		return 1
	}
	return 0
}

// protectRoutes puts every metrics route behind the policy's read access.
// Health endpoints stay open for probes. An unenforced policy changes nothing.
func protectRoutes(served []endpoint, policy *listenerauth.Policy) []endpoint {
	for i := range served {
		linkmonitorhttp.Protect(served[i].routes, policy)
	}
	return served
}

// commandRegistry adds build identity unless match rejects its name; a nil
// match keeps it.
func commandRegistry(collector prometheus.Collector, match func(string) bool) *prometheus.Registry {
	return linkmonitorhttp.Registry(collector, match, identity())
}

// identity is the build identity served on every detail path.
func identity() linkmonitorhttp.Identity {
	return linkmonitorhttp.Identity{Name: buildInfoName, Collector: buildIdentity()}
}

func buildIdentity() prometheus.Gauge {
	identity := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: buildInfoName, Help: "Executable build identity.",
		ConstLabels: prometheus.Labels{"version": version, "revision": commit},
	})
	identity.Set(1)
	return identity
}
