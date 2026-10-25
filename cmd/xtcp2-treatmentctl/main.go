// xtcp2-treatmentctl is the local, filesystem-permission-protected emergency
// interface and independent lease watchdog. It never needs a controller RPC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/agent"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"google.golang.org/protobuf/encoding/prototext"
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
	c, e := agent.LoadConfig(*config)
	if e != nil {
		return e
	}
	a, e := agent.NewLocal(c)
	if e != nil {
		return e
	}
	defer a.Close()
	switch *mode {
	case "status":
		b, e := os.ReadFile(c.Directory + "/state.textproto")
		if e != nil {
			return e
		}
		_, e = os.Stdout.Write(b)
		return e
	case "suspend", "resume":
		if e = a.Reconciler.SuspendLocal(*mode == "suspend", *reason); e != nil {
			return e
		}
	case "reconcile":
	case "watchdog":
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			pass, cancelPass := context.WithTimeout(ctx, 25*time.Second)
			_, e = a.Reconciler.Run(pass, nil, reconcile.ModeExpiryOnly)
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
	s, e := a.Reconciler.Run(ctx, nil, reconcile.ModeFull)
	if s != nil {
		fmt.Print(prototext.Format(s))
	}
	return e
}
