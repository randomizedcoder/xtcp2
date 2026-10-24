package linkmonitorhttp

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

// Identity is a build identity metric added to every detail registry. The
// zero value adds none.
type Identity struct {
	// Name is the metric name, matched against a route's metric filter.
	Name string
	// Collector is registered on each registry; one collector may serve many.
	Collector prometheus.Collector
}

// Registry registers collector and, unless match rejects its name, the
// identity; a nil match keeps it.
func Registry(collector prometheus.Collector, match func(string) bool, id Identity) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	if id.Collector == nil || (match != nil && !match(id.Name)) {
		return registry
	}
	registry.MustRegister(id.Collector)
	return registry
}

// SnapshotView serves one interface of the monitor's current snapshot through
// collectors, with the identity. Each request resolves the name and collects
// from that one snapshot.
func SnapshotView(m *linkmonitor.Monitor, id Identity, collectors ...prometheus.Collector) InterfaceView {
	return func(name string) (prometheus.Gatherer, error) {
		collector, err := monitorprom.InterfaceCollector(m.Snapshot(), name, collectors...)
		if err != nil {
			return nil, err
		}
		registry := prometheus.NewRegistry()
		registry.MustRegister(collector)
		if id.Collector != nil {
			registry.MustRegister(id.Collector)
		}
		return registry, nil
	}
}

// CategoryRegistries builds one registry per detail category and, when
// interfaces is set, its interface view. Admin request metrics are also
// served on the collection category, so its registry joins admin.
func CategoryRegistries(m *linkmonitor.Monitor, names []string, interfaces *Views, admin TeeRegisterer, id Identity) (map[string]prometheus.Gatherer, TeeRegisterer, error) {
	categories := make(map[string]prometheus.Gatherer, len(names))
	for _, category := range names {
		match, err := monitorprom.CategoryMatcher(category)
		if err != nil {
			return nil, nil, err
		}
		collector := monitorprom.NewCollector(m, monitorprom.WithMetricFilter(match))
		registry := Registry(collector, match, id)
		categories[category] = registry
		// Netstat is host scope: it has no interface form.
		if interfaces != nil && category != monitorprom.CategoryNetstat {
			interfaces.Categories[category] = SnapshotView(m, id, collector)
		}
		if category == monitorprom.CategoryCollection {
			admin = append(admin, registry)
		}
	}
	return categories, admin, nil
}

// Detail is the detail exposition of one monitor: the full registry, one
// registry per category and, optionally, their interface views.
type Detail struct {
	All        *prometheus.Registry
	Categories map[string]prometheus.Gatherer
	// Views is nil without interface paths.
	Views *Views
	// Admin holds every registry that serves admin request metrics: All and
	// the collection category.
	Admin TeeRegisterer
}

// NewDetail builds the full registry (every collector plus the problem
// series without scalars), the category registries and, with interfacePaths,
// the interface views, which reuse the path collectors' cached catalogs.
func NewDetail(m *linkmonitor.Monitor, categories []string, interfacePaths bool, id Identity) (Detail, error) {
	all, problems := monitorprom.NewCollector(m), monitorprom.NewSummaryCollector(m, monitorprom.WithoutScalars())
	d := Detail{All: Registry(all, nil, id)}
	d.All.MustRegister(problems)
	if interfacePaths {
		d.Views = &Views{All: SnapshotView(m, id, all, problems), Categories: make(map[string]InterfaceView, len(categories))}
	}
	var err error
	d.Categories, d.Admin, err = CategoryRegistries(m, categories, d.Views, TeeRegisterer{d.All}, id)
	return d, err
}

// Routes returns the detail routes of d, as DetailRoutes.
func (d Detail) Routes(categoryPrefix string, categories []string, allPath string) []Route {
	return DetailRoutes(categoryPrefix, categories, allPath, d.All, d.Categories, d.Views)
}

// IPRoute mounts the read-only `ip … show` paths (/ip/OBJECT?query,
// /ip/OBJECT/{interface} and the `ip` command lines /ip/[OPTIONS/]OBJECT/VERB/…,
// each under /i as well). They are live netlink reads, independent of the
// monitor's snapshot, so on-demand refresh never applies to them and they
// never take a metrics route's slots.
func IPRoute(o goiphttp.Options) (Route, error) {
	h, err := goiphttp.New(o)
	if err != nil {
		return Route{}, err
	}
	return Route{Path: goiphttp.Prefix, Mount: h.Register}, nil
}

// Protect puts every route behind the policy's read access. An unenforced
// policy changes nothing.
func Protect(routes []Route, policy *listenerauth.Policy) {
	if policy == nil || !policy.Enabled() {
		return
	}
	read := listenerauth.PathAccess()
	for i := range routes {
		routes[i].Protect = func(h http.Handler) http.Handler { return policy.WrapHTTP(h, read) }
	}
}

// TeeRegisterer registers each collector on every registry, so one set of
// admin request metrics is served on several paths. A failed registration
// leaves no registry holding the collector.
type TeeRegisterer []prometheus.Registerer

func (t TeeRegisterer) Register(c prometheus.Collector) error {
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

func (t TeeRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := t.Register(c); err != nil {
			panic(err)
		}
	}
}

func (t TeeRegisterer) Unregister(c prometheus.Collector) bool {
	removed := false
	for _, r := range t {
		removed = r.Unregister(c) || removed
	}
	return removed
}
