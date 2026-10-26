package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller"
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
func run() error {
	listen := flag.String("listen", ":9443", "mTLS gRPC listener")
	statusListen := flag.String("statusListen", "127.0.0.1:9444", "mTLS status/metrics HTTP listener")
	cert := flag.String("cert", "", "server certificate")
	key := flag.String("key", "", "server private key")
	ca := flag.String("ca", "", "client CA")
	dsnFile := flag.String("databaseDSNFile", "", "file containing PostgreSQL DSN (or XTCP_CONTROLLER_DATABASE_DSN)")
	migrate := flag.Bool("migrate", false, "apply additive PostgreSQL schema before serving")
	clickhouse := flag.String("clickhouse", "", "ClickHouse HTTP(S) endpoint; absent disables analysis and lease renewal")
	table := flag.String("clickhouseTable", "xtcp.xtcp_flat_records_v2", "source telemetry table")
	templateFile := flag.String("discoveryTemplates", "", "optional JSON array of candidate discovery templates")
	discoveryInterval := flag.Duration("discoveryInterval", 30*time.Minute, "candidate discovery interval (at least one minute; checked on maintenance ticks)")
	discoveryWindow := flag.Duration("discoveryWindow", time.Hour, "candidate discovery lookback (one minute to seven days)")
	flag.Parse()
	if *discoveryInterval < time.Minute || *discoveryInterval > 24*time.Hour || *discoveryWindow < time.Minute || *discoveryWindow > 7*24*time.Hour {
		return fmt.Errorf("discovery interval must be 1m..24h and window 1m..168h")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	dsn := os.Getenv("XTCP_CONTROLLER_DATABASE_DSN")
	if *dsnFile != "" {
		b, e := os.ReadFile(*dsnFile)
		if e != nil {
			return e
		}
		dsn = strings.TrimSpace(string(b))
	}
	if dsn == "" {
		return fmt.Errorf("PostgreSQL DSN required")
	}
	tlsConfig, e := identity.TLSConfig(*cert, *key, *ca, "", true)
	if e != nil {
		return e
	}
	db, e := postgres.Open(ctx, dsn)
	if e != nil {
		return e
	}
	defer db.Close()
	if *migrate {
		if e = db.Migrate(ctx); e != nil {
			return e
		}
	}
	s := &controller.Server{Store: db, Hub: controller.NewNotificationHub(), DiscoveryWindow: *discoveryWindow}
	var analysis *controller.ClickHouse
	var source controller.EvidenceSource
	if *clickhouse != "" {
		analysis = &controller.ClickHouse{URL: *clickhouse, Table: *table, Username: os.Getenv("XTCP_CLICKHOUSE_USER"), Password: os.Getenv("XTCP_CLICKHOUSE_PASSWORD")}
		source = analysis
	}
	var templates []controller.DiscoveryTemplate
	s.EvidenceSource = source
	if *templateFile != "" {
		b, e := os.ReadFile(*templateFile)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &templates); e != nil {
			return e
		}
		if analysis == nil {
			return fmt.Errorf("discovery requires ClickHouse")
		}
	}
	listener, e := net.Listen("tcp", *listen)
	if e != nil {
		return e
	}
	defer listener.Close()
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(treatment.MaxStateBytes), grpc.MaxSendMsgSize(treatment.MaxStateBytes), grpc.MaxConcurrentStreams(8))
	pb.RegisterAgentControlServiceServer(grpcServer, s)
	pb.RegisterOperatorServiceServer(grpcServer, s)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.RunNotifications(ctx) }()
	go func() {
		defer wg.Done()
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		nextDiscovery := time.Now()
		for {
			if e := s.Maintain(ctx, source); e != nil {
				log.Printf("controller maintenance: %v", e)
			}
			if analysis != nil && !time.Now().Before(nextDiscovery) {
				if e := s.Discover(ctx, analysis, templates); e != nil {
					log.Printf("candidate discovery: %v", e)
				}
				// Keep the cadence independent of query duration; otherwise a
				// one-minute interval can accidentally skip every other tick.
				for !nextDiscovery.After(time.Now()) {
					nextDiscovery = nextDiscovery.Add(*discoveryInterval)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	statusServer := &http.Server{Addr: *statusListen, Handler: s.StatusHandler(), TLSConfig: tlsConfig.Clone(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	errors := make(chan error, 2)
	go func() { errors <- grpcServer.Serve(listener) }()
	if *statusListen != "" {
		go func() {
			l, e := tls.Listen("tcp", *statusListen, tlsConfig.Clone())
			if e != nil {
				errors <- e
				return
			}
			errors <- statusServer.Serve(l)
		}()
	}
	log.Printf("xtcp2 controller listening on %s; human review required", *listen)
	select {
	case <-ctx.Done():
	case e = <-errors:
		cancel()
	}
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = statusServer.Shutdown(shutdown)
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
