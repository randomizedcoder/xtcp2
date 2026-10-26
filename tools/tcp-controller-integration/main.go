// Command tcp-controller-integration is a disposable-VM test fixture. It is not
// installed with xtcp2. Nix supplies the isolated network and the required marker.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/durationpb"
)

const pkiDir = "/var/lib/controller-test/pki"
const statePath = "/var/lib/xtcp2/routes/state.textproto"
const evidenceDir = "/var/lib/controller-test/evidence"

var hosts = []string{"agent1", "agent2", "agent3", "agent4", "agent5", "agent6"}
var client = &http.Client{Timeout: 30 * time.Second}

type action struct {
	Name                 string
	CA, Certificate, Key []byte
	Seconds              int64
	Destination          string
}
type snapshot struct {
	Routes, Main, State, Journal, Rules, Qdisc, Services, DefaultCC string
	Projection                                                      string
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, format string, args ...any) {
	if !ok {
		panic(fmt.Sprintf(format, args...))
	}
}
func command(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("%s %v: %v: %s", name, args, err, out))
	}
	return string(out)
}
func read(path string) []byte { return must(os.ReadFile(path)) }
func write(path string, data []byte) {
	check(os.MkdirAll(filepath.Dir(path), 0700))
	check(os.WriteFile(path, data, 0600))
}
func systemctl(args ...string) string { return command("systemctl", args...) }
func agentReady() {
	probe := &http.Client{Timeout: time.Second}
	eventually("local daemon ready", 15*time.Second, func() bool {
		response := must(probe.Get("http://127.0.0.1:9088/readyz"))
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	})
}
func routeTable() string {
	// Before the first assignment the private table does not exist.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "-j", "route", "show", "table", "10002").CombinedOutput()
	if err != nil && strings.Contains(string(out), "FIB table does not exist") {
		return "[]"
	}
	check(err)
	return string(out)
}
func localSnapshot() snapshot {
	s := snapshot{Routes: routeTable(), Main: command("ip", "-j", "route", "show", "table", "main"), DefaultCC: strings.TrimSpace(command("sysctl", "-n", "net.ipv4.tcp_congestion_control"))}
	b, err := os.ReadFile(statePath)
	if err == nil {
		s.State = string(b)
	} else if !os.IsNotExist(err) {
		panic(err)
	}
	b, _ = os.ReadFile("/var/lib/xtcp2/routes/assignment-netem-canary.textproto")
	s.Projection = string(b)
	return s
}
func serveAgent(address string) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /action", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				http.Error(w, fmt.Sprint(p), 500)
			}
		}()
		var a action
		check(json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&a))
		var result any = "ok"
		switch a.Name {
		case "ready":
		case "setup":
			require(len(a.CA) > 0 && len(a.Certificate) > 0 && len(a.Key) > 0, "missing fixture credentials")
			write(pkiDir+"/ca.crt", a.CA)
			write(pkiDir+"/agent.crt", a.Certificate)
			write(pkiDir+"/agent.key", a.Key)
			write("/var/lib/controller-test/load.env", []byte("TCP_TEST_DESTINATION=198.18.0.1\n"))
			command("tc", "qdisc", "replace", "dev", "eth0", "root", "handle", "1:", "prio", "bands", "3", "priomap", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0")
			command("tc", "qdisc", "add", "dev", "eth0", "parent", "1:2", "handle", "20:", "netem", "delay", "80ms")
			command("tc", "qdisc", "add", "dev", "eth0", "parent", "1:3", "handle", "30:", "netem", "delay", "5ms")
			command("tc", "filter", "add", "dev", "eth0", "parent", "1:", "protocol", "ip", "prio", "1", "u32", "match", "ip", "dst", "198.18.0.2/32", "flowid", "1:2")
			command("tc", "filter", "add", "dev", "eth0", "parent", "1:", "protocol", "ip", "prio", "2", "u32", "match", "ip", "dst", "198.18.0.0/24", "flowid", "1:3")
			systemctl("start", "load-near", "load-far", "xtcp2", "xtcp2-treatment-watchdog")
		case "status":
			result = localSnapshot()
		case "restart-agent":
			systemctl("restart", "xtcp2")
			agentReady()
		case "stop-agent":
			systemctl("stop", "xtcp2")
			systemctl("is-active", "xtcp2-treatment-watchdog")
		case "start-agent":
			systemctl("start", "xtcp2")
			agentReady()
		case "restart-load":
			systemctl("restart", "load-near")
		case "destination":
			require(validSoakDestination(a.Destination), "invalid fixture destination")
			write("/var/lib/controller-test/load.env", []byte("TCP_TEST_DESTINATION="+a.Destination+"\n"))
			systemctl("restart", "load-near")
		case "hup":
			agentReady()
			systemctl("kill", "-s", "HUP", "xtcp2")
		case "suspend", "resume":
			command("xtcp2-treatmentctl", "-mode="+a.Name, "-reason=integration-local-"+a.Name)
		case "delay":
			require(a.Seconds == 5 || a.Seconds == 180, "invalid fixture delay")
			command("tc", "qdisc", "change", "dev", "eth0", "parent", "1:3", "handle", "30:", "netem", "delay", fmt.Sprintf("%dms", a.Seconds))
		case "expire":
			command("date", "-s", fmt.Sprintf("@%d", time.Now().Unix()+3700))
		case "restore-clock":
			require(a.Seconds > 0, "invalid clock")
			command("date", "-s", fmt.Sprintf("@%d", a.Seconds))
		case "healthy":
			result = systemctl("is-active", "xtcp2", "load-near", "load-far", "xtcp2-treatment-watchdog")
		case "evidence":
			s := localSnapshot()
			s.Journal = command("journalctl", "-b", "--no-pager", "-n", "500", "-u", "xtcp2", "-u", "xtcp2-treatment-watchdog", "-u", "controller-test-agent")
			s.Rules = command("ip", "-j", "rule")
			s.Qdisc = command("tc", "-s", "qdisc", "show")
			result = s
		default:
			panic("unknown fixture action")
		}
		w.Header().Set("Content-Type", "application/json")
		check(json.NewEncoder(w).Encode(result))
	})
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 120 * time.Second}
	log.Fatal(server.ListenAndServe())
}
func remote(host string, a action, result any) {
	index := 0
	for i, h := range hosts {
		if h == host {
			index = i + 11
		}
	}
	require(index != 0, "unknown agent %q", host)
	body := must(json.Marshal(a))
	response := must(client.Post(fmt.Sprintf("http://192.168.1.%d:9009/action", index), "application/json", bytes.NewReader(body)))
	defer response.Body.Close()
	data := must(io.ReadAll(io.LimitReader(response.Body, 2<<20)))
	require(response.StatusCode == 200, "%s %s: %s", host, a.Name, data)
	if result != nil {
		check(json.Unmarshal(data, result))
	}
}
func act(host, name string)       { remote(host, action{Name: name}, nil) }
func status(host string) snapshot { var s snapshot; remote(host, action{Name: "status"}, &s); return s }
func hasRoute(host string) bool   { return strings.Contains(status(host).Routes, "198.18.0.1") }
func routeCount() int {
	n := 0
	for _, h := range hosts {
		if hasRoute(h) {
			n++
		}
	}
	return n
}
func eventually(label string, timeout time.Duration, fn func() bool) {
	log.Print(label)
	deadline := time.Now().Add(timeout)
	var last any
	for time.Now().Before(deadline) {
		ok := func() (ok bool) {
			defer func() {
				if p := recover(); p != nil {
					last = p
				}
			}()
			return fn()
		}()
		if ok {
			return
		}
		time.Sleep(3 * time.Second)
	}
	panic(fmt.Sprintf("%s: timed out after %s (last error: %v)", label, timeout, last))
}
func sql(query string) string { return command("clickhouse-client", "--query", query) }
func count(query string) int  { return must(strconv.Atoi(strings.TrimSpace(sql(query)))) }
func pass(stage string)       { fmt.Println("TCP_CONTROLLER_" + stage + "_PASS") }
func rpcContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}
func inspect(op pb.OperatorServiceClient, id string) *pb.Experiment {
	ctx, cancel := rpcContext()
	defer cancel()
	response := must(op.Inspect(ctx, &pb.InspectRequest{ExperimentId: id}))
	require(len(response.Experiments) == 1, "expected one experiment")
	return response.Experiments[0]
}
func review(op pb.OperatorServiceClient, e *pb.Experiment, id string, evidence *pb.Evidence) *pb.Experiment {
	ctx, cancel := rpcContext()
	defer cancel()
	return must(op.Review(ctx, &pb.ReviewRequest{ExperimentId: e.Id, ExpectedRevision: e.Revision, Approve: true, Reason: "Integration: reviewed measured evidence and destination scope", RequestId: id, Evidence: evidence})).Experiment
}
func propose(op pb.OperatorServiceClient, id string) *pb.Experiment {
	ch := controller.ClickHouse{URL: "http://127.0.0.1:8123", Table: "xtcp.xtcp_flat_records_v2"}
	var baseline *pb.Evidence
	eventually("fresh six-host baseline", 3*time.Minute, func() bool {
		end := time.Now().Add(-20 * time.Second)
		ctx, cancel := rpcContext()
		defer cancel()
		baseline = wire.Evidence(must(ch.Evidence(ctx, "test-dc", netip.MustParsePrefix("198.18.0.1/32"), id, hosts, end.Add(-60*time.Second), end)))
		require(len(baseline.Hosts) == 6, "baseline needs six hosts: %v", baseline.Hosts)
		for _, h := range baseline.Hosts {
			require(h.CollectionHealthy && h.Sockets >= 8 && h.BytesAcked >= 4096, "baseline not ready: %v", h)
		}
		return true
	})
	e := &pb.Experiment{Id: id, Location: "test-dc", Hypothesis: "Integration: review CUBIC on measured low-RTT destination", CohortOrder: hosts, Baseline: baseline,
		Treatment: &pb.Treatment{Id: "cubic-near", Revision: 1, Destination: "198.18.0.1/32", Settings: nearbySettings()},
		Policy:    &pb.ExperimentPolicy{MinimumObservation: durationpb.New(20 * time.Second), MaximumPhase: durationpb.New(15 * time.Minute), MaximumEvidenceAge: durationpb.New(2 * time.Minute), MinimumSocketsPerHost: 8, MinimumBytesPerHost: 4096, MinimumTreatedHosts: 4, MinimumImprovement: 1, MaximumRetransmissionIncrease: .1, MaximumRttIncrease: 1},
	}
	ctx, cancel := rpcContext()
	defer cancel()
	e = must(op.Propose(ctx, &pb.ProposeRequest{Experiment: e, RequestId: id + "-propose"})).Experiment
	require(len(e.Phases) == 0 && routeCount() == 0, "unapproved proposal applied")
	return review(op, e, id+"-initial", nil)
}
func control(op pb.OperatorServiceClient, id string, a pb.ControlAction) {
	ctx, cancel := rpcContext()
	defer cancel()
	must(op.Control(ctx, &pb.ControlRequest{RequestId: id, Action: a, Reason: "Integration withdrawal comparison", Fleet: true}))
}
func evidence() {
	// A failed assertion must still leave useful diagnostics, without replacing
	// the original failure if a crashed guest cannot answer.
	attempt := func(name string, fn func() []byte) {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("evidence %s: %v", name, p)
			}
		}()
		write(evidenceDir+"/"+name, fn())
	}
	for _, h := range hosts {
		attempt(h+".json", func() []byte {
			var s snapshot
			remote(h, action{Name: "evidence"}, &s)
			return must(json.MarshalIndent(s, "", "  "))
		})
	}
	attempt("controller.log", func() []byte { return []byte(command("journalctl", "-b", "--no-pager", "-n", "1500")) })
	attempt("controller.sql", func() []byte { return []byte(command("pg_dump", "-h", "/run/postgresql", "-U", "root", "xtcp")) })
	attempt("telemetry.jsonl", func() []byte {
		return []byte(sql("SELECT * FROM xtcp.xtcp_flat_records_v2 ORDER BY timestamp_ns DESC LIMIT 2000 FORMAT JSONEachRow"))
	})
}
func runController(soak bool) {
	defer evidence()
	eventually("ClickHouse ready", 3*time.Minute, func() bool { return strings.TrimSpace(sql("SELECT 1")) == "1" })
	sql("CREATE DATABASE xtcp")
	cmd := exec.Command("clickhouse-client", "--multiquery")
	cmd.Stdin = bytes.NewReader(read("/etc/xtcp-test/schema.sql"))
	out, err := cmd.CombinedOutput()
	require(err == nil, "schema: %v: %s", err, out)
	sql("CREATE TABLE xtcp.ingest ENGINE=Null AS SELECT toInt64(0) AS timestamp_ns, * EXCEPT(timestamp_ns) FROM xtcp.xtcp_flat_records_v2 WHERE 0")
	sql("CREATE MATERIALIZED VIEW xtcp.ingest_mv TO xtcp.xtcp_flat_records_v2 AS SELECT fromUnixTimestamp64Nano(timestamp_ns) AS timestamp_ns, * EXCEPT(timestamp_ns) FROM xtcp.ingest")
	systemctl("start", "xtcp-controller")
	for _, h := range hosts {
		eventually(h+" fixture ready", 3*time.Minute, func() bool { act(h, "ready"); return true })
		remote(h, action{Name: "setup", CA: read(pkiDir + "/ca.crt"), Certificate: read(pkiDir + "/" + h + ".crt"), Key: read(pkiDir + "/" + h + ".key")}, nil)
	}
	tls := must(identity.TLSConfig(pkiDir+"/operator.crt", pkiDir+"/operator.key", pkiDir+"/ca.crt", "controller", false))
	conn := must(grpc.NewClient("127.0.0.1:9443", grpc.WithTransportCredentials(credentials.NewTLS(tls))))
	defer conn.Close()
	op := pb.NewOperatorServiceClient(conn)
	if soak {
		runSoak(op)
		return
	}
	eventually("both latency bands in real socket telemetry", 3*time.Minute, func() bool {
		rows := strings.Split(strings.TrimSpace(sql("SELECT hex(inet_diag_msg_socket_destination), uniqExact(agent_id), quantileExact(0.5)(tcp_info_rtt)/1000 FROM xtcp.xtcp_flat_records_v2 WHERE timestamp_ns>now()-INTERVAL 30 SECOND AND inet_diag_msg_state=1 AND inet_diag_msg_socket_destination IN (unhex('C6120001000000000000000000000000'),unhex('C6120002000000000000000000000000')) GROUP BY inet_diag_msg_socket_destination ORDER BY inet_diag_msg_socket_destination FORMAT TabSeparated")), "\n")
		require(len(rows) == 2, "expected two RTT bands: %q", rows)
		a, b := strings.Fields(rows[0]), strings.Fields(rows[1])
		require(len(a) == 3 && len(b) == 3 && a[1] == "6" && b[1] == "6", "expected six hosts per RTT band: %q", rows)
		near, far := must(strconv.ParseFloat(a[2], 64)), must(strconv.ParseFloat(b[2], 64))
		require(far > near+35, "RTT bands not separated: %q", rows)
		write(evidenceDir+"/rtt-bands.tsv", []byte(strings.Join(rows, "\n")))
		return true
	})
	pass("RTT")
	mainRoutes := map[string]string{}
	for _, h := range hosts {
		mainRoutes[h] = status(h).Main
	}
	e := propose(op, "netem-canary")
	canary := e.CohortOrder[0]
	eventually("single assigned CUBIC route", 2*time.Minute, func() bool {
		return hasRoute(canary) && routeCount() == 1 && strings.Contains(status(canary).Routes, "cubic")
	})
	assertNearbyRoute(canary, "198.18.0.1")
	eventually("existing sockets retain Reno", time.Minute, func() bool {
		return count(fmt.Sprintf("SELECT count() FROM xtcp.xtcp_flat_records_v2 WHERE agent_id='%s' AND treatment_assignment_id='netem-canary' AND inet_diag_cong='reno' AND NOT treatment_algorithm_exposed", canary)) > 0
	})
	act(canary, "restart-load")
	eventually("new sockets expose actual CUBIC", time.Minute, func() bool {
		return count(fmt.Sprintf("SELECT count() FROM xtcp.xtcp_flat_records_v2 WHERE agent_id='%s' AND treatment_assignment_id='netem-canary' AND inet_diag_cong='cubic' AND treatment_algorithm_exposed", canary)) >= 8
	})
	pass("CANARY")
	act(canary, "restart-agent")
	eventually("journal replay", time.Minute, func() bool { s := status(canary); return hasRoute(canary) && s.State != "" && s.Projection != "" })
	assertNearbyRoute(canary, "198.18.0.1")
	systemctl("restart", "xtcp-controller")
	eventually("controller state survived restart", time.Minute, func() bool { return inspect(op, e.Id).Revision == e.Revision })
	pass("PERSIST")
	act(canary, "suspend")
	require(!hasRoute(canary), "local hold retained route")
	act(canary, "restart-agent")
	control(op, "cannot-clear-local", pb.ControlAction_CONTROL_ACTION_RESUME)
	act(canary, "hup")
	time.Sleep(5 * time.Second)
	s := status(canary)
	state := must(reconcile.UnmarshalText([]byte(s.State)))
	require(state.LocallySuspended && !hasRoute(canary), "remote resume cleared local hold")
	act(canary, "resume")
	eventually("local resume", time.Minute, func() bool { return hasRoute(canary) })
	control(op, "fleet-hold", pb.ControlAction_CONTROL_ACTION_SUSPEND)
	eventually("fleet withdrawal", time.Minute, func() bool { return routeCount() == 0 })
	control(op, "fleet-resume", pb.ControlAction_CONTROL_ACTION_RESUME)
	eventually("fleet resume", time.Minute, func() bool { return hasRoute(canary) })
	assertNearbyRoute(canary, "198.18.0.1")
	act(canary, "restart-load")
	pass("HOLD")
	var analysis *pb.AnalyzeResponse
	eventually("measured first-canary safety", 4*time.Minute, func() bool {
		e = inspect(op, e.Id)
		ctx, cancel := rpcContext()
		defer cancel()
		analysis = must(op.Analyze(ctx, &pb.AnalyzeRequest{ExperimentId: e.Id, ExpectedRevision: e.Revision}))
		log.Printf("analysis: %s", analysis.Outcome)
		return analysis.Outcome == pb.Outcome_OUTCOME_SAFETY_ONLY
	})
	eventually("approve second host with current check-ins", time.Minute, func() bool { e = review(op, inspect(op, e.Id), "approve-second", analysis.Evidence); return true })
	require(e.Phases[len(e.Phases)-1].TargetHosts == 2, "wrong phase target")
	second := e.CohortOrder[1]
	eventually("exactly two treated hosts", time.Minute, func() bool { return routeCount() == 2 && hasRoute(second) })
	assertNearbyRoute(second, "198.18.0.1")
	remote(second, action{Name: "delay", Seconds: 180}, nil)
	act(second, "restart-load")
	eventually("automatic rollback on measured netem harm", 5*time.Minute, func() bool { return inspect(op, e.Id).Status == pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK })
	eventually("all overrides withdrawn", time.Minute, func() bool { return routeCount() == 0 })
	e = inspect(op, e.Id)
	require(e.Phases[len(e.Phases)-1].Outcome == pb.Outcome_OUTCOME_HARMFUL, "wrong rollback reason")
	pass("ROLLBACK")
	remote(second, action{Name: "delay", Seconds: 5}, nil)
	for _, h := range hosts {
		act(h, "restart-load")
	}
	time.Sleep(85 * time.Second)
	e = propose(op, "watchdog-outage")
	victim := e.CohortOrder[0]
	eventually("outage trial installed", time.Minute, func() bool { return hasRoute(victim) })
	systemctl("stop", "xtcp-controller")
	act(victim, "stop-agent")
	act(victim, "expire")
	eventually("independent watchdog expires unchanged one-hour lease", 90*time.Second, func() bool {
		return !hasRoute(victim) && strings.Contains(status(victim).State, "APPLY_STATUS_EXPIRED")
	})
	act(victim, "start-agent")
	time.Sleep(5 * time.Second)
	require(!hasRoute(victim), "restart resurrected expired trial")
	remote(victim, action{Name: "restore-clock", Seconds: time.Now().Unix()}, nil)
	systemctl("start", "xtcp-controller")
	act(victim, "hup")
	time.Sleep(5 * time.Second)
	require(!hasRoute(victim), "reconnect resurrected expired generation")
	pass("WATCHDOG")
	for _, h := range hosts {
		s := status(h)
		require(s.Main == mainRoutes[h], "%s main routes changed", h)
		require(s.DefaultCC == "reno", "%s default CC changed", h)
		act(h, "healthy")
	}
	fmt.Println("TCP_CONTROLLER_PASS")
}
func main() {
	role := flag.String("role", "", "controller or agent")
	listen := flag.String("listen", "", "isolated guest fixture address")
	soak := flag.Bool("soak", false, "run repeated RTT/discovery/rollback cycles from the shared soak-config.json")
	flag.Parse()
	if _, err := os.Stat("/etc/xtcp-controller-integration"); err != nil {
		log.Fatal("requires disposable integration VM marker")
	}
	if *role == "agent" {
		serveAgent(*listen)
		return
	}
	if *role != "controller" {
		log.Fatal("invalid role")
	}
	defer func() {
		if p := recover(); p != nil {
			fmt.Printf("TCP_CONTROLLER_FAIL: %v\n", p)
			os.Exit(1)
		}
	}()
	runController(*soak)
}
