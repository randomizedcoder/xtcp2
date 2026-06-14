package main

import (
	"testing"
)

// namespaceName composes the canonical "<base><index>" name.
func TestNamespaceName(t *testing.T) {
	cases := []struct {
		idx  int
		want string
	}{
		{0, "ns0"},
		{1, "ns1"},
		{999, "ns999"},
		{initialNamespaces - 1, "ns999"},
	}
	for _, tc := range cases {
		if got := namespaceName(tc.idx); got != tc.want {
			t.Errorf("namespaceName(%d) = %q, want %q", tc.idx, got, tc.want)
		}
	}
}

// createNamespace / removeNamespace shell out to `ip netns`; without
// CAP_NET_ADMIN they fail but should not panic and should log the
// error. We can at least exercise the call path on a known-bad name
// so the exec branch runs.
func TestCreateNamespace_logsError(t *testing.T) {
	// Use a name with characters that ip netns rejects so the call
	// fails fast without requiring privileges.
	createNamespace("test/invalid/name/with/slashes")
	// No panic = pass; we don't introspect log output.
}

func TestRemoveNamespace_logsError(t *testing.T) {
	removeNamespace("test/invalid/name/with/slashes")
}
