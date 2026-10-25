package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/experiment"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type soakConfig struct {
	Cycles          int `json:"cycles"`
	IntervalSeconds int `json:"interval_seconds"`
}

func (c soakConfig) validate() error {
	if c.Cycles < 2 || c.Cycles > 6 || c.IntervalSeconds < 180 || c.IntervalSeconds > 300 {
		return fmt.Errorf("soak requires 2..6 cycles and 180..300 seconds per RTT phase")
	}
	return nil
}
func validSoakDestination(s string) bool {
	for i := 101; i <= 106; i++ {
		if s == fmt.Sprintf("198.18.0.%d", i) {
			return true
		}
	}
	return false
}

type soakRun struct {
	op                 pb.OperatorServiceClient
	config             soakConfig
	started            time.Time
	cycle              int
	phase, destination string
	delay              int64
	events             *os.File
	samples            *csv.Writer
	sampleFile         *os.File
}

func (s *soakRun) event(name string, e *pb.Experiment) {
	row := map[string]any{"at": time.Now().UTC(), "elapsed_seconds": time.Since(s.started).Seconds(), "cycle": s.cycle, "phase": s.phase, "destination": s.destination, "delay_ms": s.delay, "event": name}
	if e != nil {
		row["experiment"] = json.RawMessage(must(protojson.Marshal(e)))
	}
	check(json.NewEncoder(s.events).Encode(row))
	check(s.events.Sync())
	log.Printf("SOAK cycle=%d phase=%s destination=%s delay=%dms event=%s", s.cycle, s.phase, s.destination, s.delay, name)
}
func (s *soakRun) experiments() []*pb.Experiment {
	ctx, cancel := rpcContext()
	defer cancel()
	return must(s.op.Inspect(ctx, &pb.InspectRequest{})).Experiments
}
func (s *soakRun) current() *pb.Experiment {
	var found *pb.Experiment
	for _, e := range s.experiments() {
		if e.Treatment.Destination == s.destination+"/32" {
			require(found == nil, "duplicate proposal for %s", s.destination)
			found = e
		}
	}
	return found
}
func destinationRoutes(destination string) int {
	n := 0
	for _, h := range hosts {
		var rows []struct {
			Destination string `json:"dst"`
		}
		check(json.Unmarshal([]byte(status(h).Routes), &rows))
		for _, r := range rows {
			if r.Destination == destination || r.Destination == destination+"/32" {
				n++
			}
		}
	}
	return n
}
func ownedRoutes() int {
	n := 0
	for _, h := range hosts {
		var routes []json.RawMessage
		check(json.Unmarshal([]byte(status(h).Routes), &routes))
		n += len(routes)
	}
	return n
}
func (s *soakRun) sample() {
	// Restrict to the active destination. Read actual SRTT, not the lifetime
	// minimum used by discovery. Keep each host's values instead of pooling it.
	query := fmt.Sprintf(`SELECT agent_id, quantileExact(0.5)(tcp_info_rtt)/1000,
 countIf(inet_diag_cong='reno'), countIf(inet_diag_cong='cubic'), countIf(treatment_algorithm_exposed)
 FROM xtcp.xtcp_flat_records_v2 WHERE timestamp_ns>now()-INTERVAL 20 SECOND
 AND inet_diag_msg_state=1 AND hex(inet_diag_msg_socket_destination)=concat(hex(IPv4StringToNum('%s')),'000000000000000000000000')
 GROUP BY agent_id ORDER BY agent_id FORMAT TabSeparated`, s.destination)
	raw := strings.TrimSpace(sql(query))
	e := s.current()
	id, st := "", "none"
	if e != nil {
		id = e.Id
		st = e.Status.String()
	}
	if raw != "" {
		for _, line := range strings.Split(raw, "\n") {
			fields := strings.Split(line, "\t")
			require(len(fields) == 5, "invalid RTT sample: %q", line)
			row := []string{time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprint(s.cycle), s.phase, s.destination, fmt.Sprint(s.delay), id, st}
			check(s.samples.Write(append(row, fields...)))
		}
	}
	s.samples.Flush()
	check(s.samples.Error())
	check(s.sampleFile.Sync())
}
func (s *soakRun) setPhase(phase string, delay int64, newDestination bool) time.Time {
	s.phase, s.delay = phase, delay
	for _, h := range hosts {
		remote(h, action{Name: "delay", Seconds: delay}, nil)
	}
	if newDestination {
		for _, h := range hosts {
			remote(h, action{Name: "destination", Destination: s.destination}, nil)
		}
	}
	s.event("netem_changed", nil)
	return time.Now().Add(time.Duration(s.config.IntervalSeconds) * time.Second)
}
func (s *soakRun) until(end time.Time, inspectState func()) {
	for time.Now().Before(end) {
		inspectState()
		s.sample()
		remaining := time.Until(end)
		if remaining > 15*time.Second {
			remaining = 15 * time.Second
		}
		if remaining > 0 {
			time.Sleep(remaining)
		}
	}
	inspectState()
	s.sample()
}
func (s *soakRun) assertMeasuredRTT() {
	// Last twenty seconds must contain all six agents at the injected RTT.
	query := fmt.Sprintf(`SELECT uniqExact(agent_id), quantileExact(0.5)(tcp_info_rtt)/1000
 FROM xtcp.xtcp_flat_records_v2 WHERE timestamp_ns>now()-INTERVAL 20 SECOND
 AND inet_diag_msg_state=1 AND hex(inet_diag_msg_socket_destination)=concat(hex(IPv4StringToNum('%s')),'000000000000000000000000') FORMAT TabSeparated`, s.destination)
	fields := strings.Fields(sql(query))
	require(len(fields) == 2 && fields[0] == "6", "missing RTT population: %v", fields)
	var rtt float64
	_, err := fmt.Sscan(fields[1], &rtt)
	check(err)
	require(rtt >= float64(s.delay)*.8 && rtt <= float64(s.delay)*1.5+3, "RTT %.3fms does not match netem %dms", rtt, s.delay)
}

// A low netem delay does not guarantee this aggressive profile is beneficial.
// Accept an early safety withdrawal only with valid, exposed, harmful evidence.
func validateSoakRollback(e *pb.Experiment, injectedLatency bool) error {
	if e == nil || e.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK || len(e.Phases) != 1 || e.Phases[0].Outcome != "harmful" || len(e.CohortOrder) == 0 || e.Policy == nil || e.Baseline == nil || e.LatestEvidence == nil {
		return fmt.Errorf("rollback is missing its harmful phase or evidence")
	}
	if err := treatment.ValidateEvidence(e.Baseline); err != nil {
		return err
	}
	if err := treatment.ValidateEvidence(e.LatestEvidence); err != nil {
		return err
	}
	if err := treatment.ValidatePolicy(e.Policy); err != nil {
		return err
	}
	_, harmful := experiment.HealthyEvidence(e, e.LatestEvidence, e.LatestEvidence.AnalyzedAt.AsTime())
	if !harmful {
		return fmt.Errorf("rollback evidence does not cross a safety guard")
	}
	for _, h := range e.LatestEvidence.Hosts {
		if h.AgentId == e.CohortOrder[0] && h.ExposedSockets > 0 {
			if injectedLatency && h.RttP95Ms < 90 {
				return fmt.Errorf("rollback evidence did not observe injected latency on the canary")
			}
			return nil
		}
	}
	return fmt.Errorf("rollback evidence has no exposed canary sockets")
}

func injectedLatencyRollback(e *pb.Experiment, changedAt time.Time) bool {
	return validateSoakRollback(e, true) == nil && e.LatestEvidence.WindowEnd.AsTime().After(changedAt)
}

func runSoak(op pb.OperatorServiceClient) {
	var config soakConfig
	check(json.Unmarshal(read(evidenceDir+"/soak-config.json"), &config))
	check(config.validate())
	events := must(os.OpenFile(evidenceDir+"/soak-events.jsonl", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600))
	defer events.Close()
	samples := must(os.OpenFile(evidenceDir+"/soak-rtt.csv", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600))
	defer samples.Close()
	s := &soakRun{op: op, config: config, started: time.Now(), events: events, sampleFile: samples, samples: csv.NewWriter(samples)}
	check(s.samples.Write([]string{"at", "cycle", "phase", "destination", "netem_ms", "experiment", "status", "agent", "median_rtt_ms", "reno_samples", "cubic_samples", "exposed_samples"}))
	mainRoutes := map[string]string{}
	for _, h := range hosts {
		mainRoutes[h] = status(h).Main
	}
	latencyRollbacks, lowRTTRollbacks := 0, 0
	for cycle := 1; cycle <= config.Cycles; cycle++ {
		s.cycle = cycle
		s.destination = fmt.Sprintf("198.18.0.%d", 100+cycle)
		end := s.setPhase("high_no_candidate", 180, true)
		s.until(end, func() {
			require(s.current() == nil, "high-RTT destination incorrectly proposed")
			require(ownedRoutes() == 0, "unapproved route installed")
		})
		s.assertMeasuredRTT()
		s.event("high_rtt_filtered", nil)
		// New connections reset TCP_INFO's lifetime minimum RTT. The discovery
		// window and 20-second ingest watermark must both age out the high phase.
		end = s.setPhase("low_discovery", 5, true)
		var approved *pb.Experiment
		rolledBack := false
		s.until(end, func() {
			e := s.current()
			if e == nil {
				return
			}
			if approved == nil {
				require(e.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_PROPOSED && len(e.Phases) == 0 && ownedRoutes() == 0, "discovery bypassed operator approval")
				require(e.Treatment.Id == "soak-nearby-cubic" && strings.HasPrefix(e.Hypothesis, "Template soak-nearby-cubic:"), "candidate was not created by discovery")
				require(proto.Equal(e.Treatment.Settings, nearbySettings()), "discovery lost nearby route settings: %v", e.Treatment.Settings)
				require(e.GetPolicy().GetMaximumRttIncrease() == 1, "soak discovery did not use the fixture RTT tolerance")
				s.event("controller_proposed", e)
				approved = review(op, e, fmt.Sprintf("soak-review-%d", cycle), nil)
				s.event("test_operator_approved", approved)
				eventually("soak canary route installed", time.Minute, func() bool { return destinationRoutes(s.destination) == 1 })
				assertNearbyRoute(approved.CohortOrder[0], s.destination)
				act(approved.CohortOrder[0], "restart-load")
			}
			e = inspect(op, approved.Id)
			if e.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK {
				if !rolledBack {
					s.event("low_rtt_rollback_observed", e)
					check(validateSoakRollback(e, false))
					eventually("early safety overrides withdrawn", time.Minute, func() bool { return ownedRoutes() == 0 })
					s.event("low_rtt_safety_rollback", e)
					lowRTTRollbacks++
					rolledBack = true
				}
				require(ownedRoutes() == 0, "early safety withdrawal was reversed")
				return
			}
			require(!rolledBack, "early rolled-back experiment was revived")
			if e.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING || len(e.Phases) != 1 {
				s.event("unexpected_low_rtt_state", e)
				panic(fmt.Sprintf("low-RTT phase: status=%s outcome=%s; full evidence in soak-events.jsonl", e.Status, e.LatestOutcome))
			}
			require(destinationRoutes(s.destination) == 1 && ownedRoutes() == 1, "unapproved expansion")
		})
		require(approved != nil, "no controller proposal after low RTT")
		s.assertMeasuredRTT()
		if !rolledBack {
			require(count(fmt.Sprintf("SELECT count() FROM xtcp.xtcp_flat_records_v2 WHERE timestamp_ns>now()-INTERVAL 20 SECOND AND treatment_assignment_id='%s' AND inet_diag_cong='cubic' AND treatment_algorithm_exposed", approved.Id)) >= 8, "missing actual CUBIC exposure")
			s.event("canary_exposed", inspect(op, approved.Id))
		}
		// Keep existing connections so maintenance sees the RTT regression on
		// the treated sockets; the harness never requests a rollback.
		highStarted := time.Now()
		end = s.setPhase("high_automatic_rollback", 180, false)
		s.until(end, func() {
			e := inspect(op, approved.Id)
			require(len(e.Phases) == 1, "controller promoted without review")
			if e.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK && !rolledBack {
				s.event("controller_rollback_observed", e)
				check(validateSoakRollback(e, false))
				eventually("soak overrides withdrawn", time.Minute, func() bool { return ownedRoutes() == 0 })
				// Maintenance may deliver a low-RTT safety decision after the
				// phase boundary. Classify its evidence, not its arrival time.
				if injectedLatencyRollback(e, highStarted) {
					s.event("controller_automatic_rollback", e)
					latencyRollbacks++
				} else {
					s.event("low_rtt_safety_rollback", e)
					lowRTTRollbacks++
				}
				rolledBack = true
			}
			if rolledBack {
				require(e.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK, "rolled-back experiment was revived")
				require(ownedRoutes() == 0, "rolled-back treatment restored")
			}
		})
		require(rolledBack, "controller did not roll back the impaired trial")
		s.assertMeasuredRTT()
		pass(fmt.Sprintf("SOAK_CYCLE_%d", cycle))
	}
	require(latencyRollbacks > 0, "all %d trials withdrew at low RTT; injected-latency rollback was not exercised (see soak-events.jsonl)", lowRTTRollbacks)
	// Restore a previously failed destination. A fresh low-RTT window must not
	// bypass the unchanged 24-hour cooldown or revive the old assignment.
	s.destination = "198.18.0.101"
	end := s.setPhase("low_cooldown", 5, true)
	s.until(end, func() {
		e := s.current()
		require(e != nil && e.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK, "cooldown state changed")
		require(len(s.experiments()) == config.Cycles && ownedRoutes() == 0, "cooldown allowed a new action")
	})
	s.assertMeasuredRTT()
	s.event("cooldown_verified", s.current())
	pass("SOAK_COOLDOWN")
	for _, h := range hosts {
		st := status(h)
		require(st.Main == mainRoutes[h] && st.DefaultCC == "reno", "%s baseline routing changed", h)
		act(h, "healthy")
	}
	write(evidenceDir+"/soak-summary.json", must(json.MarshalIndent(map[string]any{"cycles": config.Cycles, "interval_seconds": config.IntervalSeconds, "elapsed_seconds": time.Since(s.started).Seconds(), "proposals": len(s.experiments()), "automatic_rollbacks": latencyRollbacks + lowRTTRollbacks, "injected_latency_rollbacks": latencyRollbacks, "low_rtt_safety_rollbacks": lowRTTRollbacks, "cooldown_verified": true}, "", "  ")))
	pass("SOAK")
}
