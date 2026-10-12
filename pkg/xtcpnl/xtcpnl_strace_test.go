package xtcpnl

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlstrace"
)

// The RDMA corpus (testdata/<kernel>/rdma, one per rdmaKernels entry) is
// strace output, read back by pkg/nlstrace; see its package comment.

// rdmaCorpus reads testdata/<k.dir>/rdma/<slug>.strace's netlink messages.
func rdmaCorpus(t testing.TB, k rdmaKernel, slug string) []nlstrace.Msg {
	t.Helper()
	dgs, err := nlstrace.ParseFile(k.rdma() + "/" + slug + ".strace")
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := nlstrace.Msgs(dgs)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}
