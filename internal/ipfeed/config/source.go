// Package config loads and validates source definitions. Each feed is one
// YAML file in a sources directory, which keeps sources easy to add, remove,
// and review individually, and lets the collector fan work out across files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/parse"
	"go.yaml.in/yaml/v3"
)

// Defaults mirror parse.Defaults for YAML decoding.
type Defaults struct {
	NetworkOwner    string `yaml:"network_owner"`
	ServiceOperator string `yaml:"service_operator"`
	Service         string `yaml:"service"`
	Product         string `yaml:"product"`
	Region          string `yaml:"region"`
	Direction       string `yaml:"direction"`
}

// Source is one feed definition, decoded from a single YAML file.
type Source struct {
	Name       string         `yaml:"name"`
	Provider   string         `yaml:"provider"`
	URL        string         `yaml:"url"`
	Parser     string         `yaml:"parser"`
	SourceType string         `yaml:"source_type"`
	Confidence string         `yaml:"confidence"`
	Defaults   Defaults       `yaml:"defaults"`
	Discover   string         `yaml:"discover"`
	ParserOpts map[string]any `yaml:"parser_opts"`
	Enabled    *bool          `yaml:"enabled"`

	// Path is the file the source was loaded from (for diagnostics). Not from YAML.
	Path string `yaml:"-"`
}

// IsEnabled reports whether the source should run. Absent `enabled:` means
// enabled, so a new file is active by default.
func (s Source) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Meta projects a Source onto the subset the parse package needs.
func (s Source) Meta() parse.SourceMeta {
	return parse.SourceMeta{
		Name:       s.Name,
		Provider:   s.Provider,
		URL:        s.URL,
		SourceType: s.SourceType,
		Confidence: s.Confidence,
		Defaults: parse.Defaults{
			NetworkOwner:    s.Defaults.NetworkOwner,
			ServiceOperator: s.Defaults.ServiceOperator,
			Service:         s.Defaults.Service,
			Product:         s.Defaults.Product,
			Region:          s.Defaults.Region,
			Direction:       s.Defaults.Direction,
		},
		Opts: s.ParserOpts,
	}
}

// validate checks required fields and that the parser key is known.
func (s Source) validate() error {
	var missing []string
	if s.Name == "" {
		missing = append(missing, "name")
	}
	if s.URL == "" {
		missing = append(missing, "url")
	}
	if s.Parser == "" {
		missing = append(missing, "parser")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required field(s): %s", strings.Join(missing, ", "))
	}
	if !parse.Registered(s.Parser) {
		known := parse.Names()
		sort.Strings(known)
		return fmt.Errorf("unknown parser %q (known: %s)", s.Parser, strings.Join(known, ", "))
	}
	if s.Discover != "" && s.Discover != "none" && s.Discover != "azure_download_page" {
		return fmt.Errorf("unknown discover mode %q", s.Discover)
	}
	return nil
}

// LoadFile parses and validates a single source file.
func LoadFile(path string) (Source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Source{}, err
	}
	var s Source
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true) // reject typo'd/unknown keys
	if err := dec.Decode(&s); err != nil {
		return Source{}, fmt.Errorf("decode %s: %w", path, err)
	}
	s.Path = path
	if err := s.validate(); err != nil {
		return Source{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// LoadDir loads every *.yaml / *.yml file in dir, returning only enabled
// sources sorted by name. A parse/validation error in any file is returned so
// a broken config fails fast rather than silently dropping a feed.
func LoadDir(dir string) ([]Source, error) {
	var paths []string
	for _, pat := range []string{"*.yaml", "*.yml"} {
		m, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			return nil, err
		}
		paths = append(paths, m...)
	}
	sort.Strings(paths)

	var out []Source
	seen := map[string]string{} // name -> path, to catch duplicate names
	for _, p := range paths {
		s, err := LoadFile(p)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[s.Name]; dup {
			return nil, fmt.Errorf("duplicate source name %q in %s and %s", s.Name, prev, p)
		}
		seen[s.Name] = p
		if !s.IsEnabled() {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
