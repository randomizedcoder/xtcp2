// kernel-citation-audit
//
// Audits every Go comment and string literal in the tree for the word
// `neighbour`, which is permitted only where it is part of a verbatim Linux
// kernel path.
//
// # Why this exists
//
// `neighbour` is the kernel's own spelling: net/core/neighbour.c and
// include/uapi/linux/neighbour.h are the real file names, and the value of the
// comments citing them is that they diff against a kernel tree. US spelling is
// required everywhere else in this repo, so misspell runs with locale: US and
// reports all sixteen of those citations.
//
// The exclusion that silences them can only match on misspell's *message*,
// which is identical whether the word sits inside a kernel path or inside
// British prose. Widening that exclusion to the ten files holding citations
// therefore gives up real coverage inside them — and the exclusion has to be
// widened, because the alternative is editing kernel paths into paths that do
// not exist. This audit is what makes the widening safe rather than a
// suppression: inside those files, and everywhere else, `neighbour` is allowed
// only directly after a kernel path prefix.
//
// It is strictly stronger than what misspell was giving, because it also
// catches a kernel path being "corrected" by someone running a spell fixer:
// misspell would be satisfied by `net/core/neighbor.c`, and this audit is not.
//
// The sibling exclusion for the kernel's own spelling of `preferred`, in the
// comment quoting struct ifa_cacheinfo in pkg/xtcpnl/xtcpnl_ifaddrmsg.go, needs
// no equivalent audit: it is scoped to a single word in a single file and was
// never widened, so it gives up no coverage anywhere.
//
// # What it is not
//
// This is deliberately a fifth audit tool rather than a check inside
// netlink-audit. That tool parses with parser.SkipObjectResolution and no
// parser.ParseComments, so its comment map is empty; it skips _test.go, and six
// of the sixteen citations are in tests; and it is scoped to pkg/xtcpnl, while
// the citations span six directories. Relaxing all three would change what a
// netlink-audit failure means.
//
// Exit codes: 0 = no findings, 1 = findings, 2 = internal error.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// auditedWordCst is the one word this tool owns, in one direction: the British
// spelling is what gets checked, and the US spelling `neighbor` is always fine
// and never reported.
const auditedWordCst = "neighbour"

// kernelPathPrefixesCst are the strings a legitimate citation has immediately
// before the word. Both forms in the tree are covered, and so is the URL form:
// `net/core/neighbour.c`, `include/uapi/linux/neighbour.h` and
// `https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbour.h`
// all end in one of these right before the word.
//
// They are prefixes of the word's position rather than a regexp over the line,
// so a comment that merely mentions net/core/ somewhere earlier does not
// license a British `neighbour` later in the same sentence.
var kernelPathPrefixesCst = []string{"net/core/", "linux/"}

// argvExemptions are the three files where `neighbour` is the command word a
// user types rather than prose, mapped to why.
//
// iproute2's object table carries both `neighbor` and `neighbour` (ip/ip.c),
// and matches() is prefix matching rather than fuzzy matching — "neighbour" is
// not a prefix of "neighbor" — so dropping the second spelling makes `goip
// neighbour show` report an unknown object where `ip` works.
//
// Being listed here does not exempt the whole file, only occurrences that are
// quoted as a command token; see isQuotedCommandToken. That distinction is the
// point: internal/goip/obj_neigh_test.go holds BOTH an argv string and a
// kernel citation, and internal/goip/obj_neigh.go holds only a citation, so a
// file-level exemption would stop telling those two apart and would leave
// British prose in a listed file unchecked.
var argvExemptions = map[string]string{
	"internal/goip/dispatch.go":       "holds the iproute2 object-table alias itself",
	"internal/goip/dispatch_test.go":  "names the neighbor/neighbour alias pair in the comment on the row asserting their order",
	"internal/goip/obj_neigh_test.go": "runs `goip neighbour show` end to end, so the row's argv is the string a user types",
}

// skippedDirs is the set of directory base-names auditTree skips, the same set
// metrics-audit uses, which is the other audit that runs with -root .
var skippedDirs = map[string]struct{}{
	"vendor": {},
	".git":   {},
	"gen":    {},
	"dart":   {},
	"python": {},
}

// selfDirCst is this tool's own directory, which the walk skips.
//
// The audit cannot read its own source: the word it audits appears all through
// this file's documentation, in the exemption reasons below, and in the test
// fixtures next to it — in prose, necessarily, because prose about that word is
// what this file is. Without this, a correct and clean repo reports thirteen
// findings, all of them in the tool reporting them.
//
// The alternative was to never spell the word here, assembling the constant
// from fragments and describing it indirectly. That hides the word from the
// audit and from misspell without making anything more correct, and it makes
// the one file a reader comes to for the rule the one file that will not state
// it. One skipped directory, named and reasoned, is the honest version of the
// same compromise. It is also why the misspell exclusion in both golangci
// configs lists this directory alongside the citation files.
const selfDirCst = "tools/kernel-citation-audit"

type finding struct {
	pos token.Position
	// kind is "comment" or "string literal". netlink-audit's finding carries
	// the enclosing function name in this slot, which a comment does not have:
	// most of what this tool reads is file-level documentation.
	kind string
	msg  string
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + runAudit. Extracted so tests can drive it with
// synthetic args + capture buffers without subprocessing, as the sibling audits
// do.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kernel-citation-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "directory to audit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return runAudit(*root, stdout, stderr)
}

// runAudit walks root and reports every occurrence of the audited word that is
// not a kernel citation. Returns 0 (clean), 1 (findings), or 2 (a walk or parse
// error).
func runAudit(root string, stdout, stderr io.Writer) int {
	findings, err := auditTree(root)
	if err != nil {
		fmt.Fprintf(stderr, "kernel-citation-audit: walk failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "kernel-citation-audit: scanned %s\n", root)
	if len(findings) == 0 {
		fmt.Fprintln(stdout, "kernel-citation-audit: no findings")
		return 0
	}
	for _, f := range findings {
		fmt.Fprintf(stdout, "%s: %s in a %s: %s\n", f.pos, auditedWordCst, f.kind, f.msg)
	}
	fmt.Fprintf(stderr, "kernel-citation-audit: %d finding(s)\n", len(findings))
	return 1
}

// shouldSkipFile filters non-Go and generated-proto sources.
//
// It deliberately does NOT skip _test.go, unlike both sibling audits: six of
// the sixteen kernel citations are in test files, and a test is exactly where
// prose drifts unnoticed.
func shouldSkipFile(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return true
	}
	return strings.Contains(path, ".pb.go")
}

// shouldSkipDir returns true if the directory's base name is in skippedDirs, or
// if it is this tool's own directory.
//
// The self test is a suffix match for the same reason exemptionFor is: the walk
// resolves the directory identically whether the audit was rooted at the repo
// root or at a temp tree the tests built.
func shouldSkipDir(path string) bool {
	if _, skip := skippedDirs[filepath.Base(path)]; skip {
		return true
	}
	p := filepath.ToSlash(path)
	return p == selfDirCst || strings.HasSuffix(p, "/"+selfDirCst)
}

// foldASCII lowercases A-Z and nothing else, so byte offsets in the result map
// exactly onto the input.
//
// strings.ToLower would be wrong here. It is Unicode-aware, and a handful of
// runes change byte length when folded (U+0130 LATIN CAPITAL LETTER I WITH DOT
// ABOVE becomes two runes), which would shift the reported position of every
// occurrence after one of them.
func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// isKernelCitation reports whether the occurrence at idx in text is directly
// preceded by a kernel path prefix, with that prefix itself starting at a path
// component boundary.
//
// The boundary test is not decoration. A plain suffix match accepts `linux/`
// as the tail of any longer component, so `third_party/notlinux/neighbour.h`
// would read as a kernel citation and license the word. Requiring the
// character before the prefix to be a separator — or nothing, for a citation
// that opens the text — means only a real `linux/` or `net/core/` component
// counts.
func isKernelCitation(text string, idx int) bool {
	before := text[:idx]
	for _, prefix := range kernelPathPrefixesCst {
		if !strings.HasSuffix(before, prefix) {
			continue
		}
		start := len(before) - len(prefix)
		if start == 0 || !isPathComponentChar(before[start-1]) {
			return true
		}
	}
	return false
}

// isPathComponentChar reports whether c can appear inside a single path
// component, which is how isKernelCitation decides whether a matched prefix
// really starts one. Separators, whitespace and punctuation all end a
// component; letters, digits and the three characters that appear inside
// kernel file and directory names do not.
func isPathComponentChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_', c == '-', c == '.':
		return true
	}
	return false
}

// isQuotedCommandToken reports whether the occurrence at idx opens a quoted
// token: the character immediately before it is a double quote or a backtick.
//
// That covers both argv forms in the tree — the Go string literal "neighbour",
// where the preceding byte is the literal's own opening quote, and the
// backticked `neighbour show` inside prose and inside test descriptions.
//
// It is only ever consulted for a file in argvExemptions, so quoting alone
// licenses nothing: the word still has to be in one of the three files that
// have a reason to type it.
func isQuotedCommandToken(text string, idx int) bool {
	if idx == 0 {
		return false
	}
	switch text[idx-1] {
	case '"', '`':
		return true
	}
	return false
}

// exemptionFor returns the reason path is exempt, if it is.
//
// The match is on a path suffix so an audit rooted anywhere — a temp tree in
// the tests, the repo root in the nix check — resolves the same file to the
// same exemption. The leading separator in the suffix test is what stops
// `…/notinternal/goip/dispatch.go` from matching.
func exemptionFor(path string) (string, bool) {
	p := filepath.ToSlash(path)
	for key, reason := range argvExemptions {
		if p == key || strings.HasSuffix(p, "/"+key) {
			return reason, true
		}
	}
	return "", false
}

// appendFindings appends one finding per non-citation occurrence of the audited
// word in text, which is verbatim source, so base+offset is its real position.
func appendFindings(fset *token.FileSet, path, text, kind string, base token.Pos, findings *[]finding) {
	folded := foldASCII(text)
	exempt, isExempt := exemptionFor(path)
	for off := 0; ; {
		i := strings.Index(folded[off:], auditedWordCst)
		if i < 0 {
			return
		}
		idx := off + i
		off = idx + len(auditedWordCst)

		if isKernelCitation(text, idx) {
			continue
		}
		if isExempt && isQuotedCommandToken(text, idx) {
			continue
		}
		msg := fmt.Sprintf("is not preceded by a kernel path (%s); use the US spelling, or cite the kernel file it comes from",
			strings.Join(kernelPathPrefixesCst, " or "))
		if isExempt {
			msg = fmt.Sprintf("is unquoted, and this file's exemption (%s) covers only a quoted command token", exempt)
		}
		*findings = append(*findings, finding{
			pos:  fset.Position(base + token.Pos(idx)),
			kind: kind,
			msg:  msg,
		})
	}
}

// auditFile collects findings from one parsed file's comments and string
// literals.
//
// Comments and string literals are both scanned because the word reaches the
// tree both ways: fifteen citations and the alias-pair note are comments, and
// three argv strings and a test description are literals. lit.Value is the raw
// source form including its quotes, which is what lets isQuotedCommandToken
// recognize a literal's opening quote as a quote.
func auditFile(fset *token.FileSet, file *ast.File, path string, findings *[]finding) {
	for _, group := range file.Comments {
		for _, c := range group.List {
			appendFindings(fset, path, c.Text, "comment", c.Slash, findings)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		appendFindings(fset, path, lit.Value, "string literal", lit.ValuePos, findings)
		return true
	})
}

// auditTree walks root once and returns every finding, sorted by position so
// the report is stable across filesystem ordering.
func auditTree(root string) ([]finding, error) {
	fset := token.NewFileSet()
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(path) {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		auditFile(fset, file, path, &findings)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].pos.Filename != findings[j].pos.Filename {
			return findings[i].pos.Filename < findings[j].pos.Filename
		}
		return findings[i].pos.Offset < findings[j].pos.Offset
	})
	return findings, nil
}
