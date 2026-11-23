package dockermeta

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/randomizedcoder/xtcp2/pkg/strpolicy"
)

// errFuzzStat is what fuzzStat returns for a path containing "missing".
var errFuzzStat = errors.New("stat: no such file")

// fuzzStat resolves any SandboxKey, so the fuzzer reaches the ok path without
// guessing a fixture path. A path containing "missing" fails and one
// containing "zero" resolves to inode 0, so both reject paths stay reachable.
func fuzzStat(path string) (uint64, error) {
	switch {
	case strings.Contains(path, "missing"):
		return 0, errFuzzStat
	case strings.Contains(path, "zero"):
		return 0, nil
	}
	return 4026531000 + uint64(len(path)), nil
}

// isControlRune mirrors strpolicy's rule: C0, DEL and C1.
func isControlRune(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// checkProjected asserts the invariants every projected Container must hold,
// whatever the inspect body was.
func checkProjected(t *testing.T, in inspect, c Container) {
	t.Helper()
	for field, v := range map[string]string{"Name": c.Name, "Image": c.Image} {
		if !utf8.ValidString(v) {
			t.Fatalf("%s %q: invalid UTF-8", field, v)
		}
		if len(v) > strpolicy.MaxLen {
			t.Fatalf("%s: %d bytes, over %d", field, len(v), strpolicy.MaxLen)
		}
		for _, r := range v {
			if isControlRune(r) {
				t.Fatalf("%s %q: control rune %U kept", field, v, r)
			}
		}
	}
	if c.NetnsInode == 0 {
		t.Fatal("ok with NetnsInode 0")
	}
	if c.SandboxKey == "" {
		t.Fatal("ok with an empty SandboxKey")
	}
	if strings.HasPrefix(c.Name, "/") && !strings.HasPrefix(in.Name, "//") {
		t.Fatalf("Name %q kept a leading slash from %q", c.Name, in.Name)
	}
}

func TestProject(t *testing.T) {
	const key = "/var/run/docker/netns/aaaahash"
	mk := func(name, cfgImage, image, sandbox string) inspect {
		var in inspect
		in.ID = "id1"
		in.Name = name
		in.Image = image
		in.Config.Image = cfgImage
		in.NetworkSettings.SandboxKey = sandbox
		return in
	}
	type want struct {
		ok          bool
		name, image string
	}
	cases := []struct {
		name        string
		cat         string
		description string
		in          inspect
		stat        statFn
		expected    want
	}{
		{"basic", "P", "name /web, Config.Image nginx, SandboxKey set",
			mk("/web", "nginx", "", key), fakeStat, want{true, "web", "nginx"}},
		{"image_fallback", "P", "Config.Image empty: the top-level Image is used",
			mk("/web", "", "x", key), fakeStat, want{true, "web", "x"}},
		{"host_net", "N", "an empty SandboxKey is a host-network container",
			mk("/web", "nginx", "", ""), fakeStat, want{}},
		{"stat_error", "N", "the SandboxKey does not resolve",
			mk("/web", "nginx", "", "/var/run/docker/netns/gone"), fakeStat, want{}},
		{"inode_zero", "B", "the stat returns inode 0",
			mk("/web", "nginx", "", "/zero"), fuzzStat, want{}},
		{"name_slash_only", "B", "Name / strips to empty",
			mk("/", "nginx", "", key), fakeStat, want{true, "", "nginx"}},
		{"long_name", "N", "a 10 kB name is cut to strpolicy.MaxLen",
			mk("/"+strings.Repeat("a", 10<<10), "nginx", "", key), fakeStat,
			want{true, strings.Repeat("a", strpolicy.MaxLen), "nginx"}},
		{"hostile_name", "N", "ESC, U+0085 and an invalid byte each become U+FFFD",
			mk("/a\x1b\u0085\xffb", "nginx", "", key), fakeStat,
			want{true, "a���b", "nginx"}},
		{"double_slash", "C", "only the first leading slash is stripped",
			mk("//x", "nginx", "", key), fakeStat, want{true, "/x", "nginx"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := project(tc.in, tc.stat)
			got := want{ok, c.Name, c.Image}
			if got != tc.expected {
				t.Fatalf("%s: got %+v, expected %+v", tc.description, got, tc.expected)
			}
			if ok {
				checkProjected(t, tc.in, c)
			}
		})
	}
}

// projectionCase is one raw inspect body. The rows drive both
// TestDockerProjectionJSON (exact expectations) and FuzzDockerProjection
// (seeds).
type projectionCase struct {
	name        string
	cat         string
	description string
	body        string
	fixture     string // testdata file; overrides body
	expected    projectionWant
}

type projectionWant struct {
	unmarshalErr bool
	ok           bool
	name, image  string
	pid          int
}

func projectionCases() []projectionCase {
	const key = `"NetworkSettings":{"SandboxKey":"/var/run/docker/netns/aaaahash"}`
	return []projectionCase{
		{name: "fixture_aaaa", cat: "P", description: "testdata/inspect_aaaa.json",
			fixture: "inspect_aaaa.json", expected: projectionWant{ok: true, name: "web", image: "nginx:1.27", pid: 1234}},
		{name: "fixture_bbbb", cat: "P", description: "testdata/inspect_bbbb.json (image from the top level)",
			fixture: "inspect_bbbb.json", expected: projectionWant{ok: true, name: "db", image: "postgres:16", pid: 5678}},
		{name: "fixture_cccc_hostnet", cat: "N", description: "testdata/inspect_cccc.json has no SandboxKey",
			fixture: "inspect_cccc.json", expected: projectionWant{}},
		{name: "empty_object", cat: "B", description: "{} has no SandboxKey",
			body: `{}`, expected: projectionWant{}},
		{name: "json_null", cat: "B", description: "null decodes to the zero inspect",
			body: `null`, expected: projectionWant{}},
		{name: "not_json", cat: "N", description: "a truncated object fails to unmarshal",
			body: `{"Name":`, expected: projectionWant{unmarshalErr: true}},
		{name: "name_10k", cat: "N", description: "a 10 kB name is cut to strpolicy.MaxLen",
			body:     `{"Name":"/` + strings.Repeat("n", 10<<10) + `",` + key + `}`,
			expected: projectionWant{ok: true, name: strings.Repeat("n", strpolicy.MaxLen)}},
		{name: "name_escapes", cat: "N", description: "escaped ESC and U+0085 in the name become U+FFFD",
			body:     `{"Name":"/a\u001b[2J\u0085",` + key + `}`,
			expected: projectionWant{ok: true, name: "a�[2J�"}},
		{name: "name_invalid_utf8", cat: "N", description: "a raw 0xff byte in the JSON string becomes U+FFFD",
			body:     "{\"Name\":\"/a\xffb\"," + key + "}",
			expected: projectionWant{ok: true, name: "a�b"}},
		{name: "image_both_empty", cat: "C", description: "Config.Image and Image both empty",
			body:     `{"Name":"/x","Config":{"Image":""},"Image":"",` + key + `}`,
			expected: projectionWant{ok: true, name: "x"}},
		{name: "wrong_types", cat: "N", description: "a string Pid is an unmarshal error",
			body:     `{"State":{"Pid":"x"},` + key + `}`,
			expected: projectionWant{unmarshalErr: true}},
		{name: "huge_pid", cat: "B", description: "Pid 2^31 is kept",
			body:     `{"Name":"/p","State":{"Pid":2147483648},` + key + `}`,
			expected: projectionWant{ok: true, name: "p", pid: 2147483648}},
	}
}

// input returns the row's raw body, reading the fixture when it names one.
func (tc projectionCase) input() ([]byte, error) {
	if tc.fixture == "" {
		return []byte(tc.body), nil
	}
	return os.ReadFile(filepath.Join("testdata", tc.fixture))
}

func TestDockerProjectionJSON(t *testing.T) {
	cases := projectionCases()
	for i := range cases {
		tc := &cases[i]
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.input()
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			var in inspect
			if err := json.Unmarshal(data, &in); err != nil {
				if !tc.expected.unmarshalErr {
					t.Fatalf("%s: unmarshal: %v", tc.description, err)
				}
				return
			}
			if tc.expected.unmarshalErr {
				t.Fatalf("%s: unmarshal succeeded, expected an error", tc.description)
			}
			c, ok := project(in, fakeStat)
			got := projectionWant{ok: ok, name: c.Name, image: c.Image, pid: c.Pid}
			if got != tc.expected {
				t.Fatalf("%s: got %+v, expected %+v", tc.description, got, tc.expected)
			}
			if ok {
				checkProjected(t, in, c)
			}
		})
	}
}

// FuzzDockerProjection feeds arbitrary inspect bodies through json.Unmarshal
// and project. The Docker daemon is trusted, but the container name and image
// are the tenant's choice, so whatever they hold must come out as bounded,
// valid UTF-8 without control runes.
//
//	go test -run '^$' -fuzz '^FuzzDockerProjection$' -fuzztime=30s ./pkg/dockermeta/
func FuzzDockerProjection(f *testing.F) {
	cases := projectionCases()
	for i := range cases {
		data, err := cases[i].input()
		if err != nil {
			f.Fatalf("%s: %v", cases[i].name, err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var in inspect
		if err := json.Unmarshal(data, &in); err != nil {
			return
		}
		c, ok := project(in, fuzzStat)
		if in.NetworkSettings.SandboxKey == "" && ok {
			t.Fatal("ok for an empty SandboxKey")
		}
		if ok {
			checkProjected(t, in, c)
		}
	})
}
