// xtcp2-treatment-agent runs the TCP route treatment agent as its own
// process: the mTLS check-in loop, desired-state watch, journal and kernel
// route reconciliation, beside an xtcp2 daemon that reads the journal with
// -treatmentAttribution. It is the same library cmd/xtcp2 embeds when built
// with the treatment tag; the watchdog (xtcp2-treatmentctl -mode=watchdog)
// stays a separate process in both arrangements.
//
// SIGHUP checks in and reconciles immediately; SIGINT/SIGTERM stop the agent
// and leave installed routes for the next start or the watchdog to govern.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/agent"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() (err error) {
	config := flag.String("config", "/etc/xtcp2/controller-agent.json", "agent configuration (shared with xtcp2-treatmentctl)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		log.Printf("xtcp2-treatment-agent %s (%s, %s)", version, commit, date)
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	c, err := agent.LoadConfig(*config)
	if err != nil {
		return err
	}
	a, err := agent.New(ctx, c)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := a.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				a.Wake()
			}
		}
	}()
	log.Printf("xtcp2-treatment-agent %s: %s journal=%s", version, c, c.Directory)
	a.Run(ctx)
	return nil
}
