# goip ↔ `ip` parity: nl_mon event captures, monitor replay, and iproute2 findings on the record

## Status

**Last updated:** 2026-11-13

**Summary:**
- Every plan before this one is closed ([goip-parity-next.md](goip-parity-next.md) through NX10, V078; [goip-parity-gaps.md](goip-parity-gaps.md) through G7). The dumps, writes, readbacks and the genl/XFRM objects are captured, parsed, rendered, replayed and gated.
- The **event half** of the corpus did not move with them. This plan is that increment: more nl_mon pcaps, wired the way the rest already are, so goip-parity can keep growing — and an **iproute2-findings record** so every bug the captures surface lands somewhere that cannot rot.
- Five facts, each verified in the tree:
  1. **The base event corpus is one kernel old and frozen.** The only committed `RTM_NEWLINK/DELLINK`, `RTM_NEWADDR/DELADDR`, `RTM_NEWROUTE/DELROUTE`, `RTM_NEWNEIGH/DELNEIGH` notifications are `pkg/xtcpnl/testdata/7_1_4/netlink_route_events*.pcap`, from the `nlmon-capture` oneshot. Everything else has moved to 7.3.0 (`dumps/`, `link-write/`) and 7.3.1 (`ws/`).
  2. **The oneshot predates every fix the driven machinery made.** `nlmonCaptureRunScript` (`nix/microvms/mkVm.nix:2104`) syncs on `WARMUP=3` sleeps (`:2121`, `:2174`) — the recorded root cause of empty pcaps elsewhere in the suite — and has none of `xtcp2-nlcap`'s bind wait, `--immediate-mode`, `-B 32768`, `ss -x` stop sentinel, drop-count readback or floors (`nix/microvms/netlink-capture.nix`, header). Changing the sequence means rebuilding the VM, and the VM cannot be told to stop.
  3. **Events are decoded and rendered but never compared.** [coverage-status.md](coverage-status.md#three-claims-that-are-routinely-conflated) records it: "rendered by `goip monitor`, but never compared live: the parity driver runs no monitor." `internal/goipparity/commands.go:7349` (ioam) and `:7394` (mptcp) say the same of their monitor rows: "not here: the driver runs no monitor."
  4. **The notification types with no capture anywhere.** Verified against the kernel tree (not assumed): `RTM_NEWNEXTHOPBUCKET` notifications exist (`net/ipv4/nexthop.c:1222-1231`, RTNLGRP_NEXTHOP, on resilient bucket replacement) and `RTM_DELNETCONF` exists (`net/ipv4/devinet.c:2761`, on device unregister) — neither has a committed notification. And three event *shapes* the corpus never held: an altname change (the kernel answers `RTM_NEWLINKPROP` with an ACK and notifies an `RTM_NEWLINK` carrying the new `IFLA_PROP_LIST`, `net/core/rtnetlink.c:4503`), DAD's tentative→final second `RTM_NEWADDR`, and a bulk mixed-family sequence with its ordering.
  5. **Three iproute2-bug notes are referenced from the closed plan and do not exist.** [goip-parity-next-status.md](goip-parity-next-status.md) cites `upstream/iproute2-rta-addattr-uninit-padding.md` (V069), `upstream/iproute2-print-json-bugs.md` (the P14 decision) and `upstream/iproute2-rt-names-table-load.md` (the NX7f decision) — all three dead links today. Only the bond note ([upstream/iproute2-bond-partner-state-width.md](upstream/iproute2-bond-partner-state-width.md)) is written.
- **Captures come first** (Tier EC): the event rows in the workbench, the missing notification families, both iproute2 versions, `nlcapgen verdiff`. Then replay and parsers (Tier EP), then goip and the matrix (Tier EG). Tier UB — the findings record — can start immediately, because it is debt already owed.
- Progress is in [goip-parity-events-status.md](goip-parity-events-status.md).

## Contents

- [Reference sources](#reference-sources)
- [What the increment delivers](#what-the-increment-delivers)
- [Out of scope](#out-of-scope)
- [Gates every task inherits](#gates-every-task-inherits)
- [Tier EC: captures](#tier-ec-captures)
- [Tier EP: replay and parsers](#tier-ep-replay-and-parsers)
- [Tier EG: goip and the matrix](#tier-eg-goip-and-the-matrix)
- [Tier UB: the iproute2 findings record](#tier-ub-the-iproute2-findings-record)
- [Follow-ups after Tier EG](#follow-ups-after-tier-eg)
- [See also](#see-also)

## Reference sources

- **iproute2:** `~/Downloads/iproute2` (read-only), at tags `v7.2.0` (the nixpkgs ip the workbench guest runs) and `v7.3.0` (the parity guest's pin, `nix/iproute2-7-3-0.nix`). Line numbers here are `git show v7.2.0:…` unless they say 7.3.0. The monitor's group joins are `ip/ipmonitor.c:267-367`; the offline replay path is `ip -d monitor file` (`ip/ipmonitor.c:322` → `rtnl_from_file`, `lib/libnetlink.c:1296`), the same path the bond note's testsuite test rides.
- **Linux:** `~/Downloads/linux` (read-only). The workbench guest runs nixpkgs' `linux_latest`, 7.3.1. Kernel claims below cite the measured lines.
- **xtcp2:**
  - the workbench (`nix/microvms/workbench/`, host CLI `nix run .#nlcap-ws`);
  - the generator (`tools/nlcapgen/`, `-family` matrices, `verdiff`);
  - the corpora (`pkg/xtcpnl/testdata/<kver>/ws/`, `ws-ip730/`, `link-write/`);
  - the monitor helpers (`xtcp2-nl-ipmon`, `xtcp2-nl-brmon`, `xtcp2-nl-xfrmmon`, `xtcp2-nl-ioam`, `xtcp2-nl-mptcp`, `nix/microvms/netlink-capture.nix`);
  - goip's monitor (`internal/goip/obj_monitor.go`, `file FILE` included);
  - the oracles O3/O5/O9 and their harnesses (`pkg/xtcpnl/xtcpnl_ws_*_test.go`, `internal/goip/ws_*_test.go`).

## What the increment delivers

Four things, in dependency order:

1. **The event corpus as rows** (EC0–EC2): every notification family goip's monitor renders, one pcap per row, each with the `ip monitor` output of the *same run* as its oracle sidecar — the `capio` discipline ("both halves land together or neither does", `netlink-capture.nix`) applied to events. The four base families recaptured at 7.3.1; the notification types that have never been captured; the mixed-family sequence and the deliberate `NLMSG_ERROR`.
2. **Both iproute2 versions, diffed** (EC3): the rows run at 7.2.0 and 7.3.0 and `nlcapgen verdiff` compares them, the NC3-T01 method. `ip monitor`'s printing is where iproute2's decode bugs live — the bond partner-state fix (upstream `0067cbc8`, first released in 7.3.0) changed `ip -d monitor` output between exactly these two versions.
3. **The offline monitor replay** (EP1–EP2): the crafted-blob path the bond note proved with — a `.nl` file replayed through `ip -d monitor file` and `goip monitor file` — becomes a committed artifact class with a nix check, and O10 replays every captured event pcap through goip's monitor against its `.out`.
4. **The findings record** (UB0): the three owed notes written, an index, and a docs check that fails on a referenced note that does not exist — so the next finding cannot be a dead link.

## Out of scope

- **`RTM_NEWADDRLABEL`/`RTM_DELADDRLABEL` events:** the kernel never notifies them. `net/ipv6/addrlabel.c` has no `rtnl_notify` call; a write answers only an ACK. The universe gate keeps counting the request forms the `addrlabel_*` rows already hold.
- **`RTM_NEWNEIGHTBL` events:** same. `net/core/neighbour.c`'s set path answers an ACK; its one notify is `RTNLGRP_NEIGH` (`:3609`). Dump replies only.
- **`RTM_NEWNDUSEROPT`:** `ip monitor` joins no `RTNLGRP_ND_USEROPT` (`ip/ipmonitor.c:267-367`), `accept_msg` has no arm for it, and it has no decoder. A subscriber would have to be ours, so it is daemon-side coverage, not parity; it stays out with this reason.
- **`RTM_NEWLINKPROP`/`RTM_DELLINKPROP` notifications:** they do not exist. The kernel answers the write with an ACK and notifies `RTM_NEWLINK` (`net/core/rtnetlink.c:4503` → `netdev_state_change`). The *event* row is the altname `RTM_NEWLINK`; the *request* rows (`link_altname_*`) already exist.
- **Live monitor parity rows:** Tier EG is an outline here. A monitor row is a new row kind in the parity harness (two subscribers, one trigger), and its tables are written from EC's rows when EC closes, per the "captures first" rule.
- **Bigger-endian evidence:** the s390x cross-run that proved the bond bug stays a notebook procedure in the note, not a check; a `pkgsCross.s390x` closure in a registered check is a cost/benefit decision for later.

## Gates every task inherits

- **Captures** (Tier EC), as the closed plans' LF2/LE3/LX3/NC8-T05 gates:
  - each family is run in two fresh-boot workbench batches;
  - the two must agree row for row on rc and message types, and every rc must be as the generator wrote it;
  - the first batch is the committed corpus;
  - a flaky row is fixed, or marked `corner:` with the reason;
  - `ws/` stays inside the corpus test's size budget.
- **Before a boot:**
  - `free -g`;
  - the port check (`pgrep -af '[x]tcp2-goip-parity-x86|[m]icrovm@xtcp2-vm|[s]3parquet-stress'`) and the workbench's own ports;
  - prebuild, then wait;
  - wait for the host load to settle.
- **Unit tests:** table tests whose descriptions start with `positive:`, `negative:`, `boundary:` or `corner:`, each row with its expected value. Also `go test -count=1 -race` over the touched packages, and `go vet`.
- **Mutation checks** in a scratch worktree, each mutant under `systemd-run --user --scope -p MemoryMax=6G` with `-timeout=180s`.
- **Nix checks, one per call:** lint-baseline and netlink-audit on the task's diff. The workbench tasks add vm-lib-exp, nix-fmt, statix, deadnix and codespell. Lint findings are fixed in code, never with nolint.
- **O5/O9/O10** (Tier EP and EG): regenerated in the task's commit. `differs` and `sends` stay 0, and no row moves away from `same`.
- **VMs** (Tier EG): two clean `--section` runs before a row is gated. Before any push, a clean full run and the whole-repo `test-go-race`.

## Tier EC: captures

### EC0 — the event row

#### EC0-T01 the monitor helpers take `--ip`

- **The gap.** `xtcp2-nlcap` honors `NLCAP_IP_BIN`; the monitor helpers do not. `nix/microvms/netlink-capture.nix:1131-1138` says so outright: "The monitor helpers (ipmon and the rest) keep their own PATH and their own ip." A row at iproute2 7.3.0 would run its trigger through 7.3.0 and its monitor through 7.2.0 — two versions in one `.out`, and the oracle is not the oracle.
- **The change.** `xtcp2-nl-ipmon`, `xtcp2-nl-brmon`, `xtcp2-nl-xfrmmon`, `xtcp2-nl-ioam` and `xtcp2-nl-mptcp` prefix `NLCAP_IP_BIN` on PATH the way `xtcp2-nlcap` does, before their own runtimeInputs. `xtcp2-nl-mptcp`'s and `xtcp2-nl-ioam`'s subprocess `ip` calls inherit it. A bad path (no `ip` in it) is a usage error, as in `xtcp2-nlcap`.
- **The hermetic rows.** `nix/checks/vm-lib-exp.nix` gains a row per helper: with `NLCAP_IP_BIN` set to a directory holding a shim `ip` that prints its version and execs the real one, the monitor's banner/version output proves the shim ran; unset, the pinned iproute2 runs, as before.

| description | input | expected |
|---|---|---|
| positive: the shim runs | `NLCAP_IP_BIN` with the shim | the monitor's `ip` is the shim's |
| positive: unset | — | the pinned iproute2, as before |
| negative: no `ip` in the directory | `NLCAP_IP_BIN=/nix/store/nosuch` | usage error, no monitor started |
| corner: the batch's `--ip` | `nlcap-ws batch --ip <7.3.0>` | pre steps, command, monitor and readbacks all 7.3.0 |

#### EC0-T02 the `ev` family

- **The shape.** `tools/nlcapgen/matrix_ev.go`, registered as `-family ev`, writes `ev.rows.jsonl` beside the other rows files. An event row's `cmd` is one monitor-helper invocation — monitor, trigger, `until` EREs — inside the capture window, `write` mode (one attempt: a trigger changes the namespace), with the helper's stdout the row's `.out`, the oracle from the same run.
- **The canary rule stays.** A monitor is "subscribed" only when it has proved it: `ipmon`'s multicast join on `c0`, `brmon`'s VLAN 4000, `xfrmmon`'s `/proc/net/netlink` group read. The canary's events stay in the pcap and the `.out`; the golden pairs every notification with every block, both ways, as the addr plan's A1-T02 gate did.
- **The discipline the oneshot lacked.** Each row's floor is its datagram count from a probe batch; the sentinel, immediate mode, the 32 MiB ring and the drop-count readback are `xtcp2-nlcap`'s and come for free. No `sleep` waits: DAD settles on `wait-addr-flag`, carrier on `wait-linkwatch`, LACP on `wait-lacp`, each of which already exists for the write corpora.
- **A probe batch first** (as every family before it): the forms below run once, their datagram counts and monitor output recorded in the status doc, and the floors and `until` EREs written from the measurement, not from a guess.

| description | input | expected |
|---|---|---|
| positive: a row's cmd | `xtcp2-nl-ipmon c0 "monitor link maddress" "<trigger>" "<ERE>"` | pcap holds the monitor's deliveries and the trigger's requests; `.out` holds the monitor's lines |
| negative: the monitor never subscribes | a canary the guest refuses | helper rc 1, row discarded, not kept short |
| corner: the canary in the pcap | every `ipmon` row | counted in the floor, paired in the golden |
| boundary: one attempt | a trigger that changes state | no retry, as every write row |

### EC1 — the four base families at 7.3.1

The `nlmon-capture` oneshot's sequence, as rows. The committed 7_1_4 pcaps and sidecars stay; the split generator (`TestExtractEventFixtures`) keeps reading them.

- **`evlink`** — veth carrier down/restore (the `IsCarrierDown` vs `IsAdminDown` distinction), admin down/up, a dummy add/del for a clean `RTM_DELLINK` not entangled with a peer, and the altname change (an `RTM_NEWLINK` carrying `IFLA_PROP_LIST`, verified above).
- **`evaddr`** — v4 and v6 add/del, plus the DAD pair: the tentative `RTM_NEWADDR` and the final one after `wait-addr-flag` clears `TENTATIVE`, and the `dadfailed` corner (`dad_transmits 1`, a duplicate on the peer).
- **`evroute`** — on-link, via-gateway, table 100, blackhole, v6, and the kernel-internal autoconf routes (pid 0, the case the flags discriminator exists for).
- **`evneigh`** — permanent → stale → del, v4 and v6.
- **`everr`** — the deliberate negative: delete a route that does not exist, so the window holds a genuine `NLMSG_ERROR` (not a notification; the old capture carried one and the parser's "not an event" path needs a real one).
- **`evseq`** — the bulk capture's ordering property as one row: one `ip monitor all` over a trigger touching link, addr, route, neigh and netconf, its `.out` the oracle, the pcap the mixed time sequence. This is `monitor_all` (`rtnl-write-corpus.exp`) generalized to the four base families it never triggered.

| description | input | expected |
|---|---|---|
| positive: each family's rows | the probe's forms | every notification of the family, and only it, in the row's pcap |
| positive: DAD's second NEWADDR | `evaddr`, `wait-addr-flag` | tentative then final, two `RTM_NEWADDR` |
| corner: altname | `evlink` | `RTM_NEWLINK` with `IFLA_PROP_LIST`, no `RTM_NEWLINKPROP` |
| corner: the mixed sequence | `evseq` | the `.out`'s blocks in the pcap's order |
| negative: the missing route | `everr` | `NLMSG_ERROR` −ESRCH in the window |
| boundary: floors | every row | the probe's datagram count, no lower |

### EC2 — the notification families with no capture

Each is verified to exist in the kernel tree; each gets its rows from a probe batch. `evmaddr` (the `addr-monitor` rows) and the `xfrm`/`brmpls` monitor rows already exist at 7.3.0 and are not redone.

- **`evnhb`** — resilient nexthop bucket replacement: `ip nexthop replace` on a `type resilient` group under `ip monitor nexthop`. The kernel notifies `RTM_NEWNEXTHOPBUCKET` with `NLM_F_REPLACE` (`net/ipv4/nexthop.c:1222-1231`). The parity corpus holds bucket *dump replies*; no notification.
- **`evconf`** — `RTM_NEWNETCONF` on a forwarding flip (already in `monitor_all`'s trigger) and **`RTM_DELNETCONF` on device unregister** (`net/ipv4/devinet.c:2761`, `net/ipv6/addrconf.c:7411`): d0 with forwarding on, then `ip link del d0`.
- **`evrule`** — `RTM_NEWRULE`/`RTM_DELRULE`: the rule selectors of `rule_add_selectors` under a monitor. `monitor_all` holds one add; no selector set, no delete pair, no v6.
- **`evstats`** — `RTM_NEWSTATS` beyond the one `l3_stats on`: the off/on pair, and `-s`-shaped updates if the probe shows them.
- **`evnsid`** — the `nsid_del_event` row holds the del; add the set and the assign-to-a-live-netns forms.
- **`evprefix`** — the `prefix_event` row holds one RA; add the deprecated-lifetime corner and the v6-route-from-RA pair.

| description | input | expected |
|---|---|---|
| positive: a bucket replacement | `evnhb` | `RTM_NEWNEXTHOPBUCKET`, `NLM_F_REPLACE`, in the window |
| positive: netconf del | `evconf` | `RTM_DELNETCONF` with the device's ifindex |
| positive: rule add/del pair | `evrule` | both notifications, the del's `Deleted` prefix in `.out` |
| corner: unregister without forwarding | `evconf` | no `RTM_DELNETCONF`, only `RTM_DELLINK`; recorded as a negative row |
| boundary: floors | every row | the probe's count |

### EC3 — both iproute2 versions, and `verdiff`

- **The replay.** Both families (EC1, EC2) run at the guest's 7.2.0 and at 7.3.0 (`nlcap-ws batch --ip $(nix build --print-out-paths '.#iproute2_7_3_0^out')`), the NC3-T01 method: 7.2.0 twice as the noise baseline, then 7.3.0, each on a fresh boot. Rows land in `testdata/7_3_1/ws/ev*` and `testdata/7_3_1/ws-ip730/ev*`; the corpus tests read both roots and check each row's recorded `iproute2` version against its root.
- **The diff.** `nlcapgen verdiff` over the two directories. Expected from the source, before the run:
  - `ip monitor`'s banners and object lines are unchanged between the two;
  - no request bytes change except ip's uninitialized `rta_addattr_l` padding (the known leak, [UB0-T01](#ub0-t01-the-three-owed-notes));
  - a 7.3.0-only difference in any monitor *line* is a finding: it is a decode change in `ip`'s print path, and every one of those so far (bond partner state, `test_on_not`, P14, the names tables) was a bug.
- **The finding rule.** A difference outside the expected set, present at 7.3.0 on both 7.2.0 boots' agreement, is written up in [Tier UB](#tier-ub-the-iproute2-findings-record) before any goip code changes to match it.

| description | input | expected |
|---|---|---|
| positive: identical dirs | the same dir twice | 0 differences |
| positive: the noise baseline | the two 7.2.0 boots | 0 differences outside the traffic corners |
| negative: a monitor line changed | a fixture pair | the row, the line, both versions |
| corner: seq and pid differ | fixture | not a difference |
| boundary: every row at both versions | the two roots | row for row, the same ids |

### EC4 — the tier closes

#### EC4-T01

- Recount every new family's rows, rcs and message types.
  - `TestNetlinkUniverse` needs no new names (every type in the families is already counted); the exclusion list gains nothing (addrlabel and neightbl events do not exist — recorded in [Out of scope](#out-of-scope), which is the universe gate's own "excluded with a reason" discipline applied to events).
  - Write Tier EP's and Tier EG's task tables into this plan from the rows, with each row's expected value.

| corpus | rows | rc 0 | notes |
|---|---|---|---|
| `7_3_1/ws/ev*` | from EC4-T01 | — | 7.2.0 |
| `7_3_1/ws-ip730/ev*` | the same ids | — | 7.3.0 |

## Tier EP: replay and parsers

Each task gets a table test from the captures, a benchmark over the corpus picks and `FuzzParse*` seeds. Tier P's tables are written at EC4-T01; the two below are fixed by the increment's design, not by the rows.

### EP1 O10 — the monitor replay golden

- **The oracle.** Every event row's `.out` is `ip monitor`'s decoded output of the same run. A new corpus test (O10, after O3/O5/O9) replays the row's pcap through goip's monitor — in-process, `file FILE`'s walk, `ParseNetlinkPcap` for the records — and holds goip to that `.out`, text and errors. The `.out`'s canary blocks pair with their notifications, both ways, the A1-T02 rule.
- **What it buys.** Every event render goip prints becomes replay-grounded: 7.3.1 bytes, ip 7.2.0's words. A gap is named by the first word of ip's line goip lacks, as O9 names readbacks (`ws-readbacks.txt`); the goal is 0.

| description | input | expected |
|---|---|---|
| positive: a family row | `evlink/ec-001` | goip's monitor output is the `.out` |
| corner: the canary block | every `ipmon` row | printed, paired |
| negative: a render goip lacks | a finding | `lacks "…"` in the golden |
| boundary: the empty monitor | a row whose trigger refuses | no lines, rc as recorded |

### EP2 the crafted monitor files, at two versions

- **The artifact class.** `.nl` blobs of raw netlink messages — iproute2's own testsuite format (`testsuite/tools/generate_nlmsg.c`, `ip -d monitor file`) — committed under `internal/goip/testdata/monitor/`. They are *not* pcap fixtures and break no "real captures" rule: they exist for inputs the kernel cannot produce, the same carve-out the bond note states ("the out-of-range value *is* the input under test"). Each carries a comment naming why the kernel cannot send it.
- **The rows.** The bond note's table, whole: fast `0x3f`, slow `0x3d`, zero, all-bits `0xff`, high-byte `0x3f01`, high-only `0x0100` (the row that fails on any host), absent, truncated, and the actor-stays-u8 pair. P14's reason u32 (`0x9`, `0x100`, `0x80000001`). A one-RTA_METRICS route with `cubic\0p\0` padding. Rows for whatever EC3's verdiff adds.
- **The check.** `test-goip-monitorfile`, a nix check on the `test-linkmonitor-rdma-runtime` pattern: it builds nixpkgs' iproute2 (7.2.0) and `iproute2_7_3_0`, converts each blob (and, via a small `nlcapgen monfile`, a chosen captured pcap or two) to a `.nl`, replays each through both `ip -d monitor file` and `goip monitor file` at both versions' knobs, and compares against committed goldens. ip's side is pinned per version — this is the check that would have caught `0067cbc8` and P14 before the notes existed.

| description | input | expected |
|---|---|---|
| positive: the hionly blob at 7.2.0 | `0x0100` partner state | `ip` prints `0 <>`; goip, at the 7.2.0 knob, the same |
| positive: the hionly blob at 7.3.0 | the same blob | `256`, `<active>` |
| positive: P14's reason 8 alone | `0x100` | nothing printed, on both |
| corner: the truncated partner attribute | `3f` | a decode error, not `63` |
| negative: not a netlink file | a text file | both tools' refusal, rc as each exits |
| boundary: the actor pair | `3f` as the actor attribute | `63`, valid as u8 |

## Tier EG: goip and the matrix

Outline; its tables are written at EC4-T01, from the rows.

- **EG1 — the monitor section.** A new row kind in the parity harness: the row starts `ip monitor <objects>` and `goip monitor <objects>` in the row's namespace, runs the trigger, stops both on the `until` EREs, and the comparator diffs the two monitors' stdout with the D_control discipline (an `ip`-only control run around the goip one, as `ip_a → goip → ip_b` does for dumps). The hygiene rule inverts for these rows only: a notification is the point, not pollution. Timestamps are excluded (`-t` is never used in a compared row; the `monitor_all_timestamp` row stays a capture, not a comparison).
- **EG2 — the offline section.** The EP2 blobs join the matrix as rows: `ip monitor file` against `goip monitor file` on the same blob, so the crafted inputs are gated, not just checked.

## Tier UB: the iproute2 findings record

Can start immediately; UB0-T01 is debt already owed, not new work.

### UB0-T01 the three owed notes

Written from the closed plan's recorded evidence, every iproute2 claim at `file:line`, each with the shape of the bond note: summary, where, why it is wrong, who sees it, results, how to run it, what it means for goip, sending it (or why not). Each names the capture or row that evidences it.

| note | the finding | evidence in the ledger |
|---|---|---|
| `iproute2-rta-addattr-uninit-padding.md` | `rta_addattr_l` copies `RTA_DATA`'s padding from the caller's stack, so a request byte varies run to run | V038's `cubic\0p\0` vs `cubic\0\0\0`; V069's 9 request diffs, all padding; fixed upstream after 7.3.0 (`c5b76cbb`) |
| `iproute2-print-json-bugs.md` | P14: `print_proto_down` reads the u32 `IFLA_PROTO_DOWN_REASON_VALUE` as a u8, tests `tb` instead of the nest's table, and the text has no space before `protodown_reason` | V065; linkpd ec-004, pw-006, rn1-004 |
| `iproute2-rt-names-table-load.md` | R1: an off-by-one in `rtnl_tab_initialize`; R2: `rt_dsfield` loads lazily, so DS field 0 prints `0` until another lookup loads the file | V062, V066; the 32 corpus readbacks with `dscp 0` |

### UB0-T02 the index and the check

- **`docs/netlink/upstream/README.md`:** one line per note — the finding, the versions affected, whether it is fixed upstream, the pcap/row that evidences it, and whether it has been sent. The bond note's series and its two-posting alternative link from there.
- **`test-goip-docs`,** a registered check on the `test-linkmonitor-docs` pattern (`nix/tests/linkmonitor.nix:233`, `nix/tests/linkmonitor_report.py`): every markdown link under `docs/netlink/` resolves — including `upstream/…` — and every note under `docs/netlink/upstream/` is linked from somewhere, so a note cannot rot unread. Run once before the notes land: it must fail three times, once per dead link, and pass after UB0-T01. That red/green is the task's evidence.

| description | input | expected |
|---|---|---|
| positive: all links resolve | the tree | pass |
| negative: a referenced note missing | the tree before UB0-T01 | 3 failures, one per dead link |
| negative: an orphan note | an unlinked note | failure, naming it |
| corner: the patches and txt files | `upstream/*.patch`, `*.txt` | listed by the index, not link-checked line by line |

### UB0-T03 the findings procedure

A section of `upstream/README.md`, written once, that says when a note is owed, so the next capture does not decide this for itself:

- A capture, `verdiff` or replay difference that both 7.2.0 boots agree on and that is `ip`'s print path or request bytes (not the kernel's, not goip's) obligates a note, in the same task, before goip is changed to match.
- goip replicates `ip`'s behavior, faults included, behind the version knobs (the P14 doctrine), and the note's "What it means for goip" says so.
- Inputs the kernel cannot produce get a crafted-blob row (EP2) so the fault is exercised, not just described.
- A note is sent upstream when the user decides, exactly as the bond one was; the index records the state.

## Follow-ups after Tier EG

- **Kernel spread for events.** The rdma capture's per-kernel suffix pattern (`microvm-x86_64-rdma-capture-6_1` …) applied to the event rows, so notification attribute presence is grounded across kernels the way the sock_diag and rdma corpora are. Decide at EG's close whether any decoder branch depends on a kernel-conditional attribute first.
- **The oneshot's retirement.** Once `ev*` rows hold everything `nlmon-capture` captures, decide whether the flavor stays (as the one baked-in, zero-driver capture) or is retired. `flake.nix`'s header, `nix/README.md`'s fixture table and `docs/netlink/collection.md` name it either way; that update is part of the task that decides it.
- **`RTM_NEWNDUSEROPT`.** A custom subscriber and a decoder, daemon-side, if locality ever needs RA options; the exclusion above records where it would go.
- **A `pkgsCross.s390x` monitorfile check**, if the note-documented procedure proves its keep across further findings.

## See also

- [goip-parity-next.md](goip-parity-next.md), the closed plan this one follows, and its Tier C capture gate.
- [goip-parity-gaps.md](goip-parity-gaps.md) — the message-type assessment and the universe gate the event corpus inherits.
- [coverage-status.md](coverage-status.md), [parity at a glance](coverage-status.md#goip--ip-parity-at-a-glance) and "The event layer: decoded, never rendered", both of which this plan closes.
- [upstream/iproute2-bond-partner-state-width.md](upstream/iproute2-bond-partner-state-width.md) — the flagship of what Tier UB records: found by a capture, reproduced, fixed, tested, and nearly sent.
