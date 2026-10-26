package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
)

// Templates only authorize candidate discovery. Every generated hypothesis and
// every increase in exposure still requires an authenticated operator review.
type DiscoveryTemplate struct {
	ID                         string   `json:"id"`
	Location                   string   `json:"location"`
	CongestionControl          string   `json:"congestion_control"`
	RtoMinMs                   *uint32  `json:"rto_min_ms,omitempty"`
	InitialCongestionWindow    *uint32  `json:"initial_congestion_window,omitempty"`
	InitialReceiveWindow       *uint32  `json:"initial_receive_window,omitempty"`
	QuickAck                   *bool    `json:"quick_ack,omitempty"`
	MaximumRTTIncrease         *float64 `json:"maximum_rtt_increase,omitempty"`
	MinimumRTTMs               float64  `json:"minimum_rtt_ms"`
	MaximumRTTMs               float64  `json:"maximum_rtt_ms"`
	MinimumRetransmissionRatio float64  `json:"minimum_retransmission_ratio"`
	ASNs                       []uint64 `json:"asns"`
	Prefixes                   []string `json:"prefixes"`
}

func (t DiscoveryTemplate) policy() treatment.Policy {
	p := treatment.DefaultPolicy()
	if t.MaximumRTTIncrease != nil {
		p.MaximumRTTIncrease = *t.MaximumRTTIncrease
	}
	return p
}

func (t DiscoveryTemplate) settings() treatment.TCPSettings {
	cc := t.CongestionControl
	return treatment.TCPSettings{
		CongestionControl:       &cc,
		RTOMinMs:                t.RtoMinMs,
		InitialCongestionWindow: t.InitialCongestionWindow,
		InitialReceiveWindow:    t.InitialReceiveWindow,
		QuickAck:                t.QuickAck,
	}
}

const discoverySQL = `
WITH sockets AS (
 SELECT agent_id,host_boot_id,netns_inode,inet_diag_msg_socket_cookie,
 any(hex(inet_diag_msg_socket_destination)) AS destination_hex,
 any(inet_diag_msg_family) AS family,
 argMax(enrich_socket_dest_asn,timestamp_ns) AS asn,
 min(tcp_info_min_rtt)/1000 AS min_rtt_ms,
 max(tcp_info_bytes_sent)-min(tcp_info_bytes_sent) AS sent,
 max(tcp_info_bytes_retrans)-min(tcp_info_bytes_retrans) AS retrans
 FROM {{TABLE}}
 WHERE timestamp_ns>=fromUnixTimestamp64Nano({start:Int64}) AND timestamp_ns<fromUnixTimestamp64Nano({end:Int64})
 AND location={location:String} AND agent_id!='' AND inet_diag_msg_state=1
 GROUP BY agent_id,host_boot_id,netns_inode,inet_diag_msg_socket_cookie
 HAVING countDistinct(timestamp_ns)>=3
)
SELECT destination_hex,family,any(asn) AS asn,groupUniqArray(10000)(agent_id) AS agents,
 quantileExact(0.5)(min_rtt_ms) AS rtt_ms,sum(retrans)/greatest(1,sum(sent)) AS retransmission_ratio
FROM sockets GROUP BY destination_hex,family
HAVING length(agents)>=6 AND rtt_ms>={minimum_rtt:Float64} AND rtt_ms<={maximum_rtt:Float64}
 AND retransmission_ratio>={minimum_retransmission:Float64}
ORDER BY sum(sent) DESC LIMIT 10 FORMAT JSONEachRow`

func (s *Server) Discover(ctx context.Context, c *ClickHouse, templates []DiscoveryTemplate) error {
	window := s.DiscoveryWindow
	if window == 0 {
		window = time.Hour
	}
	if window < time.Minute || window > 7*24*time.Hour {
		return errors.New("discovery window must be between one minute and seven days")
	}
	var failures []error
	for _, t := range templates {
		if !treatment.ValidID(t.ID) || !treatment.ValidID(t.Location) || (t.CongestionControl != "cubic" && t.CongestionControl != "bbr") || t.MaximumRTTMs <= t.MinimumRTTMs || t.MinimumRTTMs < 0 || t.MinimumRetransmissionRatio < 0 || t.MinimumRetransmissionRatio > 1 {
			return errors.New("invalid discovery template; initial discovery supports CUBIC/BBR only")
		}
		if err := treatment.ValidateTCPSettings(t.settings(), false); err != nil {
			return fmt.Errorf("discovery template %s: %w", t.ID, err)
		}
		if err := treatment.ValidatePolicy(t.policy()); err != nil {
			return fmt.Errorf("discovery template %s policy: %w", t.ID, err)
		}
		var prefixes []netip.Prefix
		for _, v := range t.Prefixes {
			p, e := netip.ParsePrefix(v)
			if e != nil {
				return e
			}
			prefixes = append(prefixes, p)
		}
		// Avoid warehouse aggregation while this location cannot accept a
		// new experiment. The transaction below still rechecks the budget.
		busy, e := s.Store.Repos().Experiments.HasActive(ctx, t.Location)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if busy {
			continue
		}
		end := s.now().Add(-20 * time.Second)
		start := end.Add(-window)
		params := map[string]string{"location": t.Location, "start": strconv.FormatInt(start.UnixNano(), 10), "end": strconv.FormatInt(end.UnixNano(), 10), "minimum_rtt": fmt.Sprint(t.MinimumRTTMs), "maximum_rtt": fmt.Sprint(t.MaximumRTTMs), "minimum_retransmission": fmt.Sprint(t.MinimumRetransmissionRatio)}
		data, e := c.query(ctx, discoverySQL, params)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		for {
			var row struct {
				DestinationHex string   `json:"destination_hex"`
				Family         uint32   `json:"family"`
				ASN            uint64   `json:"asn"`
				Agents         []string `json:"agents"`
				RTT            float64  `json:"rtt_ms"`
				Retransmission float64  `json:"retransmission_ratio"`
			}
			if e = dec.Decode(&row); e == io.EOF {
				break
			}
			if e != nil {
				failures = append(failures, e)
				break
			}
			if len(t.ASNs) > 0 {
				match := false
				for _, a := range t.ASNs {
					match = match || a == row.ASN
				}
				if !match {
					continue
				}
			}
			raw, e := hex.DecodeString(row.DestinationHex)
			if e != nil {
				failures = append(failures, e)
				continue
			}
			if row.Family == 2 && len(raw) >= 4 {
				raw = raw[:4]
			}
			ip, ok := netip.AddrFromSlice(raw)
			if !ok {
				continue
			}
			if len(prefixes) > 0 {
				match := false
				for _, p := range prefixes {
					match = match || p.Contains(ip)
				}
				if !match {
					continue
				}
			}
			destination := netip.PrefixFrom(ip, ip.BitLen())
			// Completed or held treatments and recent rejected trials suppress
			// repeat proposals. Cooldown is at least a day after terminal state.
			exists, e := s.Store.Repos().Experiments.DestinationBlocked(ctx, t.Location, destination, s.now().Add(-24*time.Hour))
			if e != nil {
				failures = append(failures, e)
				continue
			}
			if exists {
				continue
			}
			seed := sha256.Sum256([]byte(t.ID + "/" + destination.String() + "/" + s.now().UTC().Format("2006-01-02")))
			id := "hyp-" + hex.EncodeToString(seed[:12])
			baseline, e := c.Evidence(ctx, t.Location, destination, id, row.Agents, start, end)
			if e != nil {
				failures = append(failures, e)
				continue
			}
			n := &treatment.Experiment{ID: id, Location: t.Location, Hypothesis: fmt.Sprintf("Template %s: test %s for %s (ASN %d), observed RTT %.3f ms and retransmission ratio %.6f; this is a hypothesis, not an efficacy claim", t.ID, t.CongestionControl, destination, row.ASN, row.RTT, row.Retransmission), Treatment: treatment.Treatment{ID: t.ID, Revision: 1, Destination: destination, Settings: t.settings()}, Policy: t.policy(), Baseline: baseline, CohortOrder: row.Agents}
			if _, e = s.propose(ctx, &pb.ProposeRequest{Experiment: wire.Experiment(n), RequestId: id}, "candidate-discovery", true); e != nil {
				if !errors.Is(e, errExperimentBudget) && !errors.Is(e, errDiscoverySuppressed) {
					failures = append(failures, e)
				}
				break
			}
		}
	}
	return errors.Join(failures...)
}
