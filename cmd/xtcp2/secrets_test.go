package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// secretTestFlagSet registers the credential flags on a private FlagSet.
func secretTestFlagSet(args ...string) (*flag.FlagSet, error) {
	fs := flag.NewFlagSet("xtcp2", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, s := range secretFlags {
		fs.String(s.flag, "", "")
	}
	fs.String("dest", "null", "")
	return fs, fs.Parse(args)
}

// TestCheckSecretFlags is Test FB's command-line half: a credential flag
// warns on a development build and is refused, exit 64, on a production one.
func TestCheckSecretFlags(t *testing.T) {
	cases := []struct {
		name        string
		category    string
		description string
		args        []string
		env         map[string]string
		production  bool
		expected    string // "ok", "warn", "refuse"
	}{
		{"no_secret_flags", "positive", "no credential on the command line", []string{"-dest", "null"}, nil, true, "ok"},
		{"dev_s3_secret_warns", "negative", "dev build warns for the S3 flags (silent before)", []string{"-s3SecretKey", "x"}, nil, false, "warn"},
		{"prod_s3_secret_refused", "negative", "production refuses -s3SecretKey", []string{"-s3SecretKey", "x"}, nil, true, "refuse"},
		{"prod_listener_token_refused", "negative", "production refuses a listener token flag", []string{"-listenerRawToken", "t"}, nil, true, "refuse"},
		{"empty_value", "boundary", "an explicitly empty flag carries no secret", []string{"-s3AccessKey", ""}, nil, true, "ok"},
		{"flag_and_env_both", "corner", "the env var being set too no longer hides the flag", []string{"-listenerRawToken", "t"},
			map[string]string{"LISTENER_RAW_TOKEN": "t"}, false, "warn"},
		{"env_only", "corner", "a credential from the environment alone is fine", nil,
			map[string]string{"S3_SECRET_KEY": "x"}, true, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			fs, err := secretTestFlagSet(tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(prev) })

			err = checkSecretFlags(fs, tc.production)
			got := "ok"
			switch {
			case err != nil:
				got = "refuse"
			case strings.Contains(buf.String(), "WARNING"):
				got = "warn"
			}
			if got != tc.expected {
				t.Fatalf("%s: got %s, want %s (err %v, log %q)", tc.description, got, tc.expected, err, buf.String())
			}
			if err != nil {
				if exitCodeFor(err) != exitUsage {
					t.Errorf("%s: exit %d, want %d", tc.description, exitCodeFor(err), exitUsage)
				}
				if !strings.Contains(err.Error(), "_FILE") {
					t.Errorf("%s: error does not name the _FILE variable: %v", tc.description, err)
				}
			}
		})
	}
}

// TestReadSecretFile covers the _FILE reader: trimmed contents on success,
// and a usage error (fatal at startup) for a file it cannot safely use.
func TestReadSecretFile(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, body string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := mk("good", "tok\n", 0o400)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		category    string
		description string
		path        string
		expected    string // trimmed value, or "" with an error
		wantErr     bool
	}{
		{"owner_only", "positive", "mode 0400 is read and trimmed", good, "tok", false},
		{"owner_rw", "positive", "mode 0600 is fine", mk("rw", "  k  ", 0o600), "k", false},
		{"secret_file_mode", "negative", "mode 0644 (world-readable) is refused", mk("wr", "k", 0o644), "", true},
		{"group_readable", "boundary", "mode 0640: one group bit is enough to refuse", mk("gr", "k", 0o640), "", true},
		{"secret_file_missing", "negative", "a missing file is fatal (was silent)", filepath.Join(dir, "nope"), "", true},
		{"directory", "corner", "a directory is not a secret file", dir, "", true},
		{"symlink", "corner", "a symlink to a 0400 file is followed (Kubernetes mounts)", link, "tok", false},
		{"empty_file", "corner", "an empty 0400 file yields an empty value", mk("empty", "", 0o400), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			got, err := readSecretFile("TEST_FILE", tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("%s: err = %v, want error %v", tc.description, err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, errUsage) {
				t.Errorf("%s: error is not a usage error: %v", tc.description, err)
			}
			if got != tc.expected {
				t.Errorf("%s: value = %q, want %q", tc.description, got, tc.expected)
			}
		})
	}
}

// TestApplySecretFiles: a bad _FILE stops startup instead of leaving the
// credential empty, and a good one wins over the inline variable.
func TestApplySecretFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "tok")
	if err := os.WriteFile(good, []byte("from-file\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "open")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		category    string
		description string
		env         map[string]string
		expected    string // listener raw token after apply; "ERR" for an error
	}{
		{"file_wins", "positive", "the file replaces the inline value", map[string]string{"LISTENER_RAW_TOKEN_FILE": good}, "from-file"},
		{"unset", "boundary", "no _FILE leaves the inline value", map[string]string{}, "inline"},
		{"empty_path", "corner", "an empty _FILE value is ignored", map[string]string{"LISTENER_RAW_TOKEN_FILE": ""}, "inline"},
		{"bad_mode", "negative", "a world-readable file stops startup", map[string]string{"LISTENER_RAW_TOKEN_FILE": bad}, "ERR"},
		{"s3_missing", "negative", "a missing S3 key file stops startup", map[string]string{"S3_SECRET_KEY_FILE": filepath.Join(dir, "none")}, "ERR"},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			for _, s := range append(listenerSecretFiles, s3SecretFiles...) {
				t.Setenv(s.env, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c := &xtcp_config.XtcpConfig{ListenerAuth: &xtcp_config.ListenerAuth{RawToken: "inline"}}
			err := applySecretFiles(c)
			got := c.GetListenerAuth().GetRawToken()
			if err != nil {
				got = "ERR"
			}
			if got != tc.expected {
				t.Errorf("%s: got %q (err %v), want %q", tc.description, got, err, tc.expected)
			}
		})
	}
}
