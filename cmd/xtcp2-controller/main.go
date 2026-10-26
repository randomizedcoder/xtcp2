package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	chwarehouse "github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence/clickhouse"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/postgres"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}

// options are the controller's command-line settings.
type options struct {
	listen, statusListen               string
	cert, key, ca                      string
	dsnFile                            string
	migrate                            bool
	clickhouse, table                  string
	templateFile                       string
	discoveryInterval, discoveryWindow time.Duration
}

func parseFlags() (*options, error) {
	o := new(options)
	flag.StringVar(&o.listen, "listen", ":9443", "mTLS gRPC listener")
	flag.StringVar(&o.statusListen, "statusListen", "127.0.0.1:9444", "mTLS status/metrics HTTP listener")
	flag.StringVar(&o.cert, "cert", "", "server certificate")
	flag.StringVar(&o.key, "key", "", "server private key")
	flag.StringVar(&o.ca, "ca", "", "client CA")
	flag.StringVar(&o.dsnFile, "databaseDSNFile", "", "file containing PostgreSQL DSN (or XTCP_CONTROLLER_DATABASE_DSN)")
	flag.BoolVar(&o.migrate, "migrate", false, "apply additive PostgreSQL schema before serving")
	flag.StringVar(&o.clickhouse, "clickhouse", "", "ClickHouse HTTP(S) endpoint; absent disables analysis and lease renewal")
	flag.StringVar(&o.table, "clickhouseTable", "xtcp.xtcp_flat_records_v2", "source telemetry table")
	flag.StringVar(&o.templateFile, "discoveryTemplates", "", "optional JSON array of candidate discovery templates")
	flag.DurationVar(&o.discoveryInterval, "discoveryInterval", 30*time.Minute, "candidate discovery interval (at least one minute; checked on maintenance ticks)")
	flag.DurationVar(&o.discoveryWindow, "discoveryWindow", time.Hour, "candidate discovery lookback (one minute to seven days)")
	flag.Parse()
	if o.discoveryInterval < time.Minute || o.discoveryInterval > 24*time.Hour || o.discoveryWindow < time.Minute || o.discoveryWindow > 7*24*time.Hour {
		return nil, fmt.Errorf("discovery interval must be 1m..24h and window 1m..168h")
	}
	return o, nil
}

// dsn reads the PostgreSQL DSN from the file flag or the environment.
func (o *options) dsn() (string, error) {
	dsn := os.Getenv("XTCP_CONTROLLER_DATABASE_DSN")
	if o.dsnFile != "" {
		b, e := os.ReadFile(o.dsnFile)
		if e != nil {
			return "", e
		}
		dsn = strings.TrimSpace(string(b))
	}
	if dsn == "" {
		return "", fmt.Errorf("PostgreSQL DSN required")
	}
	return dsn, nil
}

// templates loads the discovery templates, which need a warehouse.
func (o *options) templates(warehouse *chwarehouse.Client) ([]controller.DiscoveryTemplate, error) {
	if o.templateFile == "" {
		return nil, nil
	}
	b, e := os.ReadFile(o.templateFile)
	if e != nil {
		return nil, e
	}
	var templates []controller.DiscoveryTemplate
	if e = json.Unmarshal(b, &templates); e != nil {
		return nil, e
	}
	if warehouse == nil {
		return nil, fmt.Errorf("discovery requires ClickHouse")
	}
	return templates, nil
}

// openStore connects to PostgreSQL and applies the schema when asked.
func (o *options) openStore(ctx context.Context) (*postgres.Store, error) {
	dsn, err := o.dsn()
	if err != nil {
		return nil, err
	}
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if o.migrate {
		if err = db.Migrate(ctx); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	return db, nil
}

// warehouse is one adapter behind two ports: evidence.Source for analysis
// and lease renewal, evidence.Warehouse for discovery. Both are nil without
// -clickhouse.
func (o *options) warehouse() (*chwarehouse.Client, evidence.Source) {
	if o.clickhouse == "" {
		return nil, nil
	}
	w := &chwarehouse.Client{URL: o.clickhouse, Table: o.table, Username: os.Getenv("XTCP_CLICKHOUSE_USER"), Password: os.Getenv("XTCP_CLICKHOUSE_PASSWORD")}
	return w, w
}

func run() (err error) {
	o, err := parseFlags()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	tlsConfig, e := identity.TLSConfig(o.cert, o.key, o.ca, "", true)
	if e != nil {
		return e
	}
	db, e := o.openStore(ctx)
	if e != nil {
		return e
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("database close: %v", cerr)
		}
	}()
	warehouse, source := o.warehouse()
	s := &controller.Server{Store: db, Hub: controller.NewNotificationHub(), DiscoveryWindow: o.discoveryWindow, EvidenceSource: source}
	templates, e := o.templates(warehouse)
	if e != nil {
		return e
	}
	// grpcServer.Serve owns the listener and closes it when it stops.
	listener, e := (&net.ListenConfig{}).Listen(ctx, "tcp", o.listen)
	if e != nil {
		return e
	}
	grpcServer := grpc.NewServer(append(controller.GRPCOptions(), grpc.Creds(credentials.NewTLS(tlsConfig)))...)
	pb.RegisterAgentControlServiceServer(grpcServer, s)
	pb.RegisterOperatorServiceServer(grpcServer, s)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.RunNotifications(ctx) }()
	go func() { defer wg.Done(); housekeep(ctx, s, source, warehouse, templates, o.discoveryInterval) }()
	statusServer := &http.Server{Addr: o.statusListen, Handler: s.StatusHandler(), TLSConfig: tlsConfig.Clone(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	failed := make(chan error, 2)
	go func() { failed <- grpcServer.Serve(listener) }()
	if o.statusListen != "" {
		go func() {
			l, e := (&net.ListenConfig{}).Listen(ctx, "tcp", o.statusListen)
			if e != nil {
				failed <- e
				return
			}
			failed <- statusServer.Serve(tls.NewListener(l, statusServer.TLSConfig))
		}()
	}
	log.Printf("xtcp2 controller listening on %s; human review required", o.listen)
	select {
	case <-ctx.Done():
	case e = <-failed:
		cancel()
	}
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if serr := statusServer.Shutdown(shutdown); serr != nil {
		log.Printf("status listener shutdown: %v", serr)
	}
	stopped := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-shutdown.Done():
		grpcServer.Stop()
	}
	wg.Wait()
	if e == grpc.ErrServerStopped || e == http.ErrServerClosed {
		return nil
	}
	return e
}

// housekeep runs maintenance every minute, and discovery on its own
// cadence, under the housekeeping lease until ctx is done.
func housekeep(ctx context.Context, s *controller.Server, source evidence.Source, warehouse *chwarehouse.Client, templates []controller.DiscoveryTemplate, interval time.Duration) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	nextDiscovery := time.Now()
	leader := false
	for {
		// Discovery is due on this replica's own cadence whether or not
		// it holds the lease, so a follower that becomes leader does not
		// run a backlog of passes.
		var due evidence.Warehouse
		if warehouse != nil && !time.Now().Before(nextDiscovery) {
			due = warehouse
			// Keep the cadence independent of query duration; otherwise a
			// one-minute interval can accidentally skip every other tick.
			for !nextDiscovery.After(time.Now()) {
				nextDiscovery = nextDiscovery.Add(interval)
			}
		}
		held, e := s.Housekeep(ctx, source, due, templates)
		if e != nil {
			log.Printf("controller housekeeping: %v", e)
		}
		// A failed Lease call reports held=false with an error; that is
		// not a follower, so the leader state is kept until it answers.
		if (e == nil || held) && held != leader {
			leader = held
			if held {
				log.Print("controller housekeeping: this replica holds the lease")
			} else {
				log.Print("controller housekeeping: another replica holds the lease; skipping passes")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
