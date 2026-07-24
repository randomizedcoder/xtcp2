package xtcp

import (
	"reflect"
	"testing"
)

// withEmptyLibraryDefaults swaps libraryDefaultDest for an empty map for the
// duration of the test and restores it afterwards. The default test binary
// compiles in no library destinations (no dest_* build tags), so the map is
// already empty, but saving/restoring keeps the tests independent of build
// tags and of one another.
func withEmptyLibraryDefaults(t *testing.T) {
	t.Helper()
	destRegistryMu.Lock()
	saved := libraryDefaultDest
	libraryDefaultDest = map[string]string{}
	destRegistryMu.Unlock()
	t.Cleanup(func() {
		destRegistryMu.Lock()
		libraryDefaultDest = saved
		destRegistryMu.Unlock()
	})
}

func TestLibraryDefaultDestRegistry(t *testing.T) {
	withEmptyLibraryDefaults(t)

	// Zero library destinations: no schemes, no defaults.
	if got := CompiledInLibrarySchemes(); len(got) != 0 {
		t.Fatalf("CompiledInLibrarySchemes on empty registry = %v, want empty", got)
	}
	if got := LibraryDefaultDest("kafka"); got != "" {
		t.Fatalf("LibraryDefaultDest(kafka) on empty registry = %q, want \"\"", got)
	}

	// One library destination.
	RegisterLibraryDefaultDest("s3parquet", "s3parquet:")
	if got, want := CompiledInLibrarySchemes(), []string{"s3parquet"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CompiledInLibrarySchemes = %v, want %v", got, want)
	}
	if got, want := LibraryDefaultDest("s3parquet"), "s3parquet:"; got != want {
		t.Fatalf("LibraryDefaultDest(s3parquet) = %q, want %q", got, want)
	}

	// Two library destinations: result is sorted.
	RegisterLibraryDefaultDest("kafka", "kafka:redpanda-0:9092")
	if got, want := CompiledInLibrarySchemes(), []string{"kafka", "s3parquet"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CompiledInLibrarySchemes = %v, want sorted %v", got, want)
	}
}
