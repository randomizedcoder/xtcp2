package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
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
	all, problems := monitorprom.NewCollector(m), monitorprom.NewSummaryCollector(m, monitorprom.WithoutScalars())
	allRegistry := commandRegistry(all, nil)
	allRegistry.MustRegister(problems)
	var interfaces *views
	if opts.exposure.interfacePaths {
		// The interface views reuse the path collectors' cached catalogs.
		interfaces = &views{all: snapshotView(m, all, problems), categories: make(map[string]interfaceView, len(opts.exposure.categories))}
	}
	categories, adminRegistries, err := categoryRegistries(m, opts.exposure.categories, interfaces, teeRegisterer{allRegistry})
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
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
	served, err := collectionMode(endpoints(opts.address, opts.exposure, coreRegistry, allRegistry, categories, interfaces), m, opts, logger, adminRegistries)
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

// categoryRegistries builds one registry per detail category and its
// interface view. Admin request metrics are also served on the collection
// category, so its registry joins admin.
func categoryRegistries(m *linkmonitor.Monitor, names []string, interfaces *views, admin teeRegisterer) (map[string]prometheus.Gatherer, teeRegisterer, error) {
	categories := make(map[string]prometheus.Gatherer, len(names))
	for _, category := range names {
		match, err := monitorprom.CategoryMatcher(category)
		if err != nil {
			return nil, nil, err
		}
		collector := monitorprom.NewCollector(m, monitorprom.WithMetricFilter(match))
		registry := commandRegistry(collector, match)
		categories[category] = registry
		// Netstat is host scope: it has no interface form.
		if interfaces != nil && category != monitorprom.CategoryNetstat {
			interfaces.categories[category] = snapshotView(m, collector)
		}
		if category == monitorprom.CategoryCollection {
			admin = append(admin, registry)
		}
	}
	return categories, admin, nil
}

// protectRoutes puts every metrics route behind the policy's read access.
// Health endpoints stay open for probes. An unenforced policy changes nothing.
func protectRoutes(served []endpoint, policy *listenerauth.Policy) []endpoint {
	if policy == nil || !policy.Enabled() {
		return served
	}
	read := listenerauth.PathAccess()
	for i := range served {
		for j := range served[i].routes {
			served[i].routes[j].protect = func(h http.Handler) http.Handler { return policy.WrapHTTP(h, read) }
		}
	}
	return served
}

// commandRegistry adds build identity unless match rejects its name; a nil
// match keeps it.
func commandRegistry(collector prometheus.Collector, match func(string) bool) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	if match != nil && !match(buildInfoName) {
		return registry
	}
	registry.MustRegister(buildIdentity())
	return registry
}

func buildIdentity() prometheus.Gauge {
	identity := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: buildInfoName, Help: "Executable build identity.",
		ConstLabels: prometheus.Labels{"version": version, "revision": commit},
	})
	identity.Set(1)
	return identity
}

// snapshotView serves one interface of the monitor's current snapshot through
// collectors, with build identity. Each request resolves the name and
// collects from that one snapshot.
func snapshotView(m *linkmonitor.Monitor, collectors ...prometheus.Collector) interfaceView {
	identity := buildIdentity()
	return func(name string) (prometheus.Gatherer, error) {
		collector, err := monitorprom.InterfaceCollector(m.Snapshot(), name, collectors...)
		if err != nil {
			return nil, err
		}
		registry := prometheus.NewRegistry()
		registry.MustRegister(collector, identity)
		return registry, nil
	}
}

// teeRegisterer registers each collector on every registry, so one set of
// admin request metrics is served on several paths. A failed registration
// leaves no registry holding the collector.
type teeRegisterer []prometheus.Registerer

func (t teeRegisterer) Register(c prometheus.Collector) error {
	for i, r := range t {
		if err := r.Register(c); err != nil {
			for _, registered := range t[:i] {
				registered.Unregister(c)
			}
			return err
		}
	}
	return nil
}

func (t teeRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := t.Register(c); err != nil {
			panic(err)
		}
	}
}

func (t teeRegisterer) Unregister(c prometheus.Collector) bool {
	removed := false
	for _, r := range t {
		removed = r.Unregister(c) || removed
	}
	return removed
}
