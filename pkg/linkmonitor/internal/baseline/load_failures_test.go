package baseline

import (
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

type faultyReader struct {
	io.Reader
	readErr, closeErr error
	readBytes         int
}

func (f *faultyReader) Read(data []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	n, err := f.Reader.Read(data)
	f.readBytes += n
	return n, err
}
func (f *faultyReader) Close() error { return f.closeErr }

func TestLoadFailuresAndBounds(t *testing.T) {
	failure := errors.New("injected read/close failure")
	for _, tc := range []struct{ name, category, description, expected string }{
		{"directory", "negative", "directory creation denied", "permission error without acquiring lock"},
		{"lock", "negative", "lock file open denied", "permission error without reading baseline"},
		{"open", "negative", "existing baseline is unreadable", "permission error and released lock"},
		{"read", "negative", "baseline read fails", "read error and released lock"},
		{"close", "corner", "baseline read succeeds but close fails", "close error and released lock"},
		{"bounded", "boundary", "oversized reader supplies more than the cap", "read at most cap plus one byte"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			path := filepath.Join(t.TempDir(), "baseline.json")
			store := newStore(t, path)
			reader := &faultyReader{Reader: strings.NewReader(validJSON)}
			store.ops.open = func(string) (io.ReadCloser, error) { return reader, nil }
			want := failure
			switch tc.name {
			case "directory":
				store.ops.mkdirAll = func(string, fs.FileMode) error { return fs.ErrPermission }
				want = fs.ErrPermission
			case "lock":
				store.ops.lock = func(string) (io.Closer, error) { return nil, fs.ErrPermission }
				want = fs.ErrPermission
			case "open":
				store.ops.open = func(string) (io.ReadCloser, error) { return nil, fs.ErrPermission }
				want = fs.ErrPermission
			case "read":
				reader.readErr = failure
			case "close":
				reader.closeErr = failure
			case "bounded":
				reader.Reader = strings.NewReader(strings.Repeat(" ", MaxFileBytes*2))
				want = nil
			}
			_, present, err := store.LoadAndLock(t.Context())
			if err == nil || present || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("load = %v, %v", present, err)
			}
			if tc.name == "bounded" && reader.readBytes != MaxFileBytes+1 {
				t.Fatalf("read %d bytes", reader.readBytes)
			}
			lock, err := lockFile(path + ".lock")
			if err != nil {
				t.Fatalf("load leaked lifetime lock: %v", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
