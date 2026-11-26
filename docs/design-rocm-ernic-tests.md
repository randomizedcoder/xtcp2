# rocm-ernic verification, round 2

Status: plan (2026-11-25). Follows [design-rocm-ernic-nics.md](design-rocm-ernic-nics.md), whose phases 0–4 put two emulated ionic NICs in the golden VM.

The emulator counts frames, never time, so the same workload gives the same faults. That makes it an exact outside witness for what xtcp2 reports. Round 1 compared guest counters only with each other; this round compares them, and xtcp2's TCP counters, with the emulator's own counts, fixes a bug found on the way, and pins the uplink choice with tests.

## Background

PR #335 put two ionic NICs in the golden VM:
- **ionic0** is in the root netns, is uplink 1, and has fault `eth_tx: every 50 drop`.
- **ionic1** is in netns golden-ernic-b, with fault `eth_rx: every 64 drop`.

Gaps found while planning:
1. **Egress metric bug:** `insertRoute` (`pkg/localnet/localnet.go:333`) lets a later route with the same prefix overwrite an earlier one. The kernel lists same-prefix routes in ascending metric order:
   - IPv4: `fib_find_alias`, net/ipv4/fib_trie.c:1006.
   - IPv6: `fib6_add_rt2node`, net/ipv6/ip6_fib.c:1212.

   So with two default routes, `enrich_socket_dest_egress_ifname` names the **highest**-metric route, the one the kernel uses last. The parsed `RouteInfo.Priority` (`pkg/xtcpnl/xtcpnl_rtmsg.go:108`) is never used.
2. **Emulator counts are lost:** rocm-ernic prints `Fault <site>: eligible X, fired Y` only on a clean SIGINT/TERM shutdown. Two things throw that away today:
   - `mkGoldenCaptureRunner`'s EXIT trap (`nix/microvms/lib.nix:1296`) runs `rm -rf "$STAGE"` before it stops the VM, which deletes `$STAGE/ernic/*.log`.
   - `ernic-run-many`'s `stop_group` (`nix/ernic/run-many.nix:138`) waits only for the group leader, then SIGKILLs the group. That can kill a server before it calls `report_faults`.
3. **Frames could be checked exactly against TCP counters:**
   - `tp->segs_out` counts every transmitted segment, including SYNs, pure ACKs and retransmits (net/ipv4/tcp_output.c:1758).
   - `segs_in` counts every received skb (include/net/tcp.h:2836); a child socket starts at 1 (tcp_minisocks.c:574).
   - With TSO/GSO/GRO off, one segment is one frame.
   - So the frames the emulator saw on `eth_tx` should equal the sum of the senders' `tcp_info_segs_out` as xtcp2 reports it.
4. **Two flakes predate this work:**
   - netem `loss_seen` fails in about 1 run in 5.
   - In 1 of 5 runs, one stray frame reaches ionic1.
5. **Uplinks:** `nicinfo.DefaultUplinks` (`pkg/nicinfo/uplinks.go:37`) never reads the metric. It relies on the kernel listing order and runs once at startup (`pkg/xtcp/enrich.go:111`). The tests' route helpers write metric 0 (v4) or a single fixed metric (v6), so nothing pins that reliance.

**Decisions:** one PR for all phases, the egress fix included; the golden ionic profile stays at 100000 Mb/s.

## Phase 0: spike (findings go in the doc; no code kept)

One capture with local hacks, to confirm:
1. A clean TERM (after the run-many fix) prints the `Fault` lines for ionic0 and ionic1.
2. Taking ionic0/ionic1 down after the final counter read freezes the counters: no frames are sent at poweroff.
3. On one capture, sum(client `segs_out`) for the 4 ionic clients equals `tx_packets + tx_dropped` on ionic0. If it is off by a constant, find the cause in the kernel source and record it with file:line.
4. A free PCI slot pair for ionic2/ionic3. Slot 0x1f is the only one known free, so the fallback is `-device pci-bridge,id=ernicbr,chassis_nr=1,addr=0x1f` with both NICs on `bus=ernicbr`.
5. The phase 5 `sequence` pattern finishes the 4 MiB transfer without a long stall.

## Phase 1: egress follows the lowest metric (Go)

- `routeEntry` gains `prio uint32` (from `ri.Priority`).
- `insertRoute` replaces an existing non-self entry only if the new route's `prio` is strictly lower. On equal prio the first one seen wins, which matches the kernel: its lookup takes the first alias in list order, and the dump order is the list order.
- The self-entry rule is unchanged. The comment cites the kernel lines above.
- **Golden VM row** in netns `golden-metric`:
  - two veths to `golden-metric-peer`, with default routes at metric 100 (`m1`) and 200 (`m2`);
  - a listener in the peer on 192.0.2.80 (lo), and one client connection to it;
  - row `egress_metric`: the client's egress is `m1`.
  - Red on main (it gives `m2`); recapture the fixture.

**T1 `TestLookupEgressMetric`** (`pkg/localnet/localnet_test.go`, in the existing BuildSnapshot style)

| name | category | description | input | expected |
|---|---|---|---|---|
| lowest_wins_dump_order | positive | the lower metric wins in the kernel's order | default m1 prio 100 then m2 prio 200 | egress m1 |
| lowest_wins_reverse | positive | order does not matter | m2 200 then m1 100 | egress m1 |
| specific_beats_metric | negative | longest prefix still beats metric | 10/8 prio 900 on eth1, default prio 1 on eth0; dst 10.1.1.1 | egress eth1 |
| equal_prio_first | boundary | equal metric keeps the first seen, like the kernel | m1 100 then m2 100 | egress m1 |
| prio_zero | boundary | metric 0 (no RTA_PRIORITY) is the lowest | m1 no prio, m2 prio 1 | egress m1 |
| self_kept | corner | a self /32 is never replaced by a lower-metric route | addr 10.0.0.5, route 10.0.0.5/32 prio 0 | LocalitySelf |
| v6_default | corner | IPv6 follows the same rule | ::/0 prio 1024 on m2, prio 256 on m1 | egress m1 |
| multipath_lower | corner | a lower-metric multipath route wins, with oif 0 | multipath prio 10, single-path prio 20 | egress empty (oif 0) |

## Phase 2: keep and read the emulator's counts

- **`nix/ernic/run-many.nix`:**
  - The fabric trap sends TERM to every server, then `wait`s for each one, up to 10 s, before exiting.
  - `stop_group` then needs no early SIGKILL.
  - New option `--pcap IFNAME`: tcpdump inside the fabric writes `DIR/<ifname>.pcap`; used in phase 4.
- **`nix/microvms/lib.nix` `mkGoldenCaptureRunner`:**
  - After `xtcp2-golden check`: power off the VM, `wait` on run-many, copy `$STAGE/ernic/*.log` next to `$LOG`, then run the host check.
  - The EXIT trap's `rm -rf "$STAGE"` moves after the VM stop.
  - The runner prints ROW lines, and fails on `ROW … FAIL`, as `mkFamiliesRunner` already does.
- **Guest (`golden-capture.sh`):**
  - After the last xtcp2 read, it prints one line: `GOLDEN_ERNIC_COUNTERS ionic0 tx=… dropped=… ionic1 rx=… segs_out=… segs_in=…`. The segs values are summed from the captured records, not from `ss`.
  - Then `ip link set ionic0 down`, and the same for ionic1 inside its netns.
- **Host check `ernic-oracle`:** a small shell script in `nix/microvms/scripts/` that parses the `Fault` lines and the COUNTERS line. Its pure logic is tested by T2 against fixture log text.

**T2 `checks.ernic-oracle`** (shell table, fixture log snippets; prints `ROW` lines)

| name | category | description | input | expected |
|---|---|---|---|---|
| tx_match | positive | the emulator's tx count equals the guest's | `eligible 14838, fired 296`; tx=14542 dropped=296 | PASS |
| rx_match | positive | rx eligible equals delivered plus dropped | rx `eligible 9000, fired 140`; ionic1 rx=8860 | PASS |
| fired_mismatch | negative | a guest counter that disagrees fails | fired 296, dropped=295 | FAIL `ionic0 fired 296 != tx_dropped 295` |
| no_fault_line | negative | a missing count (unclean stop) fails | log without a `Fault` line | FAIL `no Fault line for ionic0` |
| zero_eligible | boundary | a site that saw nothing is reported, not divided by zero | `eligible 0, fired 0` | FAIL `ionic0 saw no frames` |
| fired_equals_eligible | boundary | every frame dropped is still consistent | `eligible 50, fired 50`, tx=0 dropped=50 | PASS |
| two_sites | corner | lines for other sites or NICs are not mixed up | ionic0 and ionic1 logs, eth_tx and eth_rx sites | each compared with its own NIC |
| u64 | corner | large counts parse | eligible 18446744073709551615 | parsed exactly; no overflow |

## Phase 3: frame accounting, the emulator against xtcp2's TCP counters

These are new ROWs. Guest rows go in `golden-capture.sh`; the exact three-way comparison is in the host `ernic-oracle`.

| name | category | description | input | expected |
|---|---|---|---|---|
| segs_out_frames | positive | client segments equal ionic0 tx frames | sum `tcp_info_segs_out`, 4 ionic clients | == tx_packets + tx_dropped == emulator eligible (± the phase 0 constant) |
| segs_in_frames | positive | server segments equal ionic1 delivered frames | sum `tcp_info_segs_in` of the server children in golden-ernic-b | == ionic1 rx_packets (± constant) |
| retrans_covers_drops | boundary | every dropped data frame was retransmitted | sum `tcp_info_total_retrans`, ionic0 clients | ≥ ionic0 tx_dropped |
| bytes_retrans_mss | boundary | retransmitted bytes match the drops | sum `tcp_info_bytes_retrans` | ≥ tx_dropped × (mss − 1) |
| idle_segs_zero | negative | sockets not on ionic add no ionic frames | non-ionic rows | excluded; the sum is taken over ionic rows only |
| quiescent | corner | the counters are final when read | two reads 1 s apart | equal |

Any column that comes out exactly equal also gets a goldenrec unit row (`pkg/goldenrec/diff_test.go`) only if that column's diff rule changes; otherwise there is no Go change.

## Phase 4: the two flakes

- **Stray ionic1 frame:** run captures with `--pcap br-ernic` until it reproduces, identify the frame, and remove its source. Then `no_noise` again requires ionic1 rx to be 0 (red with the requirement restored, green after the fix).
- **netem `loss_seen` (TODO):** the row's detail now reports netem's own drop count per namespace (`tc -s qdisc`). It failed once in the last 8 captures. netem dropped 97 and 101 packets in golden-bulk-a and golden-bulk-b, yet the dctcp client had 0 retransmits over 4145 segments. The other three clients had 64 to 98, and in a passing run every client has 40 to 110. So the loss did not miss at random: one whole flow got through it. netem has no `ecn` option here, so it drops ECT packets as well. The cause is still open.
  - If found: fix it, and turn the row's per-client check into a table row.
  - If not found within the phase's captures: record it as a TODO in the Status section and keep going.

## Phase 5: one deterministic loss pattern on a single flow

- **New pair:** ionic2 in netns `golden-ernic-c` (10.0.5.15) and ionic3 in netns `golden-ernic-d` (10.0.5.16), both unmanaged, at PCI slots 0x1b and 0x1c. They are on their own fabric bridge, `br-ernic2`, so no frame of the first pair reaches them. The client's own namespace keeps the flow out of the root namespace's ionic0 rows, which select on destination port 4000.
- **ionic2 fault:** `eth_tx: sequence "0x500,1x1,0x500,1x3,0x500,1x8,0x60000"`. The counts are decimal (`1xK` repeats K times; src/ionic/ionic_fault.c upstream), 61512 events in all, so the pattern never repeats within the transfer. It drops frame 501 alone, then 1002-1004, then 1505-1512: 12 drops, all in the first half of the transfer's roughly 3100 frames.
- **Traffic:** one cubic `-bulk 4MiB` echo, the only traffic on either NIC, with keepalive off.
- **ROWs** (guest; the host oracle adds `oracle_tx`, `oracle_segs_out` and `oracle_segs_in` for ionic2 and ionic3):

| name | category | description | input | expected |
|---|---|---|---|---|
| seq_fired | boundary | exactly the pattern's drops | ionic2 tx_dropped (the oracle checks the emulator's fired) | 12 |
| seq_retrans | positive | xtcp2 sees the recovery | `tcp_info_total_retrans` of the flow | > 0 (pure ACK drops are never resent, so not ≥ 12) |
| seq_segs_out | positive | exact frame accounting on one flow | `tcp_info_segs_out` | == tx_packets + tx_dropped |
| seq_no_noise | corner | nothing on the pair before the transfer | ionic2 tx, ionic3 rx | 0 0 |
| seq_quiescent | corner | the counters are final when read | two reads 1 s apart | equal |
| seq_egress | negative | the flow is not attributed to uplink ionic0 | egress ifname | ionic2 |
| seq_no_rto | corner | short bursts recover without a timeout | `tcp_info_total_rto` | 0 (it was 0 in six of six captures, so it is asserted) |

- `DefaultFaultNetns` gains `golden-ernic-c` and `golden-ernic-d`, with red `TestDiff` rows first (`fault_ernic_c`, `fault_ernic_d`). `DefaultFaultEgress` is unchanged: the flow's sockets are already in fault namespaces.

## Phase 6: uplink corner cases (Go unit tests and documentation; no behavior change)

- **Fixtures:** `routeV4`/`routeV6` in `pkg/nicinfo/uplinks_test.go` gain a metric argument.
- **Header comment:** `DefaultUplinks` gets a comment saying it relies on the kernel listing order (ascending metric, with the file:lines above) and is evaluated once at startup.
- **Status section:** records that the uplink columns don't follow a runtime metric swap until restart (a TODO, not changed here).

**T6 additions to `TestDefaultUplinksFixture` / `TestParseDefaultRoutesV4`/`V6`**

| name | category | description | input | expected |
|---|---|---|---|---|
| kernel_order | positive | routes in kernel (metric) order give the uplinks in that order | eth1 100, eth0 200 | [eth1 eth0] |
| v4_before_v6 | positive | IPv4 defaults come before IPv6 ones whatever the metric | v4 eth0 1000, v6 eth1 1 | [eth0 eth1] |
| metric_ignored | negative | the parser does not reorder (it trusts the kernel) | eth0 200 listed before eth1 100 | [eth0 eth1], so the reliance is pinned |
| equal_metric | boundary | equal metric keeps the listing order | eth1 100, eth0 100 | [eth1 eth0] |
| none_physical_fallback | corner | no default route falls back to the sysfs NICs | empty route files, two sysfs NICs | PhysicalInterfaces order |
| same_nic_twice | corner | a NIC with v4 and v6 defaults counts once | eth0 in both | [eth0] |

## Critical files

- `pkg/localnet/localnet.go`, `pkg/localnet/localnet_test.go`
- `nix/ernic/run-many.nix`, `nix/checks/ernic-run-many.nix` (new rows: `clean_stop` positive: a fake server's shutdown line reaches the log; `pcap` positive; `pcap_no_fabric` negative: `--pcap` without a bridge is rejected)
- `nix/microvms/lib.nix` (`mkGoldenCaptureRunner`), `nix/microvms/scripts/{golden-capture,golden-workload}.sh`, new `nix/microvms/scripts/ernic-oracle.sh`, `nix/checks/ernic-oracle.nix` (registered in `nix/checks/default.nix`)
- `nix/microvms/golden-ernic.nix` (ionic2/ionic3), `nix/microvms/mkVm.nix` (golden-metric veths if they live in networkd)
- `pkg/goldenrec/diff.go` + test, `pkg/nicinfo/uplinks.go` (comment) + `uplinks_test.go`
- `pkg/xtcp/testdata/golden/{coverage.json,7_3_1/*}` (recapture), `nix/README.md`, `CHANGELOG.md`, `docs/design-rocm-ernic-tests.md`, `docs/README.md`

## Verification

1. `TMPDIR=/tmp go test ./pkg/localnet/... ./pkg/nicinfo/... ./pkg/goldenrec/...`, red then green per phase; then `go test ./...`.
2. `nix build .#checks.x86_64-linux.{ernic-oracle,ernic-run-many,ernic-nics,golden-records}`.
3. Golden: five captures, with every ROW PASS (guest and host), `xtcp2-golden diff` clean pairwise, and the oracle three-way equality in every capture.
4. `checks.vm-all` still passes (the run-many change touches only golden; the ionic VM checks use `ernic-run`).
5. The full nix check list before pushing: gofmt, go-vet, nix-fmt, lint-baseline, golangci ×3, redteam-lint, golden-records, test-fuzz-all, ernic-*, upstream-pins. No shellcheck disables, no nolint.
6. Re-run the fleet shape-diff and report it in chat (the speed stays 100000).

## Process

- One PR; the doc lands first, then one test-first series per phase, each red commit before its fix.
- Captures: one VM at a time, after checking the host load and the forwarded ports.
- A Status section is added at the end with the phase 0 findings and the measurements.

## Status

### Findings

- **Egress metric (phase 1):** fixed. `insertRoute` now keeps the lower metric, and on a tie the first route seen, as the kernel orders the routes (`fib_find_alias`, net/ipv4/fib_trie.c:1006; `fib6_add_rt2node`, net/ipv6/ip6_fib.c:1212). The golden row `egress_metric` checks it in netns golden-metric-a, which has default routes over m1 (metric 100) and m2 (metric 200).
- **Emulator counts (phase 2):** kept. The fabric's TERM trap waits for every server before it stops tcpdump. The runner powers the VM off and stops run-many before it deletes the stage, and copies the server logs and any pcap next to the transcript (`<log>.ernic/`). `ernic-oracle` then compares each fault site with the guest's counters; it has been exact in every capture.
- **Frame accounting (phase 3):** exact, with no constant. The emulator's `eth_tx` eligible count on ionic0 equals the sum of `tcp_info_segs_out` over the root namespace's sockets out of ionic0, and ionic1's delivered frames equal the sum of `tcp_info_segs_in` in golden-ernic-b. For example, one capture had eligible 14948 and segs_out 14948, and rx 14422 and segs_in 14422. Two sources of extra frames had to go first:
  - **TCP keepalive:** Go's default of 15 s sent a probe and its ACK on every connection after xtcp2's last poll, 2 frames per flow on each side. The ernic workload now passes `-keepalive -1s`, a new flag on tcp_client and tcp_server.
  - **The stray ionic1 frame:** a Linux bridge with multicast snooping on joins 224.0.0.106 (all-snoopers) when it comes up (`br_dev_open`, net/bridge/br_device.c:174) and floods an IGMPv3 report to every port. The pcap showed it. The fabric bridges now run with `mcast_snooping 0`, and `no_noise` again requires 0 frames on both NICs before the transfers.
- **A dropped SYN (found later):** in 2 of 12 captures `oracle_segs_out` was 1 or 2 short, while the kernel's own sum (`ss`, printed beside xtcp2's) agreed with xtcp2. The pcap on tap-ionic0 showed one SYN with no reply, and a new connection from another port 1 s later. ionic1 had dropped the SYN (every 64th frame). tcp_client's 1 s dial timeout then closed that socket just as the kernel's first SYN retransmit came due (`TCP_TIMEOUT_INIT`, include/net/tcp.h:174), and the closed socket's `segs_out` was lost. The ernic clients now use `-dialTimeout 30s`, so the kernel resends the SYN on the same socket.
- **Dropped rows:** `retrans_covers_drops` and `bytes_retrans_mss` were not added. The bulk transfers are echoes, so some dropped frames are pure ACKs for the echoed data, and TCP never retransmits those. Retransmits can therefore be fewer than the drops.
- **netem `loss_seen`:** the row's detail now reports netem's own drop count per namespace (`tc -s qdisc`). No failure has happened since, so the cause is still open (TODO).
- **Uplinks (phase 6):** `DefaultUplinks` trusts the kernel's listing order (ascending metric), puts IPv4 before IPv6, and runs once at startup (pkg/xtcp/enrich.go). `TestDefaultUplinksMetric` pins that behavior. TODO: the uplink columns don't follow a runtime metric change until xtcp2 restarts. Egress and locality refresh every 60 s, so only the uplinks lag.
- **Keepalive probe in flight (diff flake, TODO):** in one of five captures, `xtcp2-golden diff` reported an idle `golden-cc-a` connection with `inet_diag_msg_retrans` 1 and `tcp_info_probes` 1, but `total_retrans` 0 and nothing unacked. The poll caught Go's default 15 s keepalive probe before its ACK: the probe sets `icsk_probes_out` (net/ipv4/tcp_timer.c:844), `tcp_ack` clears it (net/ipv4/tcp_input.c:4430), and inet_diag reports it as `idiag_retrans` while the keepalive timer is pending (net/ipv4/inet_diag.c:295). This predates this work. A fix is `-keepalive 1h` on the idle workloads: the keepalive timer still shows (`inet_diag_msg_timer` 2), but no probe is sent during a capture.
- **Captures:** five, all 47 rows PASS on each (one at host load 18.6), with the oracle exact on all four NICs every time. `xtcp2-golden diff` is clean between captures 1, 3, 4 and 5; capture 2 differs only by the keepalive probe above. The sequence flow sent about 3100 frames with 12 drops, retransmitted 11 or 12 times (an 11 means one drop was a pure ACK), and never had an RTO, so `seq_no_rto` is asserted.
- **After the SYN fix:** eight more captures with a pcap on tap-ionic0. In seven, every row passed and the oracle was exact on all four NICs. The eighth failed only `loss_seen`, the netem TODO above, and aborted before the oracle ran.
