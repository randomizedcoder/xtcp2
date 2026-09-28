package goipparity

import (
	"bytes"
	"strings"
	"testing"
)

// go test ./internal/goipparity/ -run TestRun
func TestRun(t *testing.T) {
	linkOut := mustRead(t, tdGuestLinkOut)
	linkShow, err := Lookup("link show")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		args        []string
		// setup writes a directory and returns the args that reference it.
		// nil means the row's args need no directory.
		setup      func(t *testing.T, dir string)
		useDir     bool
		wantCode   int
		wantStdout []string
		wantStderr []string
	}{
		{
			description: "positive: `commands` prints one line per command, exit 0",
			args:        []string{"commands"},
			wantCode:    ExitOK,
			wantStdout:  []string{"link_show\t2\tyes\tno\tlink show\tlink show"},
		},
		{
			description: "positive: `compare` on a clean triple is a pass, but exits non-zero while other commands are missing",
			args:        []string{"compare"},
			useDir:      true,
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			wantCode: ExitFailure,
			wantStdout: []string{
				"GOIP_PARITY_PASS link_show",
				"GOIP_PARITY_MISSING addr_show",
				"GOIP_PARITY_OVERALL_FAIL",
			},
		},
		{
			description: "positive: -help prints usage to stdout and exits 0",
			args:        []string{"-help"},
			wantCode:    ExitOK,
			wantStdout:  []string{"Usage: goip-parity", "commands", "compare"},
		},
		{
			description: "negative: no arguments is a usage error on stderr",
			args:        nil,
			wantCode:    ExitUsage,
			wantStderr:  []string{"Usage: goip-parity"},
		},
		{
			description: "negative: an unknown subcommand names itself and exits 1",
			args:        []string{"diff"},
			wantCode:    ExitUsage,
			wantStderr:  []string{`Subcommand "diff" is unknown`},
		},
		{
			description: "negative: `compare` with no -dir is a usage error, not an empty comparison",
			args:        []string{"compare"},
			wantCode:    ExitUsage,
			wantStderr:  []string{"compare needs -dir"},
		},
		{
			description: "negative: `compare` on an empty directory fails and says nothing was compared",
			args:        []string{"compare"},
			useDir:      true,
			setup:       func(t *testing.T, _ string) {},
			wantCode:    ExitFailure,
			wantStdout:  []string{"GOIP_PARITY_NOTHING_COMPARED", "GOIP_PARITY_OVERALL_FAIL"},
		},
		{
			description: "negative: `commands` rejects a positional argument rather than ignoring it",
			args:        []string{"commands", "link"},
			wantCode:    ExitUsage,
			wantStderr:  []string{"commands takes no arguments"},
		},
		{
			description: "negative: an unknown flag on compare is a usage error",
			args:        []string{"compare", "-nope"},
			wantCode:    ExitUsage,
			wantStderr:  []string{"flag provided but not defined"},
		},
		{
			description: "boundary: `compare -dir` on a nonexistent directory reports every command missing rather than erroring out",
			// A stat failure and an absence get the same treatment, because
			// the remedy is the same: the driver did not write where the
			// comparator is looking.
			args:       []string{"compare", "-dir", "/nonexistent/goip-parity"},
			wantCode:   ExitFailure,
			wantStdout: []string{"GOIP_PARITY_MISSING link_show", "GOIP_PARITY_OVERALL_FAIL"},
		},
		{
			description: "boundary: -no-allowlist still runs, so a reviewer can see what the allowlist is hiding",
			args:        []string{"compare", "-no-allowlist"},
			useDir:      true,
			setup: func(t *testing.T, dir string) {
				writeTriple(t, dir, linkShow, sameAll(tdGuestLink), sameAll(linkOut))
			},
			wantCode:   ExitFailure, // other commands are missing
			wantStdout: []string{"GOIP_PARITY_PASS link_show"},
		},
		{
			description: "corner: `commands` output has one line per table row and no trailing blank",
			args:        []string{"commands"},
			wantCode:    ExitOK,
		},
		{
			description: "corner: `help` as a bare word behaves like -help",
			args:        []string{"help"},
			wantCode:    ExitOK,
			wantStdout:  []string{"Usage: goip-parity"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			args := tt.args
			if tt.useDir {
				dir := t.TempDir()
				tt.setup(t, dir)
				args = append(append([]string{}, tt.args...), "-dir", dir)
			}

			var stdout, stderr bytes.Buffer
			got := Run(args, &stdout, &stderr)
			if got != tt.wantCode {
				t.Errorf("Run(%v) = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					args, got, tt.wantCode, stdout.String(), stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout lacks %q\n%s", want, stdout.String())
				}
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr lacks %q\n%s", want, stderr.String())
				}
			}
		})
	}

	t.Run("corner: commands prints exactly one line per table row", func(t *testing.T) {
		// Checked outside the table because it is an assertion about the
		// whole output rather than a substring of it, and a shell `while
		// read` over an output with a stray blank line would try to look up
		// the empty command.
		var stdout, stderr bytes.Buffer
		if got := Run([]string{"commands"}, &stdout, &stderr); got != ExitOK {
			t.Fatalf("Run = %d, want 0; stderr=%s", got, stderr.String())
		}
		lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
		if len(lines) != len(Commands()) {
			t.Errorf("got %d lines for %d commands:\n%s",
				len(lines), len(Commands()), stdout.String())
		}
		for i, l := range lines {
			if strings.TrimSpace(l) == "" {
				t.Errorf("line %d is blank", i)
			}
		}
		if stderr.Len() != 0 {
			t.Errorf("commands wrote to stderr: %q", stderr.String())
		}
	})
}
