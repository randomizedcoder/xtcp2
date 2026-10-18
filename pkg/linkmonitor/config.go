package linkmonitor

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacaps"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

// IOBackend selects the socket transport. Explicit choices never fall back.
type IOBackend string

const (
	// IOBackendPoller uses nonblocking sockets with the Go runtime poller.
	IOBackendPoller IOBackend = "poller"
	// IOBackendIOUring uses the optional io_uring transport.
	IOBackendIOUring IOBackend = "io_uring"
)

// RDMAEventsBackend selects where RDMA port and device events come from.
// Explicit choices never fall back.
type RDMAEventsBackend string

const (
	// RDMAEventsAuto is verbs when the build has the cgo verbs binding and
	// netlink otherwise.
	RDMAEventsAuto RDMAEventsBackend = "auto"
	// RDMAEventsVerbs reads each device's verbs async event queue; it needs
	// the Linux rdma build tag, cgo and libibverbs.
	RDMAEventsVerbs RDMAEventsBackend = "verbs"
	// RDMAEventsNetlink derives the events from rtnetlink link and address
	// notifications and the NLDEV monitor, without cgo.
	RDMAEventsNetlink RDMAEventsBackend = "netlink"
)

// RDMAVerbsAvailable reports whether this build has the cgo verbs binding
// that RDMAEventsVerbs needs.
const RDMAVerbsAvailable = rdmaevents.Available

// ErrRDMAEventsBackend is an RDMAEventsBackend that is unknown or that this
// build lacks.
var ErrRDMAEventsBackend = errors.New("linkmonitor: RDMA events backend unavailable")

// resolve returns the backend auto stands for in this build.
func (b RDMAEventsBackend) resolve() RDMAEventsBackend {
	if b == RDMAEventsAuto || b == "" {
		if rdmaevents.Available {
			return RDMAEventsVerbs
		}
		return RDMAEventsNetlink
	}
	return b
}

func validateRDMAEventsBackend(b RDMAEventsBackend) error {
	switch b {
	case RDMAEventsAuto, "", RDMAEventsNetlink: // empty is auto, so configurations predating the field stay valid
		return nil
	case RDMAEventsVerbs:
		if rdmaevents.Available {
			return nil
		}
		return fmt.Errorf("%w: %s needs a build with the rdma tag and cgo", ErrRDMAEventsBackend, b)
	}
	return fmt.Errorf("%w: %q: want %s, %s or %s", ErrRDMAEventsBackend, b, RDMAEventsAuto, RDMAEventsVerbs, RDMAEventsNetlink)
}

// RDMAUMADBackend selects the transport of the InfiniBand PortInfo query
// behind rdma_capabilities. Explicit choices never fall back.
type RDMAUMADBackend string

const (
	// RDMAUMADAuto is libibumad when the build has the cgo binding and native
	// otherwise.
	RDMAUMADAuto RDMAUMADBackend = "auto"
	// RDMAUMADLibibumad queries through libibumad, one query at a time per
	// process; it needs the Linux rdma build tag, cgo and libibumad.
	RDMAUMADLibibumad RDMAUMADBackend = "libibumad"
	// RDMAUMADNative queries the umad device directly, without cgo; each
	// query owns its descriptor, so ports are queried in parallel.
	RDMAUMADNative RDMAUMADBackend = "native"
)

// RDMALibibumadAvailable reports whether this build has the cgo libibumad
// binding that RDMAUMADLibibumad needs.
const RDMALibibumadAvailable = rdmacaps.LibibumadAvailable

// ErrRDMAUMADBackend is an RDMAUMADBackend that is unknown or that this build
// lacks.
var ErrRDMAUMADBackend = errors.New("linkmonitor: RDMA umad backend unavailable")

// resolve returns the backend auto stands for in this build.
func (b RDMAUMADBackend) resolve() RDMAUMADBackend {
	if b == RDMAUMADAuto || b == "" {
		if rdmacaps.LibibumadAvailable {
			return RDMAUMADLibibumad
		}
		return RDMAUMADNative
	}
	return b
}

// caps is the rdmacaps backend of a validated one; rdmacaps resolves auto
// as resolve does.
func (b RDMAUMADBackend) caps() rdmacaps.Backend {
	switch b {
	case RDMAUMADLibibumad:
		return rdmacaps.Libibumad
	case RDMAUMADNative:
		return rdmacaps.Native
	case RDMAUMADAuto:
	}
	return rdmacaps.Auto // auto or empty
}

func validateRDMAUMADBackend(b RDMAUMADBackend) error {
	switch b {
	case RDMAUMADAuto, "", RDMAUMADNative: // empty is auto, so configurations predating the field stay valid
		return nil
	case RDMAUMADLibibumad:
		if rdmacaps.LibibumadAvailable {
			return nil
		}
		return fmt.Errorf("%w: %s needs a build with the rdma tag and cgo", ErrRDMAUMADBackend, b)
	}
	return fmt.Errorf("%w: %q: want %s, %s or %s", ErrRDMAUMADBackend, b, RDMAUMADAuto, RDMAUMADLibibumad, RDMAUMADNative)
}

// CollectionMode selects when detail collectors run.
type CollectionMode string

const (
	// CollectionPoll polls every collector each StatsInterval.
	CollectionPoll CollectionMode = "poll"
	// CollectionOnDemand polls the error counters each WatchInterval and the
	// link settings each SlowInterval; the remaining detail collectors run
	// at startup, on events and resyncs, and when Monitor.Refresh asks.
	CollectionOnDemand CollectionMode = "on-demand"
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
	// RDMAEventsBackend is auto, verbs or netlink; see RDMAEventsBackend.
	RDMAEventsBackend RDMAEventsBackend
	// RDMAUMADBackend is auto, libibumad or native; see RDMAUMADBackend.
	RDMAUMADBackend RDMAUMADBackend
	// ErrorWindow and FlapWindow bound how long counter-increase and flap
	// evidence keeps an interface problem active; zero disables the reason.
	ErrorWindow, FlapWindow time.Duration
	// ErrorClasses selects the error classes (link, rdma, drop) that count as
	// error evidence; ErrorExclude is an anchored regexp of series keys
	// <metric>[:<statistic>]@<interface> that never count.
	ErrorClasses []string
	ErrorExclude string
	// CollectionMode is poll or on-demand. The remaining fields apply only
	// on demand, where StatsInterval is unused: WatchInterval polls the
	// error and state collectors, SlowInterval the settings, a Refresh
	// reuses data younger than OnDemandTTL, and waits at most OnDemandWait.
	CollectionMode              CollectionMode
	WatchInterval, SlowInterval time.Duration
	OnDemandTTL, OnDemandWait   time.Duration
}

// DefaultConfig returns independent configuration with all statistics selected.
func DefaultConfig() Config {
	return Config{
		BaselineFile: "/var/lib/go-link-monitor/baseline.json",
		Settle:       30 * time.Second, Resync: time.Hour, StatsInterval: 15 * time.Second,
		StatsInclude: ".*", StatsExclude: "^$", NetstatFields: ".*",
		IOBackend: IOBackendPoller, RDMAEventsBackend: RDMAEventsAuto, RDMAUMADBackend: RDMAUMADAuto,
		ErrorWindow: defaultProblemWindow, FlapWindow: defaultProblemWindow,
		ErrorClasses:   []string{"link", "rdma"},
		CollectionMode: CollectionOnDemand, WatchInterval: time.Minute, SlowInterval: 5 * time.Minute,
		OnDemandTTL: 15 * time.Second, OnDemandWait: 5 * time.Second,
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
	if err := validateMode(cfg); err != nil {
		return result, err
	}
	if cfg.IOBackend != IOBackendPoller && cfg.IOBackend != IOBackendIOUring {
		return result, fmt.Errorf("invalid I/O backend %q", cfg.IOBackend)
	}
	if err := validateRDMAEventsBackend(cfg.RDMAEventsBackend); err != nil {
		return result, err
	}
	if err := validateRDMAUMADBackend(cfg.RDMAUMADBackend); err != nil {
		return result, err
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

func validateMode(cfg Config) error {
	switch cfg.CollectionMode {
	case CollectionPoll, "": // empty is poll, so configurations predating the mode stay valid
		return nil
	case CollectionOnDemand:
	default:
		return fmt.Errorf("invalid collection mode %q: want %s or %s", cfg.CollectionMode, CollectionPoll, CollectionOnDemand)
	}
	if cfg.WatchInterval <= 0 || cfg.SlowInterval <= 0 || cfg.OnDemandWait <= 0 || cfg.OnDemandTTL < 0 {
		return fmt.Errorf("watch and slow intervals and on-demand wait must be positive; on-demand TTL must be nonnegative")
	}
	if cfg.WatchInterval > math.MaxInt64/3 || cfg.SlowInterval > math.MaxInt64/3 {
		return fmt.Errorf("watch or slow interval exceeds representable freshness deadline")
	}
	return nil
}

// pollInterval is the period of a polled collector kind: StatsInterval in
// poll mode. On demand it is WatchInterval for the error and state
// collectors, SlowInterval for settings and RDMA capabilities, and zero
// (startup, events, resyncs and refreshes only) for the rest.
func (c Config) pollInterval(kind model.CollectorKind) time.Duration {
	if c.CollectionMode != CollectionOnDemand {
		return c.StatsInterval
	}
	switch kind {
	case model.CollectorNetdev, model.CollectorCarrier, model.CollectorDriver, model.CollectorPHY,
		model.CollectorRDMAState, model.CollectorRDMAErrors:
		return c.WatchInterval
	case model.CollectorSettings, model.CollectorRDMACapabilities:
		return c.SlowInterval
	default:
		return 0
	}
}

// onDemand reports whether the RDMA error counters are split from the rest.
func (c Config) onDemand() bool { return c.CollectionMode == CollectionOnDemand }

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
