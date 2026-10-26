// xtcp2-treatmentctl is the local, filesystem-permission-protected emergency
// interface and independent lease watchdog. It never needs a controller RPC,
// a certificate or DNS: it builds on pkg/treatment/local and
// pkg/treatment/reconcile only, and the import-boundary test keeps it that way.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/local"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	config := flag.String("config", "/etc/xtcp2/controller-agent.json", "agent configuration")
	mode := flag.String("mode", "status", "status, suspend, resume, reconcile, or watchdog")
	reason := flag.String("reason", "", "required for suspend/resume")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	c, e := local.LoadConfig(*config)
	if e != nil {
		return e
	}
	h, e := local.New(c, nil)
	if e != nil {
		return e
	}
	switch *mode {
	case "status":
		// Read without the lock: the snapshot is replaced atomically and
		// status must answer while a reconcile pass holds the journal.
		b, e := os.ReadFile(filepath.Join(c.Directory, reconcile.SnapshotFile))
		if e != nil {
			return e
		}
		_, e = os.Stdout.Write(b)
		return e
	case "suspend", "resume":
		if e = h.Reconciler.SuspendLocal(*mode == "suspend", *reason); e != nil {
			return e
		}
	case "reconcile":
	case "watchdog":
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			pass, cancelPass := context.WithTimeout(ctx, 25*time.Second)
			_, e = h.Reconciler.Run(pass, nil, reconcile.ModeExpiryOnly)
			cancelPass()
			if e != nil {
				log.Printf("watchdog: %v", e)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
			}
		}
	default:
		return fmt.Errorf("unknown mode %q", *mode)
	}
	s, e := h.Reconciler.Run(ctx, nil, reconcile.ModeFull)
	if s != nil {
		if b, err := reconcile.MarshalText(s); err == nil {
			os.Stdout.Write(b)
		}
	}
	return e
}
