//go:build dest_s3parquet

// Command xtcp2-golden works with golden captures of xtcp2's Parquet
// records (see pkg/goldenrec and nix/README.md, "Golden records"):
//
//	xtcp2-golden scrub in.parquet out.parquet
//	xtcp2-golden normalize in.parquet > records.jsonl
//	xtcp2-golden diff [-columns re] [-limit 50] golden.parquet candidate.parquet
//	xtcp2-golden check dir
//	xtcp2-golden profile in.parquet > profile.json
//	xtcp2-golden shape-diff a.parquet|a.json b.parquet|b.json
//
// profile reduces a capture to its shape (populated columns, enum values,
// families, kinds of namespace) and records no identity, so a profile can
// leave the host it was taken on. shape-diff compares two shapes, for
// captures whose workloads differ.
//
// diff, check and shape-diff exit 0 when they find nothing, 1 when they do, and 2 on an
// error.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/parquet-go/parquet-go"

	"github.com/randomizedcoder/xtcp2/pkg/goldenrec"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

const (
	exitSame  = 0
	exitDiff  = 1
	exitError = 2
)

// errFound is returned when diff or check finds something.
var errFound = errors.New("differences found")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	err := dispatch(args, stdout, stderr)
	switch {
	case err == nil:
		return exitSame
	case errors.Is(err, errFound):
		return exitDiff
	default:
		fmt.Fprintln(stderr, "xtcp2-golden:", err)
		return exitError
	}
}

const usage = "usage: xtcp2-golden scrub IN OUT | normalize IN | diff [-columns re] [-limit n] GOLDEN CANDIDATE | check DIR | profile IN | shape-diff A B"

func dispatch(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "scrub":
		if len(args) != 2 {
			return errors.New(usage)
		}
		return scrub(args[0], args[1], stderr)
	case "normalize":
		if len(args) != 1 {
			return errors.New(usage)
		}
		return normalize(args[0], stdout)
	case "diff":
		return diff(args, stdout, stderr)
	case "check":
		if len(args) != 1 {
			return errors.New(usage)
		}
		return check(args[0], stdout)
	case "profile":
		if len(args) != 1 {
			return errors.New(usage)
		}
		return profile(args[0], stdout)
	case "shape-diff":
		if len(args) != 2 {
			return errors.New(usage)
		}
		return shapeDiff(args[0], args[1], stdout)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

// readFile reads name in dir through os.Root, which keeps it inside dir.
func readFile(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	b, err := root.ReadFile(name)
	return b, errors.Join(err, root.Close())
}

func readRows(path string) ([]xtcp.ParquetRow, error) {
	b, err := readFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	f, err := parquet.OpenFile(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := sameSchema(f.Schema()); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rows, err := parquet.Read[xtcp.ParquetRow](bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rows, nil
}

// sameSchema rejects a file whose columns are not ParquetRow's: a capture
// from before a schema change reads with zeros in the new columns, which
// would compare as a real difference, or hide one.
func sameSchema(s *parquet.Schema) error {
	have := make([]string, 0, len(s.Columns()))
	for _, p := range s.Columns() {
		have = append(have, p[0])
	}
	wantCols := parquet.SchemaOf(xtcp.ParquetRow{}).Columns()
	want := make([]string, 0, len(wantCols))
	for _, p := range wantCols {
		want = append(want, p[0])
	}
	slices.Sort(have)
	slices.Sort(want)
	if !slices.Equal(have, want) {
		return errors.New("schema drift: the file's columns are not ParquetRow's; recapture with nix run .#capture-golden-records")
	}
	return nil
}

func scrub(in, out string, stderr io.Writer) error {
	rows, err := readRows(in)
	if err != nil {
		return err
	}
	rep, err := goldenrec.Scrub(rows, goldenrec.Columns)
	if err != nil {
		return err
	}
	for _, s := range rep.Invalid {
		fmt.Fprintln(stderr, "xtcp2-golden: left as is:", s)
	}
	return parquet.WriteFile(out, rows)
}

// normalizedJSONL is one JSON object per row, keys sorted.
func normalizedJSONL(rows []xtcp.ParquetRow) ([]byte, error) {
	n, err := goldenrec.Normalize(rows, goldenrec.Columns)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range n {
		if err := enc.Encode(r); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func normalize(in string, stdout io.Writer) error {
	rows, err := readRows(in)
	if err != nil {
		return err
	}
	b, err := normalizedJSONL(rows)
	if err != nil {
		return err
	}
	_, err = stdout.Write(b)
	return err
}

func diff(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	columns := fs.String("columns", "", "compare only columns matching this regular expression")
	limit := fs.Int("limit", 50, "print at most this many differences")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New(usage)
	}
	var sides [2][]goldenrec.Row
	for i, p := range fs.Args() {
		rows, err := readRows(p)
		if err != nil {
			return err
		}
		if sides[i], err = goldenrec.Normalize(rows, goldenrec.Columns); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	diffs, err := goldenrec.Diff(sides[0], sides[1], goldenrec.Columns, goldenrec.Options{Columns: *columns})
	if err != nil {
		return err
	}
	if len(diffs) == 0 {
		return nil
	}
	fmt.Fprint(stdout, goldenrec.Format(diffs, *limit))
	return fmt.Errorf("%w: %d", errFound, len(diffs))
}

// manifest is the part of manifest.json check reads.
type manifest struct {
	Rows int `json:"rows"`
}

// check verifies a fixture directory: records.jsonl is records.parquet
// normalized, records.parquet is scrubbed, manifest.json counts its rows,
// and the capture meets ../coverage.json when there is one.
func check(dir string, stdout io.Writer) error {
	rows, err := readRows(filepath.Join(dir, "records.parquet"))
	if err != nil {
		return err
	}
	var problems []string
	want, err := normalizedJSONL(rows)
	if err != nil {
		return err
	}
	got, err := readFile(dir, "records.jsonl")
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		problems = append(problems, "records.jsonl is not records.parquet normalized: "+firstDiffLine(got, want))
	}
	bad, err := goldenrec.CheckScrubbed(rows, goldenrec.Columns)
	if err != nil {
		return err
	}
	problems = append(problems, bad...)
	mb, err := readFile(dir, "manifest.json")
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	if m.Rows != len(rows) {
		problems = append(problems, fmt.Sprintf("manifest.json says %d rows, records.parquet has %d", m.Rows, len(rows)))
	}
	cov, err := coverageProblems(dir, rows, mb)
	if err != nil {
		return err
	}
	problems = append(problems, cov...)
	if len(problems) == 0 {
		return nil
	}
	for _, p := range problems {
		fmt.Fprintln(stdout, p)
	}
	return fmt.Errorf("%w: %d", errFound, len(problems))
}

// coverageFile is shared by every kernel's fixture, next to their
// directories.
const coverageFile = "coverage.json"

// coverageProblems checks rows against the coverage.json beside dir, with
// "@manifest.<key>" values taken from manifest (the fixture's
// manifest.json). No coverage.json, no problems.
func coverageProblems(dir string, rows []xtcp.ParquetRow, manifest []byte) ([]string, error) {
	b, err := readFile(filepath.Dir(filepath.Clean(dir)), coverageFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c goldenrec.Coverage
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", coverageFile, err)
	}
	p, err := goldenrec.ProfileOf(rows, goldenrec.Columns)
	if err != nil {
		return nil, err
	}
	refs, err := manifestRefs(manifest)
	if err != nil {
		return nil, err
	}
	return goldenrec.CheckCoverage(p, c, refs), nil
}

// manifestRefs is every string list at the top of manifest.json, by key.
func manifestRefs(manifest []byte) (map[string][]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &raw); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	refs := make(map[string][]string)
	for k, v := range raw {
		var list []string
		if json.Unmarshal(v, &list) == nil && list != nil {
			refs[k] = list
		}
	}
	return refs, nil
}

// firstDiffLine names the first line where got and want differ.
func firstDiffLine(got, want []byte) string {
	g, w := bytes.Split(got, []byte("\n")), bytes.Split(want, []byte("\n"))
	for i := range max(len(g), len(w)) {
		var gl, wl []byte
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if !bytes.Equal(gl, wl) {
			return fmt.Sprintf("line %d:%s", i+1, lineDiff(gl, wl))
		}
	}
	return "no differing line"
}

// lineDiff names the fields two normalized rows differ in, or shows both
// lines whole when either is not a row.
func lineDiff(got, want []byte) string {
	var g, w map[string]string
	if json.Unmarshal(got, &g) != nil || json.Unmarshal(want, &w) != nil {
		return fmt.Sprintf("\n  committed  %s\n  normalized %s", got, want)
	}
	keys := slices.Sorted(maps.Keys(g))
	for k := range w {
		if _, ok := g[k]; !ok {
			keys = append(keys, k)
		}
	}
	var b strings.Builder
	for _, k := range keys {
		gv, gok := g[k]
		wv, wok := w[k]
		switch {
		case !gok:
			fmt.Fprintf(&b, "\n  %s: missing, normalized %q", k, wv)
		case !wok:
			fmt.Fprintf(&b, "\n  %s: committed %q, not a column", k, gv)
		case gv != wv:
			fmt.Fprintf(&b, "\n  %s: committed %q, normalized %q", k, gv, wv)
		}
	}
	return b.String()
}

// loadProfile profiles a .parquet capture, or reads a profile written by
// xtcp2-golden profile (a .json file).
func loadProfile(path string) (goldenrec.Profile, error) {
	var p goldenrec.Profile
	if strings.HasSuffix(path, ".json") {
		b, err := readFile(filepath.Dir(path), filepath.Base(path))
		if err != nil {
			return p, err
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return p, fmt.Errorf("%s: %w", path, err)
		}
		return p, nil
	}
	rows, err := readRows(path)
	if err != nil {
		return p, err
	}
	return goldenrec.ProfileOf(rows, goldenrec.Columns)
}

func profile(in string, stdout io.Writer) error {
	p, err := loadProfile(in)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(p)
}

func shapeDiff(a, b string, stdout io.Writer) error {
	pa, err := loadProfile(a)
	if err != nil {
		return err
	}
	pb, err := loadProfile(b)
	if err != nil {
		return err
	}
	findings := goldenrec.DiffProfiles(pa, pb)
	if len(findings) == 0 {
		return nil
	}
	fmt.Fprintf(stdout, "A = %s\nB = %s\n", a, b)
	for _, f := range findings {
		fmt.Fprintln(stdout, f)
	}
	return errFound
}
