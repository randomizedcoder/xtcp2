package linkmonitorhttp

import (
	"fmt"
	"slices"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// Paths is the detail path layout of one listener, checked before any route
// is registered.
type Paths struct {
	// CategoryPrefix precedes each category name, such as "/metrics/".
	CategoryPrefix string
	Categories     []string
	// AllPath is the all-metrics path; empty serves none.
	AllPath        string
	InterfacePaths bool
	IPPaths        bool
	// Reserved are the paths the caller serves on the same listener. One
	// ending in a slash reserves its subtree, as in a ServeMux pattern.
	Reserved []string
}

// Validate rejects categories the collector does not know, an all-metrics
// path a ServeMux would misread, and any path that would shadow, or be
// shadowed by, another route on the listener.
func (p Paths) Validate() error {
	if err := monitorprom.ValidateCategories(p.Categories); err != nil {
		return err
	}
	if !strings.HasPrefix(p.CategoryPrefix, "/") || !strings.HasSuffix(p.CategoryPrefix, "/") {
		return fmt.Errorf("category prefix %q must be an absolute path ending in a slash", p.CategoryPrefix)
	}
	if p.IPPaths {
		for _, r := range p.Reserved {
			if root, ok := ipRoot(r); ok {
				return fmt.Errorf("path %q collides with the %s paths", r, root)
			}
		}
	}
	if p.AllPath == "" {
		return nil
	}
	// ServeMux patterns treat spaces, braces and trailing slashes specially;
	// accept only one exact path.
	if !strings.HasPrefix(p.AllPath, "/") || strings.HasSuffix(p.AllPath, "/") || strings.ContainsAny(p.AllPath, " \t{}") {
		return fmt.Errorf("all-metrics path %q must be an exact absolute path without a trailing slash", p.AllPath)
	}
	for _, r := range p.Reserved {
		if p.AllPath == r || (strings.HasSuffix(r, "/") && strings.HasPrefix(p.AllPath+"/", r)) {
			return fmt.Errorf("all-metrics path %q collides with %q", p.AllPath, r)
		}
	}
	if p.IPPaths {
		if root, ok := ipRoot(p.AllPath); ok {
			return fmt.Errorf("all-metrics path %q collides with the %s paths", p.AllPath, root)
		}
	}
	category, under := strings.CutPrefix(p.AllPath, p.CategoryPrefix)
	if !under {
		return nil
	}
	if slices.Contains(p.Categories, category) {
		return fmt.Errorf("all-metrics path %q collides with a category path", p.AllPath)
	}
	// A category's interface route would shadow the all-metrics path under it.
	for _, c := range p.Categories {
		if p.InterfacePaths && strings.HasPrefix(category, c+"/") {
			return fmt.Errorf("all-metrics path %q is under the %s interface paths", p.AllPath, c)
		}
	}
	return nil
}

// ipRoot reports the /ip or /i root that path is, or is under.
func ipRoot(path string) (string, bool) {
	for _, root := range []string{goiphttp.Prefix, goiphttp.ShortPrefix} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return root, true
		}
	}
	return "", false
}
