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
	// ErrorWindow and FlapWindow bound how long counter-increase and flap
	// evidence keeps an interface problem active; zero disables the reason.
	ErrorWindow, FlapWindow time.Duration
	// ErrorClasses selects the error classes (link, rdma, drop) that count as
	// error evidence; ErrorExclude is an anchored regexp of series keys
	// <metric>[:<statistic>]@<interface> that never count.
	ErrorClasses []string
	ErrorExclude string
}

// DefaultConfig returns independent configuration with all statistics selected.
func DefaultConfig() Config {
	return Config{
		BaselineFile: "/var/lib/go-link-monitor/baseline.json",
		Settle:       30 * time.Second, Resync: time.Hour, StatsInterval: 15 * time.Second,
		StatsInclude: ".*", StatsExclude: "^$", NetstatFields: ".*",
		IOBackend:   IOBackendPoller,
		ErrorWindow: defaultProblemWindow, FlapWindow: defaultProblemWindow,
		ErrorClasses: []string{"link", "rdma"},
	}
}

type configuration struct {
	Config
	include, exclude, netstat *regexp.Regexp
	problems                  problemPolicy
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
	problems, err := newProblemPolicy(cfg)
	if err != nil {
		return result, err
	}
	result.problems = problems
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

// normalizeExceptions validates selectors and returns them in canonical form:
// the bare name, or name=SPEED with SPEED in whole Gb/s or Mb/s. A name named
// twice must agree on its expected speed.
func normalizeExceptions(input []string) ([]string, error) {
	result := make([]string, 0, len(input))
	seen := make(map[string]uint64, len(input))
	for _, raw := range input {
		name, expectedBits, err := parseException(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid maximum-speed exception %q: %w", raw, err)
		}
		previous, exists := seen[name]
		if exists && previous != expectedBits {
			return nil, fmt.Errorf("maximum-speed exception %q conflicts with an earlier entry for %s", raw, name)
		}
		if !exists {
			result = append(result, formatException(name, expectedBits))
			seen[name] = expectedBits
		}
	}
	return result, nil
}

// parseException splits "name" or "name=SPEED" at the last '='. An expected
// speed of zero means any speed, the bare-name meaning.
func parseException(selector string) (name string, expectedBits uint64, err error) {
	name = selector
	if i := strings.LastIndexByte(selector, '='); i >= 0 {
		name = selector[:i]
		if expectedBits, err = parseSpeed(selector[i+1:]); err != nil {
			return "", 0, err
		}
	}
	if !validSelector(name) {
		return "", 0, fmt.Errorf("not an exact interface name or rdma:DEVICE:PORT selector")
	}
	return name, expectedBits, nil
}

// maxExpectedMbps bounds an expected speed at 10 Tb/s, past any link today.
const maxExpectedMbps = 10_000_000

// parseSpeed reads Mb/s as ethtool reports it ("1000"), or a value with an M
// or G suffix ("100M", "2.5G"). G allows up to three decimals, so every
// value is a whole number of Mb/s; the result is in bits per second.
func parseSpeed(text string) (uint64, error) {
	digits, scale := text, uint64(1)
	switch {
	case strings.HasSuffix(text, "G"):
		digits, scale = strings.TrimSuffix(text, "G"), 1000
	case strings.HasSuffix(text, "M"):
		digits = strings.TrimSuffix(text, "M")
	}
	whole, fraction, decimal := strings.Cut(digits, ".")
	if decimal && (scale == 1 || fraction == "" || len(fraction) > 3) {
		return 0, fmt.Errorf("speed %q: only G takes decimals, at most three", text)
	}
	if whole == "" || !allDigits(whole) || !allDigits(fraction) || (len(whole) > 1 && whole[0] == '0') {
		return 0, fmt.Errorf("speed %q: want Mb/s, or a number with an M or G suffix", text)
	}
	units, err := strconv.ParseUint(whole, 10, 64)
	if err != nil || units > maxExpectedMbps {
		return 0, fmt.Errorf("speed %q: exceeds %d Mb/s", text, maxExpectedMbps)
	}
	mbps := units * scale
	if fraction != "" {
		// At most three digits, already checked, so this cannot fail.
		part, err := strconv.ParseUint((fraction + "00")[:3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("speed %q: %w", text, err)
		}
		mbps += part
	}
	if mbps == 0 || mbps > maxExpectedMbps {
		return 0, fmt.Errorf("speed %q: want 1 to %d Mb/s", text, maxExpectedMbps)
	}
	return mbps * 1_000_000, nil
}

func allDigits(text string) bool {
	return !strings.ContainsFunc(text, func(r rune) bool { return r < '0' || r > '9' })
}

// formatException renders the canonical selector: whole gigabits as G,
// anything else as M.
func formatException(name string, expectedBits uint64) string {
	if expectedBits == 0 {
		return name
	}
	mbps := expectedBits / 1_000_000
	if mbps%1000 == 0 {
		return name + "=" + strconv.FormatUint(mbps/1000, 10) + "G"
	}
	return name + "=" + strconv.FormatUint(mbps, 10) + "M"
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
