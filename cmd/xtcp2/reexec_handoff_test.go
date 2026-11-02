package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// The six secrets, each distinct so a leak names its field.
const (
	carriedS3        = "handoff-s3"
	carriedS3ID      = "handoff-s3-id"
	carriedReadRaw   = "handoff-read-raw"
	carriedReadHMAC  = "handoff-read-hmac"
	carriedAdminRaw  = "handoff-admin-raw"
	carriedAdminHMAC = "handoff-admin-hmac"
)

var carriedValues = []string{carriedS3, carriedS3ID, carriedReadRaw, carriedReadHMAC, carriedAdminRaw, carriedAdminHMAC}

// TestSplitMergeSecrets: splitSecrets blanks exactly the secrets, and
// mergeSecrets puts them back so the exec'd image sees the config Set took.
func TestSplitMergeSecrets(t *testing.T) {
	rawToken := xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN
	tests := []struct {
		category    string
		description string
		cfg         func() *xtcp_config.XtcpConfig
		expected    bool // secrets carried, so a memfd is needed
	}{
		{"positive", "all six secrets", func() *xtcp_config.XtcpConfig {
			c := sampleConfig()
			c.S3SecretKey, c.S3AccessKey = carriedS3, carriedS3ID
			c.ListenerAuth = &xtcp_config.ListenerAuth{Mode: rawToken, RawToken: carriedReadRaw, HmacSharedKey: carriedReadHMAC}
			c.AdminListenerAuth = &xtcp_config.ListenerAuth{Mode: rawToken, RawToken: carriedAdminRaw, HmacSharedKey: carriedAdminHMAC}
			return c
		}, true},
		{"boundary", "no secrets at all", sampleConfig, false},
		{"corner", "S3 secrets with both auth blocks nil", func() *xtcp_config.XtcpConfig {
			c := sampleConfig()
			c.S3SecretKey = carriedS3
			return c
		}, true},
		{"corner", "an auth block with a mode but no secrets keeps its mode", func() *xtcp_config.XtcpConfig {
			c := sampleConfig()
			c.ListenerAuth = &xtcp_config.ListenerAuth{Mode: rawToken}
			return c
		}, false},
		{"corner", "only the admin block's HMAC key", func() *xtcp_config.XtcpConfig {
			c := sampleConfig()
			c.AdminListenerAuth = &xtcp_config.ListenerAuth{Mode: rawToken, HmacSharedKey: carriedAdminHMAC}
			return c
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected secrets carried: %v", tc.category, tc.description, tc.expected)
			cfg := tc.cfg()
			orig := proto.Clone(cfg)
			redacted, secrets := splitSecrets(cfg)
			if !proto.Equal(cfg, orig) {
				t.Error("splitSecrets changed its input")
			}
			js, err := protojson.Marshal(redacted)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range carriedValues {
				if strings.Contains(string(js), s) {
					t.Errorf("the redacted JSON holds %q", s)
				}
			}
			if got := proto.Size(secrets) > 0; got != tc.expected {
				t.Errorf("secrets carried = %v, want %v", got, tc.expected)
			}
			mergeSecrets(redacted, secrets)
			if !proto.Equal(redacted, orig) {
				t.Errorf("merged config differs from the original:\n got %v\nwant %v", redacted, orig)
			}
		})
	}
}

// TestSecretsMemfd: the memfd holds the bytes, carries every seal, and is
// not close-on-exec, so the exec'd image inherits it.
func TestSecretsMemfd(t *testing.T) {
	tests := []struct {
		category    string
		description string
		payload     []byte
		expected    string
	}{
		{"positive", "a secrets payload", []byte(`{"s3SecretKey":"x"}`), `{"s3SecretKey":"x"}`},
		{"boundary", "an empty payload is still sealed", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected contents: %q", tc.category, tc.description, tc.expected)
			f, fd, err := secretsMemfd(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			defer closeFile(t, f)
			if int(f.Fd()) != fd {
				t.Errorf("fd = %d, but the file's is %d", fd, f.Fd())
			}
			b, err := os.ReadFile("/proc/self/fd/" + strconv.Itoa(fd))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.expected {
				t.Errorf("contents %q, want %q", b, tc.expected)
			}
			seals, err := unix.FcntlInt(f.Fd(), unix.F_GET_SEALS, 0)
			if err != nil {
				t.Fatal(err)
			}
			if seals&reconfigureSecretSeals != reconfigureSecretSeals {
				t.Errorf("seals %#x, want %#x", seals, reconfigureSecretSeals)
			}
			flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
			if err != nil {
				t.Fatal(err)
			}
			if flags&unix.FD_CLOEXEC != 0 {
				t.Error("the memfd is close-on-exec, so the exec'd image would not inherit it")
			}
			if _, err := f.WriteAt([]byte("y"), 0); !errors.Is(err, unix.EPERM) {
				t.Errorf("write after sealing: %v, want EPERM", err)
			}
		})
	}
}

// TestLoadReconfigureSecrets: only a sealed memfd is read, and one that is
// read is closed and its key unset.
func TestLoadReconfigureSecrets(t *testing.T) {
	// sealed returns a descriptor for a sealed memfd holding b, for
	// loadReconfigureSecrets to take: a dup, so the test's own *os.File (and
	// its finalizer) keeps the original.
	sealed := func(t *testing.T, b string) string {
		f, _, err := secretsMemfd([]byte(b))
		if err != nil {
			t.Fatal(err)
		}
		defer closeFile(t, f)
		fd, err := unix.Dup(int(f.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		return strconv.Itoa(fd)
	}
	// owned returns f's descriptor; the test closes f.
	owned := func(t *testing.T, f *os.File) string {
		t.Cleanup(func() { closeFile(t, f) })
		return strconv.Itoa(int(f.Fd()))
	}
	memfd := func(t *testing.T, seals int) string {
		fd, err := unix.MemfdCreate("handoff-test", unix.MFD_ALLOW_SEALING|unix.MFD_CLOEXEC)
		if err != nil {
			t.Fatal(err)
		}
		f := os.NewFile(uintptr(fd), "handoff-test")
		if seals != 0 {
			if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, seals); err != nil {
				t.Fatal(err)
			}
		}
		return owned(t, f)
	}
	tests := []struct {
		category    string
		description string
		value       func(t *testing.T) string // "" leaves the key unset
		closes      bool                      // loadReconfigureSecrets takes the descriptor
		wantErr     bool
		expected    string // the S3 secret merged in, or the error's text
	}{
		{"positive", "a sealed memfd with the secrets", func(t *testing.T) string {
			return sealed(t, `{"s3SecretKey":"`+carriedS3+`"}`)
		}, true, false, carriedS3},
		{"positive", "no key: a restart with no secrets", func(*testing.T) string { return "" }, false, false, ""},
		{"corner", "a sealed memfd holding an empty config merges nothing", func(t *testing.T) string {
			return sealed(t, `{}`)
		}, true, false, ""},
		{"negative", "a value that is not a number", func(*testing.T) string { return "three" }, false, true, "is not a descriptor"},
		{"negative", "a negative number", func(*testing.T) string { return "-4" }, false, true, "is not a descriptor"},
		{"boundary", "descriptor 2, stderr, is refused", func(*testing.T) string { return "2" }, false, true, "is not a descriptor"},
		{"boundary", "a number past 31 bits", func(*testing.T) string { return "2147483648" }, false, true, "is not a descriptor"},
		{"negative", "a pipe is not a memfd", func(t *testing.T) string {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeFile(t, w) })
			return owned(t, r)
		}, false, true, "is not a memfd"},
		{"negative", "a regular file is refused", func(t *testing.T) string {
			f, err := os.Create(filepath.Join(t.TempDir(), "secrets.json"))
			if err != nil {
				t.Fatal(err)
			}
			return owned(t, f)
		}, false, true, "descriptor"},
		{"negative", "an unsealed memfd", func(t *testing.T) string { return memfd(t, 0) }, false, true, "is not sealed"},
		{"negative", "a memfd sealed against writes but not against unsealing", func(t *testing.T) string {
			return memfd(t, unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE)
		}, false, true, "is not sealed"},
		{"negative", "a sealed memfd holding malformed JSON", func(t *testing.T) string {
			return sealed(t, `{"s3SecretKey":`)
		}, true, true, "reconfigure secrets"},
		{"boundary", "a sealed memfd one byte past the bound", func(t *testing.T) string {
			return sealed(t, strings.Repeat(" ", reconfigureSecretsMaxBytes+1))
		}, true, true, "past"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q", tc.category, tc.description, tc.expected)
			v := tc.value(t)
			if v == "" {
				t.Setenv(reconfigureMemfdFDKey, "")
				if err := os.Unsetenv(reconfigureMemfdFDKey); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv(reconfigureMemfdFDKey, v)
			}
			cfg := sampleConfig()
			err := loadReconfigureSecrets(cfg)
			if tc.closes {
				fd, perr := strconv.Atoi(v)
				if perr != nil {
					t.Fatal(perr)
				}
				if _, ferr := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(ferr, unix.EBADF) {
					t.Errorf("descriptor %d still open after loading: %v", fd, ferr)
				}
			}
			if tc.wantErr {
				if err == nil || !errors.Is(err, errReconfigureSecrets) || !strings.Contains(err.Error(), tc.expected) {
					t.Fatalf("err = %v, want an errReconfigureSecrets naming %q", err, tc.expected)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.GetS3SecretKey() != tc.expected {
				t.Errorf("S3SecretKey = %q, want %q", cfg.GetS3SecretKey(), tc.expected)
			}
			if _, set := os.LookupEnv(reconfigureMemfdFDKey); set {
				t.Errorf("%s is still set after loading", reconfigureMemfdFDKey)
			}
		})
	}
}

// TestLoadReconfigureConfig_secretsWithoutConfig: a descriptor key with no
// config to merge into is a broken carry, not a first boot.
func TestLoadReconfigureConfig_secretsWithoutConfig(t *testing.T) {
	t.Setenv(reconfigureEnvKey, "")
	t.Setenv(reconfigureMemfdFDKey, "7")
	_, ok, err := loadReconfigureConfig()
	if ok || !errors.Is(err, errReconfigureSecrets) {
		t.Errorf("ok=%v err=%v, want an errReconfigureSecrets", ok, err)
	}
}

// TestReexecArgv: argv carries each flag the command line set, as
// -name=value, and parses back to the same values.
func TestReexecArgv(t *testing.T) {
	newFlags := func() *flag.FlagSet {
		fs := flag.NewFlagSet("xtcp2", flag.ContinueOnError)
		fs.String("dest", "null", "")
		fs.Bool("enabled", false, "")
		fs.Int("netlinkers", 4, "")
		fs.Duration("pollFrequency", 10*time.Second, "")
		return fs
	}
	tests := []struct {
		category    string
		description string
		args        []string
		expected    []string
	}{
		{"boundary", "no flags set", nil, []string{"/bin/xtcp2"}},
		{"positive", "several flags, in name order", []string{"-netlinkers", "8", "-dest", "kafka:127.0.0.1:9092", "-enabled"},
			[]string{"/bin/xtcp2", "-dest=kafka:127.0.0.1:9092", "-enabled=true", "-netlinkers=8"}},
		{"corner", "a value with spaces and '=' stays one argument", []string{"-dest=a b=c"},
			[]string{"/bin/xtcp2", "-dest=a b=c"}},
		{"corner", "a bool set false is carried as false", []string{"-enabled=false"},
			[]string{"/bin/xtcp2", "-enabled=false"}},
		{"corner", "a flag set to its default is still carried", []string{"-netlinkers=4"},
			[]string{"/bin/xtcp2", "-netlinkers=4"}},
		{"positive", "a duration in its canonical form", []string{"-pollFrequency=90s"},
			[]string{"/bin/xtcp2", "-pollFrequency=1m30s"}},
		{"negative", "arguments after the flags are not carried", []string{"-netlinkers=2", "stray"},
			[]string{"/bin/xtcp2", "-netlinkers=2"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %q", tc.category, tc.description, tc.expected)
			fs := newFlags()
			if err := fs.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			got := reexecArgv("/bin/xtcp2", fs)
			if !slices.Equal(got, tc.expected) {
				t.Fatalf("argv %q, want %q", got, tc.expected)
			}
			back := newFlags()
			if err := back.Parse(got[1:]); err != nil {
				t.Fatalf("argv does not parse back: %v", err)
			}
			fs.VisitAll(func(f *flag.Flag) {
				if b := back.Lookup(f.Name).Value.String(); b != f.Value.String() {
					t.Errorf("-%s parsed back as %q, want %q", f.Name, b, f.Value.String())
				}
			})
		})
	}
}

func closeFile(t *testing.T, f *os.File) {
	t.Helper()
	if err := f.Close(); err != nil {
		t.Error(err)
	}
}
