package dockermeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// containerRef is one entry of GET /containers/json, the reference decode
// decodeIDs is checked against.
type containerRef struct {
	ID string `json:"Id"`
}

// refInspect is what encoding/json makes of body, the behavior decodeInspect
// must match.
func refInspect(body []byte) (inspect, error) {
	var in inspect
	err := json.NewDecoder(bytes.NewReader(body)).Decode(&in)
	return in, err
}

// checkInspectParity fails if decodeInspect and encoding/json disagree on
// body: both must accept or both reject, and accepted bodies must project
// to the same Container. An over-long daemon-set field is the one
// deliberate difference.
func checkInspectParity(t *testing.T, body []byte) (inspect, error) {
	t.Helper()
	got, werr := decodeInspect(bytes.NewReader(body), maxInspectBody)
	if errors.Is(werr, errFieldTooLong) {
		return got, werr
	}
	want, jerr := refInspect(body)
	if (werr == nil) != (jerr == nil) {
		t.Fatalf("walker err %v, encoding/json err %v, on %q", werr, jerr, clip(body))
	}
	if werr != nil {
		return got, werr
	}
	gc, gok := project(got, fuzzStat)
	wc, wok := project(want, fuzzStat)
	if gc != wc || gok != wok {
		t.Fatalf("walker %+v/%v, encoding/json %+v/%v, on %q", gc, gok, wc, wok, clip(body))
	}
	return got, nil
}

func clip(b []byte) []byte {
	if len(b) > 200 {
		return b[:200]
	}
	return b
}

func TestDecodeInspect(t *testing.T) {
	type want struct {
		err           bool
		name, image   string
		pid           int
		sandbox, cImg string
	}
	deep := strings.Repeat(`{"a":`, maxWalkDepth) + "1" + strings.Repeat("}", maxWalkDepth)
	cases := []struct {
		name        string
		cat         string
		description string
		body        string
		expected    want
	}{
		{"basic", "P", "every kept field",
			`{"Id":"i","Name":"/n","Image":"img","Config":{"Image":"c"},"State":{"Pid":7},"NetworkSettings":{"SandboxKey":"/k"}}`,
			want{name: "/n", image: "img", cImg: "c", pid: 7, sandbox: "/k"}},
		{"skips_unknown", "P", "unknown fields of every type are skipped",
			`{"A":[1,-2.5e+3,true,false,null,{"b":[{}]}],"Name":"n","Z":"é\n"}`, want{name: "n"}},
		{"case_insensitive", "C", "keys match case-insensitively, as encoding/json does",
			`{"name":"a","STATE":{"pid":3}}`, want{name: "a", pid: 3}},
		{"escaped_key", "C", `"Name" is the key Name`,
			`{"Name":"a"}`, want{name: "a"}},
		{"kelvin_key", "C", "U+212A KELVIN SIGN folds to k in SandboxKey (parity row)",
			`{"NetworkSettings":{"SandboxKey":"/k"}}`, want{sandbox: "/k"}},
		{"duplicate_last_wins", "C", "the last duplicate key wins",
			`{"Name":"a","Name":"b"}`, want{name: "b"}},
		{"null_keeps", "C", "a later null leaves an object field as it was",
			`{"Config":{"Image":"c"},"Config":null,"Name":null}`, want{cImg: "c"}},
		{"top_null", "B", "a top-level null is the zero value",
			`null`, want{}},
		{"trailing", "B", "data after the top-level value is ignored",
			`{"Name":"a"} junk`, want{name: "a"}},
		{"depth_at_limit", "B", "nesting of exactly 10000 is accepted",
			deep, want{}},
		{"depth_over_limit", "N", "nesting of 10001 is an error",
			`{"a":` + deep + `}`, want{err: true}},
		{"pid_float", "N", "a fractional Pid is a type error",
			`{"State":{"Pid":1.5}}`, want{err: true}},
		{"pid_exp", "N", "an exponent Pid is a type error",
			`{"State":{"Pid":1e3}}`, want{err: true}},
		{"pid_overflow", "N", "a Pid past int64 is a type error",
			`{"State":{"Pid":99999999999999999999}}`, want{err: true}},
		{"pid_string", "N", "a string Pid is a type error",
			`{"State":{"Pid":"1"}}`, want{err: true}},
		{"leading_zero", "N", "01 is not JSON, even in a skipped field",
			`{"X":01}`, want{err: true}},
		{"bad_escape", "N", `\x is not a JSON escape`,
			`{"X":"\x"}`, want{err: true}},
		{"raw_control", "N", "a raw control byte in a skipped string is an error",
			"{\"X\":\"a\x01b\"}", want{err: true}},
		{"bad_literal", "N", "tru is not a literal",
			`{"X":tru}`, want{err: true}},
		{"wrong_top", "N", "a top-level array is a type error",
			`[1]`, want{err: true}},
		{"config_string", "N", "Config as a string is a type error",
			`{"Config":"x"}`, want{err: true}},
		{"truncated", "N", "a body cut mid-object",
			`{"Name":"a"`, want{err: true}},
		{"empty", "B", "an empty body",
			``, want{err: true}},
		{"invalid_utf8_name", "C", "invalid UTF-8 becomes U+FFFD, as encoding/json does",
			"{\"Name\":\"a\xffb\"}", want{name: "a�b"}},
		{"lone_surrogate", "C", `a lone \ud800 becomes U+FFFD`,
			`{"Name":"\ud800"}`, want{name: "�"}},
		{"long_name_cut", "B", "a 1 MiB name is cut to maxTextRaw raw bytes; Clean's result is unchanged",
			`{"Name":"` + strings.Repeat("n", 1<<20) + `"}`, want{name: strings.Repeat("n", maxTextRaw)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := checkInspectParity(t, []byte(tc.body))
			got := want{err: err != nil, name: in.Name, image: in.Image, pid: in.State.Pid,
				sandbox: in.NetworkSettings.SandboxKey, cImg: in.Config.Image}
			if err != nil {
				got = want{err: true}
			}
			if got != tc.expected {
				t.Fatalf("%s: got %+v (err %v), expected %+v", tc.description, got, err, tc.expected)
			}
		})
	}
}

// idCases drive TestDecodeIDs and seed FuzzDecodeIDs.
func idCases() []struct {
	name, cat, description, body string
	expected                     []string // nil with err
	err                          bool
} {
	long := strings.Repeat("a", maxIDRaw+1)
	return []struct {
		name, cat, description, body string
		expected                     []string
		err                          bool
	}{
		{"two", "P", "two entries with labels", `[{"Id":"a","Labels":{"k":"v"}},{"Id":"b"}]`, []string{"a", "b"}, false},
		{"empty_array", "B", "no containers", `[]`, nil, false},
		{"null", "B", "a null list", `null`, nil, false},
		{"null_entry", "C", "a null entry is an empty Id, as encoding/json makes it", `[null,{"Id":"b"}]`, []string{"", "b"}, false},
		{"no_id", "C", "an entry without Id", `[{"Names":["/x"]}]`, []string{""}, false},
		{"long_id_dropped", "C", "an Id over maxIDRaw drops that entry only", `[{"Id":"` + long + `"},{"Id":"b"}]`, []string{"b"}, false},
		{"object_top", "N", "an object is not a list", `{"Id":"a"}`, nil, true},
		{"number_entry", "N", "a number entry is a type error", `[1]`, nil, true},
		{"id_number", "N", "a numeric Id is a type error", `[{"Id":1}]`, nil, true},
		{"truncated", "N", "a list cut mid-entry", `[{"Id":"a"},{"Id"`, nil, true},
	}
}

// checkIDsParity compares decodeIDs with encoding/json on body, except for
// the entries decodeIDs drops for an over-long Id.
func checkIDsParity(t *testing.T, body []byte) ([]string, error) {
	t.Helper()
	got, _, werr := decodeIDs(bytes.NewReader(body), noLimit, maxListEntry)
	var refs []containerRef
	jerr := json.NewDecoder(bytes.NewReader(body)).Decode(&refs)
	if (werr == nil) != (jerr == nil) {
		t.Fatalf("walker err %v, encoding/json err %v, on %q", werr, jerr, clip(body))
	}
	if werr != nil {
		return nil, werr
	}
	// Walk both in step: every Id must match, except that decodeIDs may
	// drop one whose raw form was over maxIDRaw (so its decoded form is over
	// maxIDRaw/6 bytes).
	i := 0
	for _, r := range refs {
		if i < len(got) && got[i] == r.ID {
			i++
			continue
		}
		if len(r.ID) <= maxIDRaw/6 {
			t.Fatalf("walker %q dropped or changed %q, on %q", got, r.ID, clip(body))
		}
	}
	if i != len(got) {
		t.Fatalf("walker %q has Ids encoding/json does not, on %q", got[i:], clip(body))
	}
	return got, nil
}

func TestDecodeIDs(t *testing.T) {
	for _, tc := range idCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkIDsParity(t, []byte(tc.body))
			if (err != nil) != tc.err {
				t.Fatalf("%s: err %v, expected err %v", tc.description, err, tc.err)
			}
			if strings.Join(got, ",") != strings.Join(tc.expected, ",") || len(got) != len(tc.expected) {
				t.Fatalf("%s: got %q, expected %q", tc.description, got, tc.expected)
			}
		})
	}
}

// FuzzDecodeIDs holds decodeIDs to encoding/json on arbitrary list bodies.
//
//	go test -run '^$' -fuzz '^FuzzDecodeIDs$' -fuzztime=30s ./pkg/dockermeta/
func FuzzDecodeIDs(f *testing.F) {
	for _, tc := range idCases() {
		f.Add([]byte(tc.body))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = checkIDsParity(t, data)
	})
}
