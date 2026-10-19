package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

const (
	exceptionEnvironment   = "GO_LINK_MONITOR_MAX_SPEED_EXCEPTIONS"
	coreIncludeEnvironment = "GO_LINK_MONITOR_METRICS_CORE_INCLUDE"
	coreExcludeEnvironment = "GO_LINK_MONITOR_METRICS_CORE_EXCLUDE"
	allPathEnvironment     = "GO_LINK_MONITOR_METRICS_ALL_PATH"
	allAddressEnvironment  = "GO_LINK_MONITOR_METRICS_ALL_ADDR"
	summaryEnvironment     = "GO_LINK_MONITOR_METRICS_SUMMARY"
	categoriesEnvironment  = "GO_LINK_MONITOR_METRICS_CATEGORIES"
	interfacePathsEnviron  = "GO_LINK_MONITOR_METRICS_INTERFACE_PATHS"
	errorWindowEnvironment = "GO_LINK_MONITOR_ERRORS_WINDOW"
	flapWindowEnvironment  = "GO_LINK_MONITOR_FLAP_WINDOW"
	errorClassEnvironment  = "GO_LINK_MONITOR_ERRORS_CLASSES"
	errorExcludeEnviron    = "GO_LINK_MONITOR_ERRORS_EXCLUDE"
	modeEnvironment        = "GO_LINK_MONITOR_COLLECTION_MODE"
	watchEnvironment       = "GO_LINK_MONITOR_WATCH_INTERVAL"
	slowEnvironment        = "GO_LINK_MONITOR_WATCH_SLOW_INTERVAL"
	ttlEnvironment         = "GO_LINK_MONITOR_ON_DEMAND_TTL"
	waitEnvironment        = "GO_LINK_MONITOR_ON_DEMAND_WAIT"
	ipPathsEnvironment     = "GO_LINK_MONITOR_IP_PATHS"
	ipMaxBytesEnvironment  = "GO_LINK_MONITOR_IP_MAX_BYTES"

	metricsPath        = "/metrics"
	defaultAllPath     = "/metrics-all"
	defaultCoreInclude = monitorprom.CoreMetricsPattern
	categoryPathPrefix = metricsPath + "/"

	// summaryProblems serves the sparse problem summary on /metrics;
	// summaryCore serves the core pattern selection, for migration.
	summaryProblems = "problems"
	summaryCore     = "core"
)

type commandOptions struct {
	monitor  linkmonitor.Config
	address  string
	exposure exposureOptions
	admin    adminOptions
	version  bool
	// statsIgnored is an explicit -stats-interval on demand, which uses
	// the watch intervals instead.
	statsIgnored bool
}

// exposureOptions selects which metrics each endpoint serves.
type exposureOptions struct {
	coreInclude, coreExclude string
	allPath, allAddress      string
	summary                  string
	categories               []string
	// interfacePaths serves PATH/{interface} beside every detail path.
	interfacePaths bool
	// ipPaths serves the read-only `ip … show` paths under /ip on the main
	// listener; ipMaxBytes caps one response.
	ipPaths    bool
	ipMaxBytes int
}

func parseOptions(args []string, output io.Writer, getenv func(string) string) (commandOptions, error) {
	opts := commandOptions{monitor: linkmonitor.DefaultConfig()}
	flags := flag.NewFlagSet("go-link-monitor", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.address, "metrics-addr", ":9101", "HTTP listen address")
	flags.StringVar(&opts.monitor.BaselineFile, "baseline-file", opts.monitor.BaselineFile, "Persistent baseline file")
	flags.DurationVar(&opts.monitor.Settle, "settle", opts.monitor.Settle, "Initial stable interval (nonnegative)")
	flags.DurationVar(&opts.monitor.Resync, "resync", opts.monitor.Resync, "Full reconciliation interval (positive)")
	flags.DurationVar(&opts.monitor.StatsInterval, "stats-interval", opts.monitor.StatsInterval, "Poll mode: statistics interval (positive); unused on demand")
	flags.StringVar(&opts.monitor.StatsInclude, "stats-include", opts.monitor.StatsInclude, "Driver/PHY names to include (regexp)")
	flags.StringVar(&opts.monitor.StatsExclude, "stats-exclude", opts.monitor.StatsExclude, "Driver/PHY names to exclude (regexp)")
	flags.StringVar(&opts.monitor.NetstatFields, "collector.netstat.fields", opts.monitor.NetstatFields, "Host protocol fields (regexp)")
	var exceptions string
	flags.StringVar(&exceptions, "max-speed-exceptions", "", "Comma-separated exact selectors, each optionally NAME=SPEED (1G, 2500M); overrides "+exceptionEnvironment)
	categories := strings.Join(monitorprom.Categories(), ",")
	exposureFallbacks := defineExposureFlags(flags, &opts.exposure, &categories)
	errorWindow, flapWindow := opts.monitor.ErrorWindow.String(), opts.monitor.FlapWindow.String()
	flags.StringVar(&errorWindow, "errors.window", errorWindow, "Window an error counter increase stays a problem (0 disables); overrides "+errorWindowEnvironment)
	flags.StringVar(&flapWindow, "flap-window", flapWindow, "Window a link flap stays a problem (0 disables); overrides "+flapWindowEnvironment)
	errorClasses := strings.Join(opts.monitor.ErrorClasses, ",")
	flags.StringVar(&errorClasses, "errors.classes", errorClasses, "Comma-separated error classes: link, rdma, drop; overrides "+errorClassEnvironment)
	flags.StringVar(&opts.monitor.ErrorExclude, "errors.exclude", "",
		"Error series keys METRIC[:STATISTIC]@INTERFACE never counted (anchored regexp); overrides "+errorExcludeEnviron)
	mode := string(opts.monitor.CollectionMode)
	flags.StringVar(&mode, "collection.mode", mode, "When detail is collected: poll (every collector each -stats-interval) or on-demand (errors and state each -watch.interval, settings each -watch.slow-interval, the rest when a detail path is scraped); overrides "+modeEnvironment)
	watch, slow := opts.monitor.WatchInterval.String(), opts.monitor.SlowInterval.String()
	flags.StringVar(&watch, "watch.interval", watch, "On demand: error counter and state poll interval; overrides "+watchEnvironment)
	flags.StringVar(&slow, "watch.slow-interval", slow, "On demand: link settings poll interval; overrides "+slowEnvironment)
	ttl, wait := opts.monitor.OnDemandTTL.String(), opts.monitor.OnDemandWait.String()
	flags.StringVar(&ttl, "on-demand.ttl", ttl, "On demand: a detail scrape reuses data collected this recently (0 always collects); overrides "+ttlEnvironment)
	flags.StringVar(&wait, "on-demand.wait", wait, "On demand: longest a detail scrape waits for collection before serving what it has; overrides "+waitEnvironment)
	adminFallbacks := defineAdminFlags(flags, &opts.admin)
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
	// An explicit flag, even an empty one, overrides its environment variable.
	// An unset or empty variable keeps the flag default.
	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	for _, fallback := range append([]envFallback{
		{"max-speed-exceptions", exceptionEnvironment, &exceptions},
		{"errors.window", errorWindowEnvironment, &errorWindow},
		{"flap-window", flapWindowEnvironment, &flapWindow},
		{"errors.classes", errorClassEnvironment, &errorClasses},
		{"errors.exclude", errorExcludeEnviron, &opts.monitor.ErrorExclude},
		{"collection.mode", modeEnvironment, &mode},
		{"watch.interval", watchEnvironment, &watch},
		{"watch.slow-interval", slowEnvironment, &slow},
		{"on-demand.ttl", ttlEnvironment, &ttl},
		{"on-demand.wait", waitEnvironment, &wait},
	}, append(exposureFallbacks, adminFallbacks...)...) {
		if value := getenv(fallback.environment); !explicit[fallback.flag] && value != "" {
			*fallback.target = value
		}
	}
	if exceptions != "" {
		opts.monitor.MaxSpeedExceptions = strings.Split(exceptions, ",")
	}
	opts.exposure.categories = splitList(categories)
	if err := opts.exposure.applyEnvironment(getenv, explicit); err != nil {
		return opts, err
	}
	opts.monitor.ErrorClasses = splitList(errorClasses)
	for _, window := range [...]struct {
		name, value string
		target      *time.Duration
	}{
		{"errors.window", errorWindow, &opts.monitor.ErrorWindow}, {"flap-window", flapWindow, &opts.monitor.FlapWindow},
		{"watch.interval", watch, &opts.monitor.WatchInterval}, {"watch.slow-interval", slow, &opts.monitor.SlowInterval},
		{"on-demand.ttl", ttl, &opts.monitor.OnDemandTTL}, {"on-demand.wait", wait, &opts.monitor.OnDemandWait},
	} {
		parsed, err := time.ParseDuration(window.value)
		if err != nil {
			return opts, fmt.Errorf("%s: %w", window.name, err)
		}
		*window.target = parsed
	}
	opts.monitor.CollectionMode = linkmonitor.CollectionMode(mode)
	// A scrape must be answered inside the metrics timeout after its wait.
	if opts.monitor.CollectionMode == linkmonitor.CollectionOnDemand && opts.monitor.OnDemandWait >= metricsTimeout {
		return opts, fmt.Errorf("on-demand.wait %v must be under the %v metrics timeout", opts.monitor.OnDemandWait, metricsTimeout)
	}
	if opts.monitor.CollectionMode == linkmonitor.CollectionOnDemand && explicit["stats-interval"] {
		opts.statsIgnored = true
	}
	return opts, nil
}

// defineExposureFlags defines the endpoint flags. categories holds the
// comma-separated category list until parseOptions splits it.
func defineExposureFlags(flags *flag.FlagSet, o *exposureOptions, categories *string) []envFallback {
	flags.StringVar(&o.coreInclude, "metrics.core-include", defaultCoreInclude,
		"Metric names served on "+metricsPath+" (anchored regexp); overrides "+coreIncludeEnvironment)
	flags.StringVar(&o.coreExclude, "metrics.core-exclude", "",
		"Metric names removed from "+metricsPath+" (anchored regexp, empty excludes nothing); overrides "+coreExcludeEnvironment)
	flags.StringVar(&o.allPath, "metrics-all-path", defaultAllPath,
		"Path serving every metric (empty disables); overrides "+allPathEnvironment)
	flags.StringVar(&o.allAddress, "metrics-all-addr", "",
		"Separate listen address for the all-metrics path (empty shares -metrics-addr); overrides "+allAddressEnvironment)
	flags.StringVar(&o.summary, "metrics.summary", summaryProblems,
		"What "+metricsPath+" serves: problems (sparse summary) or core (-metrics.core-include selection); overrides "+summaryEnvironment)
	flags.StringVar(categories, "metrics.categories", *categories,
		"Comma-separated detail categories served on "+categoryPathPrefix+"CATEGORY beside the all-metrics path (empty serves none); overrides "+categoriesEnvironment)
	flags.BoolVar(&o.interfacePaths, "metrics.interface-paths", true,
		"Serve one interface of each category path and of the all-metrics path on PATH/INTERFACE; overrides "+interfacePathsEnviron)
	flags.BoolVar(&o.ipPaths, "ip.paths", false,
		"Serve read-only `ip -j OBJECT show` output on "+goiphttp.Prefix+"/OBJECT (link, addr, route, neigh, ...); exposes addresses, routes and neighbors, so pair it with read auth or a loopback -metrics-addr; overrides "+ipPathsEnvironment)
	flags.IntVar(&o.ipMaxBytes, "ip.max-bytes", goiphttp.DefaultMaxBytes,
		"Largest "+goiphttp.Prefix+" response body in bytes (positive); overrides "+ipMaxBytesEnvironment)
	return []envFallback{
		{"metrics.core-include", coreIncludeEnvironment, &o.coreInclude},
		{"metrics.core-exclude", coreExcludeEnvironment, &o.coreExclude},
		{"metrics-all-path", allPathEnvironment, &o.allPath},
		{"metrics-all-addr", allAddressEnvironment, &o.allAddress},
		{"metrics.summary", summaryEnvironment, &o.summary},
		{"metrics.categories", categoriesEnvironment, categories},
	}
}

// applyEnvironment applies the non-string exposure variables. As for the
// string fallbacks, an explicit flag wins and an empty variable is ignored.
func (o *exposureOptions) applyEnvironment(getenv func(string) string, explicit map[string]bool) error {
	for _, b := range [...]struct {
		flag, environment string
		target            *bool
	}{
		{"metrics.interface-paths", interfacePathsEnviron, &o.interfacePaths},
		{"ip.paths", ipPathsEnvironment, &o.ipPaths},
	} {
		if value := getenv(b.environment); !explicit[b.flag] && value != "" {
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: %w", b.environment, err)
			}
			*b.target = enabled
		}
	}
	if value := getenv(ipMaxBytesEnvironment); !explicit["ip.max-bytes"] && value != "" {
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s: %w", ipMaxBytesEnvironment, err)
		}
		o.ipMaxBytes = n
	}
	return nil
}

// splitList splits a comma-separated list; an empty string is an empty list.
func splitList(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}

// coreMatcher validates the exposure options and compiles the core selection
// exactly once, before any listener exists.
func (o exposureOptions) coreMatcher(address string) (func(string) bool, error) {
	if err := o.validate(address); err != nil {
		return nil, err
	}
	return monitorprom.NameMatcher(o.coreInclude, o.coreExclude)
}

func (o exposureOptions) validate(address string) error {
	if o.summary != summaryProblems && o.summary != summaryCore {
		return fmt.Errorf("metrics.summary %q must be %s or %s", o.summary, summaryProblems, summaryCore)
	}
	if err := monitorprom.ValidateCategories(o.categories); err != nil {
		return err
	}
	if o.ipPaths {
		if o.ipMaxBytes <= 0 {
			return fmt.Errorf("ip.max-bytes %d must be positive", o.ipMaxBytes)
		}
		// The /ip and /i paths live on the main listener only.
		for _, root := range []string{goiphttp.Prefix, goiphttp.ShortPrefix} {
			if o.shared(address) && (o.allPath == root || strings.HasPrefix(o.allPath, root+"/")) {
				return fmt.Errorf("metrics-all-path %q collides with the %s paths on the same listener", o.allPath, root)
			}
		}
	}
	if o.allPath != "" && strings.HasPrefix(o.allPath, categoryPathPrefix) && slices.Contains(o.categories, strings.TrimPrefix(o.allPath, categoryPathPrefix)) {
		return fmt.Errorf("metrics-all-path %q collides with a category path", o.allPath)
	}
	if o.allPath == "" {
		if o.allAddress != "" {
			return errors.New("metrics-all-addr requires a nonempty metrics-all-path")
		}
		return nil
	}
	// ServeMux patterns treat spaces, braces and trailing slashes specially;
	// accept only one exact path that does not collide with fixed routes.
	if !strings.HasPrefix(o.allPath, "/") || strings.HasSuffix(o.allPath, "/") || strings.ContainsAny(o.allPath, " \t{}") {
		return fmt.Errorf("metrics-all-path %q must be an exact absolute path without a trailing slash", o.allPath)
	}
	switch o.allPath {
	case "/healthz", "/readyz":
		return fmt.Errorf("metrics-all-path %q collides with a health endpoint", o.allPath)
	case metricsPath:
		if o.shared(address) {
			return fmt.Errorf("metrics-all-path %q collides with the core endpoint on the same listener", o.allPath)
		}
		// METRICS_PATH/{interface} would shadow interfaces named after a category.
		if o.interfacePaths && len(o.categories) != 0 {
			return fmt.Errorf("metrics-all-path %q with metrics.interface-paths shares the category prefix", o.allPath)
		}
	}
	// A category's interface route would shadow the all-metrics path under it.
	for _, category := range o.categories {
		if o.interfacePaths && strings.HasPrefix(o.allPath, categoryPathPrefix+category+"/") {
			return fmt.Errorf("metrics-all-path %q is under the %s interface paths", o.allPath, category)
		}
	}
	return nil
}

func (o exposureOptions) shared(address string) bool {
	return o.allAddress == "" || o.allAddress == address
}
