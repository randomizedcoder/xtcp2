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
}

// exposureOptions selects which metrics each endpoint serves.
type exposureOptions struct {
	coreInclude, coreExclude string
	allPath, allAddress      string
	summary                  string
	categories               []string
	// interfacePaths serves PATH/{interface} beside every detail path.
	interfacePaths bool
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
	flags.StringVar(&exceptions, "max-speed-exceptions", "", "Comma-separated exact selectors, each optionally NAME=SPEED (1G, 2500M); overrides "+exceptionEnvironment)
	flags.StringVar(&opts.exposure.coreInclude, "metrics.core-include", defaultCoreInclude,
		"Metric names served on "+metricsPath+" (anchored regexp); overrides "+coreIncludeEnvironment)
	flags.StringVar(&opts.exposure.coreExclude, "metrics.core-exclude", "",
		"Metric names removed from "+metricsPath+" (anchored regexp, empty excludes nothing); overrides "+coreExcludeEnvironment)
	flags.StringVar(&opts.exposure.allPath, "metrics-all-path", defaultAllPath,
		"Path serving every metric (empty disables); overrides "+allPathEnvironment)
	flags.StringVar(&opts.exposure.allAddress, "metrics-all-addr", "",
		"Separate listen address for the all-metrics path (empty shares -metrics-addr); overrides "+allAddressEnvironment)
	flags.StringVar(&opts.exposure.summary, "metrics.summary", summaryProblems,
		"What "+metricsPath+" serves: problems (sparse summary) or core (-metrics.core-include selection); overrides "+summaryEnvironment)
	categories := strings.Join(monitorprom.Categories(), ",")
	flags.StringVar(&categories, "metrics.categories", categories,
		"Comma-separated detail categories served on "+categoryPathPrefix+"CATEGORY beside the all-metrics path (empty serves none); overrides "+categoriesEnvironment)
	flags.BoolVar(&opts.exposure.interfacePaths, "metrics.interface-paths", true,
		"Serve one interface of each category path and of the all-metrics path on PATH/INTERFACE; overrides "+interfacePathsEnviron)
	errorWindow, flapWindow := opts.monitor.ErrorWindow.String(), opts.monitor.FlapWindow.String()
	flags.StringVar(&errorWindow, "errors.window", errorWindow, "Window an error counter increase stays a problem (0 disables); overrides "+errorWindowEnvironment)
	flags.StringVar(&flapWindow, "flap-window", flapWindow, "Window a link flap stays a problem (0 disables); overrides "+flapWindowEnvironment)
	errorClasses := strings.Join(opts.monitor.ErrorClasses, ",")
	flags.StringVar(&errorClasses, "errors.classes", errorClasses, "Comma-separated error classes: link, rdma, drop; overrides "+errorClassEnvironment)
	flags.StringVar(&opts.monitor.ErrorExclude, "errors.exclude", "",
		"Error series keys METRIC[:STATISTIC]@INTERFACE never counted (anchored regexp); overrides "+errorExcludeEnviron)
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
		{"metrics.core-include", coreIncludeEnvironment, &opts.exposure.coreInclude},
		{"metrics.core-exclude", coreExcludeEnvironment, &opts.exposure.coreExclude},
		{"metrics-all-path", allPathEnvironment, &opts.exposure.allPath},
		{"metrics-all-addr", allAddressEnvironment, &opts.exposure.allAddress},
		{"metrics.summary", summaryEnvironment, &opts.exposure.summary},
		{"metrics.categories", categoriesEnvironment, &categories},
		{"errors.window", errorWindowEnvironment, &errorWindow},
		{"flap-window", flapWindowEnvironment, &flapWindow},
		{"errors.classes", errorClassEnvironment, &errorClasses},
		{"errors.exclude", errorExcludeEnviron, &opts.monitor.ErrorExclude},
	}, adminFallbacks...) {
		if value := getenv(fallback.environment); !explicit[fallback.flag] && value != "" {
			*fallback.target = value
		}
	}
	if exceptions != "" {
		opts.monitor.MaxSpeedExceptions = strings.Split(exceptions, ",")
	}
	opts.exposure.categories = splitList(categories)
	if value := getenv(interfacePathsEnviron); !explicit["metrics.interface-paths"] && value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return opts, fmt.Errorf("%s: %w", interfacePathsEnviron, err)
		}
		opts.exposure.interfacePaths = enabled
	}
	opts.monitor.ErrorClasses = splitList(errorClasses)
	for _, window := range [...]struct {
		name, value string
		target      *time.Duration
	}{{"errors.window", errorWindow, &opts.monitor.ErrorWindow}, {"flap-window", flapWindow, &opts.monitor.FlapWindow}} {
		parsed, err := time.ParseDuration(window.value)
		if err != nil {
			return opts, fmt.Errorf("%s: %w", window.name, err)
		}
		*window.target = parsed
	}
	return opts, nil
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
