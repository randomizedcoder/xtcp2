package parse

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("github_meta", githubMeta{}) }

// githubMeta parses https://api.github.com/meta, an object whose values are
// per-service CIDR arrays (hooks, web, api, git, packages, pages, actions,
// actions_macos, dependabot, copilot, ...). Non-array values (booleans, the
// ssh key fingerprint object, domains, etc.) are ignored. The JSON key becomes
// the record service.
type githubMeta struct{}

func (githubMeta) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("github_meta: %w", err)
	}
	// Deterministic order for stable output/tests.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []model.Record
	for _, k := range keys {
		var cidrs []string
		if err := json.Unmarshal(raw[k], &cidrs); err != nil {
			continue // value is not a []string (e.g. bool/object): skip
		}
		for _, c := range cidrs {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			r := meta.Base(retrievedAt)
			r.Prefix = c
			r.Service = k
			out = append(out, r)
		}
	}
	return out, nil
}
