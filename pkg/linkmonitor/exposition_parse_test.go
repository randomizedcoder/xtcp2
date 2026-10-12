package linkmonitor

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// metricSample is one line of the Prometheus text exposition format. VM tests
// scrape the real binary with it; there is no client-side parser in vendor.
type metricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

var errExposition = errors.New("malformed exposition line")

// parseExposition parses the text format: comments and blank lines are
// skipped, label values may use the \\, \" and \n escapes, and an optional
// trailing timestamp is ignored.
func parseExposition(text string) ([]metricSample, error) {
	var samples []metricSample
	for number, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		s, err := parseSampleLine(line)
		if err != nil {
			return nil, fmt.Errorf("%w %d: %v: %q", errExposition, number+1, err, line)
		}
		samples = append(samples, s)
	}
	return samples, nil
}

func parseSampleLine(line string) (metricSample, error) {
	end := strings.IndexAny(line, "{ \t")
	if end <= 0 {
		return metricSample{}, errors.New("no value")
	}
	s := metricSample{Name: line[:end], Labels: map[string]string{}}
	rest := line[end:]
	if rest[0] == '{' {
		var err error
		if rest, err = parseLabels(rest[1:], s.Labels); err != nil {
			return metricSample{}, err
		}
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 || len(fields) > 2 {
		return metricSample{}, errors.New("want a value and an optional timestamp")
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return metricSample{}, err
	}
	s.Value = value
	return s, nil
}

// parseLabels reads `name="value",...}` and returns the text after the brace.
func parseLabels(rest string, labels map[string]string) (string, error) {
	for {
		rest = strings.TrimLeft(rest, " ")
		if strings.HasPrefix(rest, "}") {
			return rest[1:], nil
		}
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 || len(rest) < eq+2 || rest[eq+1] != '"' {
			return "", errors.New("label without a quoted value")
		}
		name := strings.TrimSpace(rest[:eq])
		var value strings.Builder
		i := eq + 2
		for ; i < len(rest) && rest[i] != '"'; i++ {
			if rest[i] != '\\' {
				value.WriteByte(rest[i])
				continue
			}
			if i++; i == len(rest) {
				break
			}
			switch rest[i] {
			case 'n':
				value.WriteByte('\n')
			case '\\', '"':
				value.WriteByte(rest[i])
			default:
				return "", fmt.Errorf("unknown escape \\%c", rest[i])
			}
		}
		if i >= len(rest) {
			return "", errors.New("unterminated label value")
		}
		if _, dup := labels[name]; dup {
			return "", fmt.Errorf("duplicate label %q", name)
		}
		labels[name] = value.String()
		rest = strings.TrimLeft(rest[i+1:], " ")
		if strings.HasPrefix(rest, ",") {
			rest = rest[1:]
		} else if !strings.HasPrefix(rest, "}") {
			return "", errors.New("labels not separated by a comma")
		}
	}
}

// matching returns the samples named name whose labels include every
// key/value pair in pairs.
func matching(samples []metricSample, name string, pairs ...string) []metricSample {
	var out []metricSample
next:
	for _, s := range samples {
		if s.Name != name {
			continue
		}
		for i := 0; i+1 < len(pairs); i += 2 {
			if s.Labels[pairs[i]] != pairs[i+1] {
				continue next
			}
		}
		out = append(out, s)
	}
	return out
}

func TestParseExposition(t *testing.T) {
	rows := []struct {
		category, description, expected string
		text                            string
		want                            []metricSample
		wantErr                         bool
	}{
		{"positive", "sample without labels", "name and value", "up 1\n",
			[]metricSample{{"up", map[string]string{}, 1}}, false},
		{"positive", "sample with labels", "every label kept", `m{interface="eth0",collector="rings"} 0.5`,
			[]metricSample{{"m", map[string]string{"interface": "eth0", "collector": "rings"}, 0.5}}, false},
		{"positive", "HELP and TYPE comments", "skipped", "# HELP m help\n# TYPE m gauge\nm 2\n",
			[]metricSample{{"m", map[string]string{}, 2}}, false},
		{"boundary", "empty input", "no samples", "", nil, false},
		{"boundary", "trailing comma and timestamp", "accepted; timestamp ignored", `m{a="1",} 3 1700000000000`,
			[]metricSample{{"m", map[string]string{"a": "1"}, 3}}, false},
		{"boundary", "empty label value", "kept as empty", `m{ifalias=""} 1`,
			[]metricSample{{"m", map[string]string{"ifalias": ""}, 1}}, false},
		{"corner", "escaped quote, backslash and newline", "unescaped", `m{v="a\"b\\c\nd"} 1`,
			[]metricSample{{"m", map[string]string{"v": "a\"b\\c\nd"}, 1}}, false},
		{"corner", "brace and comma inside a value", "part of the value", `m{v="},{"} 1`,
			[]metricSample{{"m", map[string]string{"v": "},{"}, 1}}, false},
		{"corner", "+Inf value", "positive infinity", "m +Inf",
			[]metricSample{{"m", map[string]string{}, math.Inf(1)}}, false},
		{"negative", "unterminated label value", "error", `m{a="1} 1`, nil, true},
		{"negative", "missing value", "error", `m{a="1"}`, nil, true},
		{"negative", "value is not a number", "error", "m one", nil, true},
		{"negative", "duplicate label", "error", `m{a="1",a="2"} 1`, nil, true},
		{"negative", "unknown escape", "error", `m{a="\t"} 1`, nil, true},
		{"negative", "labels without a comma", "error", `m{a="1" b="2"} 1`, nil, true},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			got, err := parseExposition(row.text)
			if (err != nil) != row.wantErr || (err != nil && !errors.Is(err, errExposition)) {
				t.Fatalf("error=%v wantErr=%v", err, row.wantErr)
			}
			if len(got) != len(row.want) {
				t.Fatalf("samples=%+v want %+v", got, row.want)
			}
			for i := range got {
				w := row.want[i]
				if got[i].Name != w.Name || got[i].Value != w.Value || fmt.Sprint(got[i].Labels) != fmt.Sprint(w.Labels) {
					t.Fatalf("sample %d=%+v want %+v", i, got[i], w)
				}
			}
		})
	}
}

func TestMatching(t *testing.T) {
	samples := []metricSample{
		{"m", map[string]string{"interface": "eth0", "collector": "rings"}, 1},
		{"m", map[string]string{"interface": "eth1", "collector": "rings"}, 0},
		{"n", map[string]string{"interface": "eth0"}, 2},
	}
	rows := []struct {
		category, description, expected string
		name                            string
		pairs                           []string
		want                            int
	}{
		{"positive", "name and one label", "one sample", "m", []string{"interface", "eth0"}, 1},
		{"positive", "name only", "both m samples", "m", nil, 2},
		{"negative", "label value absent", "none", "m", []string{"interface", "eth9"}, 0},
		{"boundary", "label required to be empty", "none, since every m has an interface", "m", []string{"interface", ""}, 0},
		{"corner", "odd trailing key", "ignored", "n", []string{"interface", "eth0", "extra"}, 1},
	}
	for _, row := range rows {
		t.Run(row.category+"/"+row.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			if got := matching(samples, row.name, row.pairs...); len(got) != row.want {
				t.Fatalf("matched %d want %d", len(got), row.want)
			}
		})
	}
}
