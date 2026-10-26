package actuate

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

// Run only in an explicitly created disposable network namespace. Refuse the
// host namespace even if the opt-in variable was accidentally set there.
func TestTreatmentLiveNamespace(t *testing.T) {
	if os.Getenv("XTCP_TREATMENT_LIVE_TEST") != "1" {
		t.Skip("requires disposable network namespace")
	}
	if os.Getenv("XTCP_TREATMENT_CHILD_NS") != "1" {
		original, e := os.Open("/proc/self/ns/net")
		if e != nil {
			t.Fatal(e)
		}
		defer original.Close()
		cmd := exec.Command("unshare", "--user", "--map-root-user", "--net", os.Args[0], "-test.run=^TestTreatmentLiveNamespace$", "-test.v")
		cmd.ExtraFiles = []*os.File{original}
		cmd.Env = append(os.Environ(), "XTCP_TREATMENT_CHILD_NS=1")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("isolated namespace: %v\n%s", e, out)
		}
		t.Log(string(out))
		return
	}
	var self, init unix.Stat_t
	if e := unix.Stat("/proc/self/ns/net", &self); e != nil {
		t.Fatal(e)
	}
	if e := unix.Fstat(3, &init); e != nil {
		t.Fatal("missing parent namespace descriptor", e)
	}
	var fs unix.Statfs_t
	if e := unix.Fstatfs(3, &fs); e != nil || fs.Type != unix.NSFS_MAGIC {
		t.Fatal("parent descriptor is not a namespace", e)
	}
	if self.Ino == init.Ino {
		t.Fatal("refusing live-write test in host namespace")
	}
	ip := func(args ...string) string {
		t.Helper()
		out, e := exec.Command("ip", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("ip %v: %v: %s", args, e, out)
		}
		return string(out)
	}
	ip("link", "set", "lo", "up")
	ip("link", "add", "xtcp-test", "type", "dummy")
	ip("link", "set", "xtcp-test", "up")
	ip("addr", "add", "192.0.2.10/24", "dev", "xtcp-test")
	ip("-6", "addr", "add", "2001:db8::10/64", "dev", "xtcp-test", "nodad")
	b := &Backend{Table: 10002, Priority: 30000, Protocol: 242, Enabled: true}
	ctx := context.Background()
	for _, destination := range []string{"192.0.2.20/32", "2001:db8::20/128"} {
		t.Run(destination, func(t *testing.T) {
			b.BeginReconcile()
			request := &RouteTreatment{Destination: destination, Settings: &TCPRouteSettings{CongestionControl: ptr("cubic"), RtoMinMs: ptr(uint32(10)), InitialCongestionWindow: ptr(uint32(12))}}
			route, e := b.Plan(ctx, request)
			if e != nil {
				t.Fatal(e)
			}
			if e = b.Install(ctx, route); e != nil {
				t.Fatal(e)
			}
			if e = b.Verify(ctx, route); e != nil {
				t.Fatal(e)
			}
			if e = b.Enable(ctx); e != nil {
				t.Fatal(e)
			}
			b.BeginReconcile()
			if e = b.Install(ctx, route); e != nil {
				t.Fatal("idempotent install", e)
			}
			if e = b.Disable(ctx); e != nil {
				t.Fatal(e)
			}
			if e = b.Remove(ctx, route); e != nil {
				t.Fatal(e)
			}
			if e = b.Remove(ctx, route); e != nil {
				t.Fatal("idempotent withdrawal", e)
			}
		})
	}
	// A colliding route with a foreign protocol must not be overwritten.
	ip("route", "add", "192.0.2.20/32", "dev", "xtcp-test", "table", "10002", "proto", "static")
	b.BeginReconcile()
	route, e := b.Plan(ctx, &RouteTreatment{Destination: "192.0.2.20/32", Settings: &TCPRouteSettings{CongestionControl: ptr("cubic")}})
	if e != nil {
		t.Fatal(e)
	}
	if e = b.Install(ctx, route); e == nil {
		t.Fatal("overwrote foreign route")
	}
	if e = b.Remove(ctx, route); e == nil {
		t.Fatal("deleted foreign route")
	}
}
