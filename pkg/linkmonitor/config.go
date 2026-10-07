package linkmonitor

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// IOBackend selects the socket transport. Explicit choices never fall back.
type IOBackend string

const (
	// IOBackendPoller uses nonblocking sockets with the Go runtime poller.
	IOBackendPoller IOBackend = "poller"
	// IOBackendIOUring uses the optional io_uring transport.
	IOBackendIOUring IOBackend = "io_uring"
)

// Config is copied by New. Use DefaultConfig before overriding fields; zero
// values are not replaced by defaults. CLI/environment parsing belongs to hosts.
type Config struct {
	BaselineFile       string
	Settle             time.Duration
	Resync             time.Duration
	StatsInterval      time.Duration
	StatsInclude       string
	StatsExclude       string
	NetstatFields      string
	MaxSpeedExceptions []string
	IOBackend          IOBackend
}

// DefaultConfig returns independent configuration with all statistics selected.
func DefaultConfig() Config {
	return Config{
		BaselineFile: "/var/lib/go-link-monitor/baseline.json",
		Settle:       30 * time.Second, Resync: time.Hour, StatsInterval: 15 * time.Second,
		StatsInclude: ".*", StatsExclude: "^$", NetstatFields: ".*",
		IOBackend: IOBackendPoller,
	}
}

type configuration struct {
	Config
	include, exclude, netstat *regexp.Regexp
}

func validateConfig(cfg Config) (configuration, error) {
	var result configuration
	if cfg.BaselineFile == "" || strings.ContainsRune(cfg.BaselineFile, 0) {
		return result, fmt.Errorf("baseline file must be a nonempty path without NUL")
	}
	if cfg.Settle < 0 || cfg.Resync <= 0 || cfg.StatsInterval <= 0 {
		return result, fmt.Errorf("settle must be nonnegative; resync and stats interval must be positive")
	}
	if cfg.StatsInterval > math.MaxInt64/3 || cfg.Resync > (math.MaxInt64-1)/2 {
		return result, fmt.Errorf("stats or resync interval exceeds representable freshness deadline")
	}
	if cfg.IOBackend != IOBackendPoller && cfg.IOBackend != IOBackendIOUring {
		return result, fmt.Errorf("invalid I/O backend %q", cfg.IOBackend)
	}
	exceptions, err := normalizeExceptions(cfg.MaxSpeedExceptions)
	if err != nil {
		return result, err
	}
	cfg.MaxSpeedExceptions = exceptions
	result.Config = cfg
	for _, field := range []struct {
		name, expression string
		destination      **regexp.Regexp
	}{
		{"stats include", cfg.StatsInclude, &result.include},
		{"stats exclude", cfg.StatsExclude, &result.exclude},
		{"netstat fields", cfg.NetstatFields, &result.netstat},
	} {
		compiled, compileErr := regexp.Compile(field.expression)
		if compileErr != nil {
			return configuration{}, fmt.Errorf("%s: %w", field.name, compileErr)
		}
		*field.destination = compiled
	}
	return result, nil
}

func normalizeExceptions(input []string) ([]string, error) {
	result := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		name := strings.TrimSpace(raw)
		if !validSelector(name) {
			return nil, fmt.Errorf("invalid maximum-speed exception %q", raw)
		}
		if _, exists := seen[name]; !exists {
			result = append(result, name)
			seen[name] = struct{}{}
		}
	}
	return result, nil
}

func validSelector(name string) bool {
	if !strings.HasPrefix(name, "rdma:") {
		return validName(name, 15)
	}
	parts := strings.Split(name, ":")
	if len(parts) != 3 || !validName(parts[1], 63) {
		return false
	}
	port, err := strconv.ParseUint(parts[2], 10, 32)
	return err == nil && port > 0 && strconv.FormatUint(port, 10) == parts[2]
}

func validName(name string, maxBytes int) bool {
	if name == "" || name == "." || name == ".." || len(name) > maxBytes {
		return false
	}
	return !strings.ContainsAny(name, "/:\\,*?[](){}|^$") &&
		!strings.ContainsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
