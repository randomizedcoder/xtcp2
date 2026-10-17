package rdmacapture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestName is the manifest's file name in a capture directory.
const ManifestName = "manifest.json"

// streamDirs maps each top-level directory of a capture to its stream type.
var streamDirs = map[string]string{
	"async": "async",
	"nldev": "nldev",
	"nlmon": "rtnl",
	"sysfs": "sysfs",
	"umad":  "umad",
	"verbs": "verbs",
}

// File is one captured file.
type File struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Stream  string `json:"stream"`
	Command string `json:"command,omitempty"`
	Trigger string `json:"trigger,omitempty"`
}

// Manifest describes one capture directory.
type Manifest struct {
	Source    string            `json:"source"`
	Uname     string            `json:"uname"`
	Driver    string            `json:"driver"`
	Firmware  string            `json:"firmware"`
	RDMACore  string            `json:"rdma_core"`
	Files     []File            `json:"files"`
	Redaction map[string]string `json:"redaction"`
}

// IndexEntry is one line of the capture script's index.tsv: the row's path
// without its extension, so it names the .strace, .out, .err and .rc of one
// command together.
type IndexEntry struct {
	Stem    string
	Command string
	Trigger string
}

// ErrManifest reports a capture directory that does not match its manifest.
var ErrManifest = errors.New("rdmacapture: manifest mismatch")

// ReadIndex parses index.tsv: "<stem>\t<command>\t<trigger>" per line.
func ReadIndex(data []byte) (map[string]IndexEntry, error) {
	out := map[string]IndexEntry{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if text == "" {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 3 || fields[0] == "" {
			return nil, fmt.Errorf("%w: index line %d: want 3 tab-separated fields, got %q", ErrManifest, line, text)
		}
		if _, dup := out[fields[0]]; dup {
			return nil, fmt.Errorf("%w: index line %d: %s listed twice", ErrManifest, line, fields[0])
		}
		out[fields[0]] = IndexEntry{Stem: fields[0], Command: fields[1], Trigger: fields[2]}
	}
	return out, scanner.Err()
}

func stem(p string) string {
	return strings.TrimSuffix(p, path.Ext(p))
}

// streamOf returns a captured file's stream from its top-level directory.
func streamOf(p string) (string, bool) {
	top, _, found := strings.Cut(p, "/")
	if !found {
		return "", false
	}
	s, ok := streamDirs[top]
	return s, ok
}

// listFiles returns every regular file under dir as a slash path, skipping the
// manifest and golden files.
func listFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestName || strings.HasSuffix(rel, ".golden.json") {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%w: %s is not a regular file", ErrManifest, rel)
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func sha256File(p string) (string, error) {
	data, err := os.ReadFile(p) // #nosec G304 -- paths come from walking the capture directory
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// redactFile rewrites one file under root with its real values replaced and
// returns the sha256 of the result. root keeps every access inside the
// capture directory.
func redactFile(root *os.Root, rel string, r *Redactor) (string, error) {
	name := filepath.FromSlash(rel)
	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}
	red, err := r.Redact(data, FormatFor(rel))
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if !bytes.Equal(red, data) {
		if err := root.WriteFile(name, red, 0o600); err != nil {
			return "", err
		}
	}
	sum := sha256.Sum256(red)
	return hex.EncodeToString(sum[:]), nil
}

// Build redacts every stream file under dir in place and returns its
// manifest. Files outside a stream directory are an error, so nothing the
// script wrote by mistake is committed unlisted.
func Build(dir string, base Manifest, index map[string]IndexEntry, r *Redactor) (Manifest, error) {
	files, err := listFiles(dir)
	if err != nil {
		return Manifest{}, err
	}
	if len(files) == 0 {
		return Manifest{}, fmt.Errorf("%w: %s holds no captured files", ErrManifest, dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Manifest{}, err
	}
	m, err := build(root, files, base, index, r)
	if err = errors.Join(err, root.Close()); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// build is Build after the listing, with every file access under root.
func build(root *os.Root, files []string, base Manifest, index map[string]IndexEntry, r *Redactor) (Manifest, error) {
	m := base
	m.Files = nil
	m.Redaction = r.Map()
	used := map[string]bool{}
	for _, rel := range files {
		stream, ok := streamOf(rel)
		if !ok {
			return Manifest{}, fmt.Errorf("%w: %s is not under a stream directory", ErrManifest, rel)
		}
		sum, err := redactFile(root, rel, r)
		if err != nil {
			return Manifest{}, err
		}
		f := File{Path: rel, SHA256: sum, Stream: stream}
		if e, ok := index[stem(rel)]; ok {
			f.Command, f.Trigger = e.Command, e.Trigger
			used[e.Stem] = true
		}
		m.Files = append(m.Files, f)
	}
	for s := range index {
		if !used[s] {
			return Manifest{}, fmt.Errorf("%w: index names %s, which wrote no file", ErrManifest, s)
		}
	}
	return m, nil
}

// Write writes the manifest into dir.
func Write(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ManifestName), append(data, '\n'), 0o600)
}

// Verify checks dir against its manifest: every listed file exists with the
// recorded sha256, every file present is listed, and the paths are clean,
// sorted and unique.
func Verify(dir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName)) // #nosec G304 -- dir is a capture directory chosen by the caller
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %s: %v", ErrManifest, dir, err)
	}
	if m.Source == "" || len(m.Files) == 0 {
		return Manifest{}, fmt.Errorf("%w: %s: no source or no files", ErrManifest, dir)
	}
	var problems []string
	listed := map[string]bool{}
	for i, f := range m.Files {
		if !filepath.IsLocal(f.Path) || path.Clean(f.Path) != f.Path {
			problems = append(problems, fmt.Sprintf("%s: not a clean relative path", f.Path))
			continue
		}
		if i > 0 && m.Files[i-1].Path >= f.Path {
			problems = append(problems, fmt.Sprintf("%s: out of order or listed twice", f.Path))
		}
		if s, ok := streamOf(f.Path); !ok || s != f.Stream {
			problems = append(problems, fmt.Sprintf("%s: stream %q does not match its directory", f.Path, f.Stream))
		}
		listed[f.Path] = true
		sum, err := sha256File(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f.Path, err))
			continue
		}
		if sum != f.SHA256 {
			problems = append(problems, fmt.Sprintf("%s: sha256 %s, manifest %s", f.Path, sum, f.SHA256))
		}
	}
	present, err := listFiles(dir)
	if err != nil {
		return Manifest{}, err
	}
	for _, p := range present {
		if !listed[p] {
			problems = append(problems, fmt.Sprintf("%s: present but not in the manifest", p))
		}
	}
	if len(problems) > 0 {
		return m, fmt.Errorf("%w: %s:\n  %s", ErrManifest, dir, strings.Join(problems, "\n  "))
	}
	return m, nil
}
