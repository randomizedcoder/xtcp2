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
- **Findings** (every tier, and EP2's crafted blobs above all): a difference that is `ip`'s print path or request bytes — both 7.2.0 boots agreeing on it, or a 7.3.0-only line shape — obligates a note in [upstream/](upstream/) in the same task, before goip is changed to match, exactly as [upstream/README.md](upstream/README.md)'s "When a note is owed" says. A goip-side gap the same probe finds is not a note: it is the task's own work, and the ledger entry says which is which. EP1-T01's baseline measured zero of the first kind (all 66 rows agree), and EP2's planning probes measured three of the second (the `state UNKNOWN` word, the `congctl cubic p` padding, the nested-flagged `RTA_METRICS`) — recorded in V386 with the iproute2-side facts (the bond series' landing, the truncated attribute's `63` at both versions) already in the bond note.
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

- **`evnhb`** — resilient nexthop bucket replacement: `ip nexthop replace` on a `type resilient` group under `ip monitor nexthop`. The kernel notifies `RTM_NEWNEXTHOPBUCKET` with `NLM_F_REPLACE` (`net/ipv4/nexthop.c:1222-1231`). The parity corpus holds bucket *dump replies*; no notification. The EC2 probe traced the notify's only path: a group replace migrates idle buckets **silently** (`nh_res_table_migrate_buckets` re-points directly, and the replace's upkeep runs `notify_nl=false`), so the notified migration needs buckets that carried traffic and the unbalanced timer's forced deferral — 64 `/dev/udp` writes through a route on the group, then a weight-swap replace, and the migration lands one second later. The corner: a member's gateway replace notifies no bucket at all.
- **`evconf`** — `RTM_NEWNETCONF` on a forwarding flip (already in `monitor_all`'s trigger) and **`RTM_DELNETCONF` on device unregister** (`net/ipv4/devinet.c:2761`, `net/ipv6/addrconf.c:7411`): d0 with forwarding on, then `ip link del d0`. The plan's corner — unregister without forwarding — is falsified by the kernel source: `inetdev_init` registers the sysctl table for every device (`devinet.c:294`), so `inetdev_destroy` → `devinet_sysctl_unregister` → `inet_netconf_notify_devconf` fires on **every** unregister; the probe measured it, and the corner row records the notification, not its absence.
- **`evrule`** — `RTM_NEWRULE`/`RTM_DELRULE`: the rule selectors of `rule_add_selectors` under a monitor. `monitor_all` holds one add; no selector set, no delete pair, no v6.
- **`evstats`** — `RTM_NEWSTATS` beyond the one `l3_stats on`: the off/on pair, and `-s`-shaped updates if the probe shows them.
- **`evnsid`** — the `nsid_del_event` row holds the del; add the set and the assign-to-a-live-netns forms. The probe found the live-netns shape needs no add or del at all — the row's own netns, assigned an id inside the window — and that a second id for a netns that has one is the kernel's EEXIST ("Peer netns already has a nsid assigned"), a refusal worth its own negative row. The same probe corrected EC1's reading of the init probe: an unassigned netns is answered `RTM_NEWNSID` carrying `NETNSA_NSID -1` (`NETNSA_NSID_NOT_ASSIGNED`), not EINVAL — the EINVALs were V380's churn race.
- **`evprefix`** — the `prefix_event` row holds one RA; add the deprecated-lifetime corner and the v6-route-from-RA pair. Both need `xtcp2-nl-ra` lifetime knobs: an optional router lifetime (nonzero lets the receiver install the default route the RA's source is the gateway of) and preferred lifetime (0 is the deprecated prefix); the tool grew both, its 3-arg form unchanged for the seed row.

| description | input | expected |
|---|---|---|
| positive: a bucket replacement | `evnhb` | `RTM_NEWNEXTHOPBUCKET`, `NLM_F_REPLACE`, in the window |
| positive: netconf del | `evconf` | `RTM_DELNETCONF` with the device's ifindex |
| positive: rule add/del pair | `evrule` | both notifications, the del's `Deleted` prefix in `.out` |
| corner: unregister without forwarding | `evconf` | `RTM_DELNETCONF` anyway — the kernel notifies every unregister; the row records the measured behavior |
| corner: a second id for a netns | `evnsid` | `NLMSG_ERROR` EEXIST, the refusal iproute2 prints |
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
  - `TestNetlinkUniverse` needs no new names (every type in the families is already counted; the recount below was run with it green over the enlarged corpus — 15693 pcaps); the exclusion list gains nothing (addrlabel and neightbl events do not exist — recorded in [Out of scope](#out-of-scope), which is the universe gate's own "excluded with a reason" discipline applied to events).
  - Write Tier EP's and Tier EG's task tables into this plan from the rows, with each row's expected value.

| corpus | rows | rc 0 | notes |
|---|---|---|---|
| `7_3_1/ws/ev*` | 33 | 31 | 7.2.0; the two rc 1 are the refusals: `everr-esrch` (ESRCH) and `evnsid-eexist` (EEXIST) |
| `7_3_1/ws-ip730/ev*` | the same 33 ids | 31 | 7.3.0; verdiff clean (V382) |

The notification types both roots hold, over all 66 rows (each type's message count): `RTM_NEWLINK` 498, `RTM_DELLINK` 8, `RTM_NEWADDR` 124, `RTM_DELADDR` 27, `RTM_NEWROUTE` 67, `RTM_DELROUTE` 7, `RTM_NEWNEIGH` 24, `RTM_DELNEIGH` 9, `RTM_NEWRULE` 12, `RTM_DELRULE` 8, `RTM_NEWPREFIX` 8, `RTM_NEWMULTICAST` 87, `RTM_DELMULTICAST` 34, `RTM_NEWANYCAST` 4, `RTM_DELANYCAST` 2, `RTM_NEWNETCONF` 4, `RTM_DELNETCONF` 8, `RTM_NEWNSID` 86, `RTM_DELNSID` 4, `RTM_NEWSTATS` 6, `RTM_NEWNEXTHOP` 12, `RTM_NEWLINKPROP` 2, `RTM_NEWNEXTHOPBUCKET` 2 (both with `NLM_F_REPLACE`). `RTM_DELNEXTHOPBUCKET` is absent by design — the kernel never sends it (V381's dead-letter finding).

## Tier EP: replay and parsers

Each task gets a table test from the captures, a benchmark over the corpus picks and `FuzzParse*` seeds. Tier P's tables are written at EC4-T01; the two below are fixed by the increment's design, not by the rows.

The increment's order is baseline-first, the discipline the captures set: EP1-T01 measures the render gap before any goip line changes, and EP2's check is built red before it is made green — the `0x0100` partner state at the two iproute2 versions is the red case the bond note predicted.

### EP1 O10 — the monitor replay golden

- **The oracle.** Every event row's `.out` is `ip monitor`'s decoded output of the same run. A new corpus test (O10, after O3/O5/O9) replays the row's pcap through goip's monitor — in-process, `file FILE`'s walk, `ParseNetlinkPcap` for the records — and holds goip to that `.out`, text and errors. The `.out`'s canary blocks pair with their notifications, both ways, the A1-T02 rule.
- **What it buys.** Every event render goip prints becomes replay-grounded: 7.3.1 bytes, ip 7.2.0's words. A gap is named by the first word of ip's line goip lacks, as O9 names readbacks (`ws-readbacks.txt`); the goal is 0.

| description | input | expected |
|---|---|---|
| positive: a row per family | `ev/evlink-carrier`, `ev/evaddr-dad`, `ev/evroute-autoconf`, `ev/evneigh-v4`, `ev/evnhb-bucket`, `ev/evconf-del`, `ev/evrule-v4`, `ev/evstats-l3`, `ev/evnsid-live`, `evprefix-route`, `ev/anycast-del`, `ev/evseq-mixed` | goip's monitor output is each row's `.out`, canary blocks and all |
| positive: the refusal rows | `ev/everr-esrch`, `ev/evnsid-eexist` | the canary blocks, no render of the refused request, rc as recorded |
| corner: the canary block | every `ipmon` row | printed, paired, both ways |
| negative: a render goip lacks | a finding | `lacks "…"` in the golden |
| boundary: the empty monitor | a row whose trigger refuses | no lines, rc as recorded |

#### EP1-T01 the O10 harness, and the measured baseline

The corpus test `internal/goip/ws_monitor_test.go`, modeled on O9 (`ws_readbacks_test.go`): walk every `ev` row under both roots, parse the row's pcap (`ParseNetlinkPcap`), feed each message to goip's monitor (`obj_monitor.go`'s `accept`, the same path `monitor file` walks), and compare the rendered lines against the row's `.out`. The delivery filter is measured, not guessed: the pcap holds every socket's traffic in the namespace, and the monitor's socket received only its groups' broadcasts — the first run reports both sides, the lines goip lacks (renders to add) and the lines the walk would print that the monitor never received (messages to filter), each class named in the golden `testdata/ws-monitor.txt` (`-update-ws-monitor`, the O9 pattern). The canary blocks pair with their notifications, both ways — the A1-T02 rule the golden's per-row blocks hold.

Gates: the test green with the golden recording the measured baseline (its lacks list is the task's evidence, however long); `go test ./pkg/xtcpnl/` unfiltered; the nlcapgen suite; `lint-baseline`, `gofmt`, `netlink-audit`.

#### EP1-T02 close the lacks

Each `lacks` entry from the baseline is a render gap in goip's monitor, closed in `obj_monitor.go`/`render` until the golden is empty. The refusal rows' boundary is part of the close: no render of the refused request, rc as recorded. If a lack is a version difference (a print that changed between 7.2.0 and 7.3.0), the render goes behind the version knob, the P14 doctrine — but EC3's verdiff says there are none in the event families, so a version difference here is a finding first, a note in the same task.

Gates: the golden empty; O5/O9/O10 regenerated in the task's commit (`differs` and `sends` stay 0, no row moves away from `same`); the full suite.

#### EP1-T03 the corpus picks' benchmarks

The `FuzzParse*` seeds and the benchmark over the EP1 twelve (`evlink-carrier`, `evaddr-dad`, `evroute-autoconf`, `evneigh-v4`, `evnhb-bucket`, `evconf-del`, `evrule-v4`, `evstats-l3`, `evnsid-live`, `evprefix-route`, `anycast-del`, `evseq-mixed`), the tier's own opening rule. Gates: the fuzzers seeded, the benchmark recorded in the ledger.

### EP2 the crafted monitor files, at two versions

The blob class exists for inputs the kernel cannot produce, and every row's expected value is now measured, not argued: this plan's commit ran each blob shape through nixpkgs' `ip` (7.2.0), the pinned 7.3.0, and goip as it stands (V386). Three facts came out of the probes that reshape the tasks:

- **The bond series is upstream.** `0067cbc8` (the u16 read), `075f1b6f` (the `test_on_not` fix) and `4f49441d` (the testsuite test) were all applied 2026-10-19 and first released in **v7.3.0** — so the hionly blob genuinely prints differently at the two pins, which is what makes it the check's red case. The bond note's status block is corrected in this commit's docs pass.
- **The red case's 7.3.0 print is `256` with an empty list, not `<active>`.** `print_slave_oper_state` iterates the eight `LACP_STATE_*` names and `0x0100` sets none of them, so 7.3.0 prints `ad_partner_oper_port_state 256 ad_partner_oper_port_state_str <>` — an earlier draft of this plan said `<active>`, which is the big-endian u8 read, not the u16 one. The hibyte blob (`0x3f01`) is the richer 7.3.0 corner: `319` and a **trailing comma** before the `>` (the `_PF` loop prints `state ? "," : "%s"` against bits still set, and the uncovered `0x100` bit keeps `state` non-zero after the last name) — goip's `addOperState` already prints that comma, its divergence note documented at `internal/goip/render/link_bond.go`.
- **The truncated row is a match, not a refusal.** The bond note's goip table says a 1-byte partner payload is a decode error, not `63`; measured, `ip` prints `63 <full list>` at **both** versions — 7.2.0 reads the octet with `rta_getattr_u8`, 7.3.0's `rta_getattr_u16` reads the payload plus the alignment padding, which is zero — and goip's `setPartnerOperState` one-byte arm (`pkg/xtcpnl/xtcpnl_bond.go`) matches both. The note's row is superseded by the measurement; the committed blob expects `63`.

Two goip render gaps the probes found, both reachable only by crafted input (every corpus message carries `IFLA_OPERSTATE`, and no kernel writer sends what these blobs carry):

- goip prints `state UNKNOWN` when `IFLA_OPERSTATE` is absent; `ip` prints nothing (the attribute is guarded at `ip/ipaddress.c:859-860` and `1040-1041`). Every crafted link blob shows the word today.
- goip prints `congctl cubic p` for a CC_ALG payload with garbage past the first NUL — both in the V038 shape (rta_len 10, the garbage in the padding, which goip's subattr walk reads past `rta_len`) and in the over-long shape (rta_len 12, `cubic\0p\0` as 8 data bytes, where `bytes.TrimRight` at `pkg/xtcpnl/xtcpnl_rtnexthop.go` keeps the interior NUL and the `p`). `ip`'s `rta_getattr_str` is a C string: it stops at the first NUL, and both versions print `congctl cubic` for every shape.

#### EP2-T01 `nlcapgen monfile`

The small subcommand, two inputs, one output: `-spec FILE` writes the `.nl` stream a crafted message list names (JSONL, one message per line — type, flags, seq, pid, body as hex, and a `why` the row must carry), and `-pcap FILE` writes what a captured pcap holds, the monitor's notifications only by default (`-all` for the raw stream) — the delivery filter, so a plain `ip monitor file` over a converted row prints the row's `.out` and nothing else. The filter moves to `pkg/nlparity` (exported, `internal/goip/source_monitor.go`'s `replayNotifications` delegating to it), one home for the replay and the converter. The format facts are pinned at the source: `rtnl_from_file` (`lib/libnetlink.c:1315-1368`) takes no magic — each nlmsghdr, then its body to the 4-byte boundary, `generate_nlmsg.c`'s shape — refuses a header cut short with `rtnl-from_file: truncated message`, a length under 16 or over the 16384 buffer with `!!!malformed message: len=%d @%lu` (the `@` is `ftell` after the header's sixteen bytes, which is where goip's error puts it), and a body cut short with the truncated message again.

| description | input | expected |
|---|---|---|
| positive: a crafted message list | the spec, three messages | `.nl` bytes golden; `monitorReadFile` hands back the same three |
| positive: a captured pcap | the `evnhb-bucket` row's pcap | only the notifications the monitor received; the guest replay (below) prints the `.out` |
| negative: a header cut short | 10 bytes | `ErrMonitorFile` wrapping `rtnl-from_file: truncated message`, as `fromFile` documents |
| negative: a body cut short | a length past the file's end | the same truncated message |
| boundary: a length under the header's | `nlmsg_len` 12 | `!!!malformed message: len=12 @16` |
| boundary: a length over the buffer | `nlmsg_len` 16385 | `!!!malformed message: len=16385 @16` |
| corner: a nested-flagged RTA_METRICS | a metrics attr with `NLA_F_NESTED` | the spec does not set it: `parse_rtattr` does not mask the bit, so `ip` drops the attr whole (`iproute2`, `ip/iproute.c:982-984` — `RTA_PAYLOAD` walk); goip must too, a T02 row |

Gates: the unit tables in `tools/nlcapgen` (spec → bytes, golden; pcap → bytes with the filter, golden); the round-trip and error table in `internal/goip` (the committed `.nl` bytes through `monitorReadFile`, the same messages back, and the four refusals above); and the guest probe — the `evnhb-bucket` row's pcap converted, then `ip monitor file` run **in the row's live namespace, before its teardown**, so the lazy gets resolve the row's own names; its lines are the row's `.out`. (The file path runs no `ll_init_map`; `ll_index_to_name` resolves lazily — `ll_link_get` on a miss, then `if_indextoname`, then `if%u` — so a replay outside the namespace prints `if7`-style names, a measured difference to record, not a gate to fudge.)

Done (V389), with three measurements the plan text carried wrong or unwritten:

- **The `.out`-matching invocation is plain `ip monitor file`, not `-d`.** The row's monitor ran without `-d` (the `xtcp2-nl-ipmon` helper builds `ip <args>`), and measured, `-d` changes the replay: the `RTM_NEWNEXTHOP` line gains `scope global proto unspec` at both versions. The byte-identical replay is the plain one, at both 7.2.0 and the pinned 7.3.0 (222 bytes for 222, the whole `.out`).
- **The filter's three of sixteen are exactly the `.out`'s three events**: an `RTM_NEWMULTICAST` (type 56 — the `xtcp2-nl-ipmon` canary's own join, which is why the row's `.out` starts with an mcast line), the `RTM_NEWNEXTHOP` of `nexthop_notify` carrying the request's own `NLM_F_REQUEST|ACK|REPLACE|CREATE` (0x505) with the requester's seq and port id — the one notification shape a naive request filter would drop, held by the pid test — and the `RTM_NEWNEXTHOPBUCKET` with `NLM_F_REPLACE` (type 116).
- **The if `%u` fallback measured, not just predicted**: the same `.nl` in the guest's root namespace prints `3: if3` where the row's namespace prints `3: c0` (lo 1, nlmon0 2, c0 3 — the `ns-add` recipe reproduced verbatim), and nothing else differs: the two nexthop lines carry no device names to resolve. The T03 check's sandbox goldens are the `ifN`-style names.

#### EP2-T02 the blobs

The specs and their `.nl` are committed under `internal/goip/testdata/monitor/`, regenerated together: `nlcapgen monfile -spec` must write the committed `.nl` byte for byte, so the binary can never drift from its reviewable source, and each spec line carries the `why` the check asserts is non-empty — why the kernel cannot send it. Crafted blobs use an ifindex no sandbox carries (7), so every `dev` prints `if7` and the goldens are host-independent. The measured table:

| description | input | ip 7.2.0 | ip 7.3.0 | goip today |
|---|---|---|---|---|
| positive: negotiated fast partner | `0x003f` | `63 <active,short_timeout,aggregating,in_sync,collecting,distributing>` | same | same |
| positive: slow partner | `0x003d` | `61 <active,aggregating,in_sync,collecting,distributing>` | same | same |
| boundary: zero | `0x0000` | `0 <>` | same | same |
| boundary: all eight bits | `0x00ff` | `255 <…defaulted,expired>` (all eight names) | same | same |
| corner: high byte with low | `0x3f01` | `63 <full list>` (the u8 fault) | `319 <full list,>` (trailing comma) | 7.3.0's shape, no knob |
| corner: high byte only, the red case | `0x0100` | `0 <>` | `256 <>` | 7.3.0's shape, no knob |
| negative: the attribute absent | no partner attr | no partner line | same | same |
| negative: truncated, one byte | `0x3f` as 1 byte | `63 <full list>` | `63 <full list>` (the pad read) | same — the note's decode-error row superseded |
| boundary: the actor stays u8 | `0x3f` as the actor attr | `63`, both versions — an over-eager u16 read is the regression this row catches | same | same |
| positive: P14 reason 0 and 3 | `0x9` | `protodown on protodown_reason <0,3>` | same | same |
| boundary: P14 reason 8 alone | `0x100` | `protodown on`, no reasons (the u8 read) | same | same |
| corner: P14 reasons 0 and 31 | `0x80000001` | `protodown on protodown_reason <0>` | same | same |
| positive: the V038 padding | rta_len 10, pad `p\0` | `congctl cubic` | same | `congctl cubic p` ← the fault |
| corner: the over-long payload | `cubic\0p\0` as 8 data bytes | `congctl cubic` | same | `congctl cubic p` ← the fault |
| corner: a nested-flagged RTA_METRICS | the metrics attr with `NLA_F_NESTED` | no metrics printed | same | `congctl cubic …` ← diverges |
| negative: not a netlink file | a text file | `rtnl-from_file: truncated message`, rc 1 | same | the same complaint, goip-prefixed, rc 1 |

The work the table names: close the two render gaps (the `state UNKNOWN` word, the CC_ALG C-string semantics — stop at the first NUL, never read past `rta_len`); and add the version knob `render.RenderBondPartnerU8` plus the `GOIP_IP730` env seam that sets the whole release-knob set (`Keywords730`, `LLAddrStrict`, `RenderBondLacpStrict`, `RenderNhDstPort`, `RenderOvpnMode`, the new one — `useWsO5Release`'s list) from outside the binary, the P14 doctrine's external control. The u8 read is the **low** byte, as iproute2 on a little-endian host reads it (`rta_getattr_u8` is `*(__u8 *)RTA_DATA`, `include/libnetlink.h:240-243`); big-endian hosts read the high byte and are out of scope — the corpus VMs and the check are x86_64. `link_bond.go`'s "no knob is needed for the older releases" comment is superseded: it reasoned from kernel-producible values, and the crafted blob is exactly the input it was wrong about. The knob defaults off (7.3.0, the parity target).

Done (V390), the blobs committed as fifteen spec/`.nl` pairs under `internal/goip/testdata/monitor/`, each with the ip goldens it was measured against (`<name>.ip720.out`, `<name>.ip730.out`), goip byte-identical to both at the matching `GOIP_IP730` knob — and four measurements the plan text carried wrong or unwritten:

- **The V038-padding row does not fault.** The walk slices `rta_len` exactly (`WalkRTAttrs`), so `cubic\0` plus garbage in the alignment pad already printed `cubic`; only the over-long shape (`cubic\0p\0` as eight data bytes) printed the interior NUL and the `p` — `bytes.TrimRight` stripped the trailing NULs alone. The plan's "← the fault" on the padding row is corrected; the committed blob keeps it, as the row that proves the walk reads no padding.
- **The P14 blobs must carry `IFLA_BROADCAST`.** `print_proto_down`'s reason gate tests `tb[IFLA_PROTO_DOWN_REASON_VALUE]` — the link's table at index 2, `IFLA_BROADCAST` — not the just-parsed nest, so a blob without it prints ` protodown on ` and no reasons at all. The first cut measured exactly that (both ips agreeing); the committed blobs carry the broadcast like every real link message, and the gate is what the P14 note records.
- **The route walk is raw, with one carve-out.** `parse_rtattr` never masks `NLA_F_NESTED` (`lib/libnetlink.c:1541-1557`), so a nested-flagged `RTA_METRICS` matches nothing and `ip` drops it whole — `WalkRTAttrsRaw` now walks `ParseNewRoute` and `ParseRouteMetrics` that way. The carve-out: iproute2's own REQUEST builder writes `RTA_ENCAP` flagged (`rta_nest`, `lib/libnetlink.c:1519-1527` — `nest->rta_type |= NLA_F_NESTED`), where the kernel's replies never do (`lwtunnel_fill_encap` is `nla_nest_start_noflag`), and the corpus's captured request echoes carry the flagged shape, which O3 reads; the ENCAP case alone accepts it.
- **`TestMain` runs the package's tests at 7.2.0**, and the blob tests set the knob set through the seam — so they snapshot and restore rather than "reset to defaults", which would clobber the ambient release every test after them renders at.

The committed set, the blob file index the docs check names (EP2-T03's gate: every file under `internal/goip/testdata/monitor/`, no orphans):

- `bond-actor-u8`: `bond-actor-u8.spec` `bond-actor-u8.nl` `bond-actor-u8.ip720.out` `bond-actor-u8.ip730.out`
- `bond-partner-absent`: `bond-partner-absent.spec` `bond-partner-absent.nl` `bond-partner-absent.ip720.out` `bond-partner-absent.ip730.out`
- `bond-partner-all`: `bond-partner-all.spec` `bond-partner-all.nl` `bond-partner-all.ip720.out` `bond-partner-all.ip730.out`
- `bond-partner-fast`: `bond-partner-fast.spec` `bond-partner-fast.nl` `bond-partner-fast.ip720.out` `bond-partner-fast.ip730.out`
- `bond-partner-hibyte`: `bond-partner-hibyte.spec` `bond-partner-hibyte.nl` `bond-partner-hibyte.ip720.out` `bond-partner-hibyte.ip730.out`
- `bond-partner-hionly`: `bond-partner-hionly.spec` `bond-partner-hionly.nl` `bond-partner-hionly.ip720.out` `bond-partner-hionly.ip730.out`
- `bond-partner-slow`: `bond-partner-slow.spec` `bond-partner-slow.nl` `bond-partner-slow.ip720.out` `bond-partner-slow.ip730.out`
- `bond-partner-trunc1`: `bond-partner-trunc1.spec` `bond-partner-trunc1.nl` `bond-partner-trunc1.ip720.out` `bond-partner-trunc1.ip730.out`
- `bond-partner-zero`: `bond-partner-zero.spec` `bond-partner-zero.nl` `bond-partner-zero.ip720.out` `bond-partner-zero.ip730.out`
- `linkpd-0009`: `linkpd-0009.spec` `linkpd-0009.nl` `linkpd-0009.ip720.out` `linkpd-0009.ip730.out`
- `linkpd-0100`: `linkpd-0100.spec` `linkpd-0100.nl` `linkpd-0100.ip720.out` `linkpd-0100.ip730.out`
- `linkpd-80000001`: `linkpd-80000001.spec` `linkpd-80000001.nl` `linkpd-80000001.ip720.out` `linkpd-80000001.ip730.out`
- `route-metrics-nested`: `route-metrics-nested.spec` `route-metrics-nested.nl` `route-metrics-nested.ip720.out` `route-metrics-nested.ip730.out`
- `route-metrics-overlong`: `route-metrics-overlong.spec` `route-metrics-overlong.nl` `route-metrics-overlong.ip720.out` `route-metrics-overlong.ip730.out`
- `route-metrics-v038pad`: `route-metrics-v038pad.spec` `route-metrics-v038pad.nl` `route-metrics-v038pad.ip720.out` `route-metrics-v038pad.ip730.out`

Gates: the spec-to-`.nl` regeneration golden (`TestMonfileCommittedSpecs`); goip's renders matching the ip column per knob, both knobs (`TestMonitorBlobs`, byte for byte against the committed goldens); the seam's own table (`TestReleaseEnv`, both ways and the refusals); the O10 golden regenerated — **66 rows, 0 differ stands** (every corpus message carries `IFLA_OPERSTATE` and no kernel partner state, so the two render fixes and the knob move no corpus row); the full suites; the fuzzers still green (the CC_ALG fix changes a parser — `FuzzParseNewRoute` gained the three crafted metrics bodies as seeds and ran clean 8 s, `FuzzParseNewLink` likewise).

#### EP2-T03 `test-goip-monitorfile`

The nix check on the `test-linkmonitor-rdma-runtime` pattern (`nix/checks/goip-monitorfile.nix`, registered in `nix/checks/default.nix`): it builds nixpkgs' iproute2 (7.2.0) and the pinned 7.3.0 (`nix/iproute2-7-3-0.nix`, the parity pin), regenerates every committed `.nl` from its spec with the real `nlcapgen` and refuses a byte of drift, then replays each blob through `ip -d monitor file` at both versions and `goip -d monitor file` at `GOIP_IP730=0` and `=1`, and compares stdout and rc against committed goldens (stderr only in the refusal rows — goip prefixes its complaint; stdout is the parity claim). This is the check that would have caught `0067cbc8` and P14 before the notes existed. Measured for the design: AF_NETLINK works in the build sandbox (a `runCommand` probe ran `ip -d link show` in it), so goip's eager `linkMap` dump (`obj_monitor.go`'s file path) works there too. The sandbox's link set, re-measured at this task (the plan once claimed the sandbox carries the host's indices; it does not): the sandbox's own network namespace holds `lo`, `tunl0` and `ip6tnl0` — no 7th link — which is the no-sandbox-index rule the blobs above rely on, and the check asserts it first, so a sandbox that ever names ifindex 7 is a named red, not a wall of golden mismatches.

The red-run mechanics, corrected for the order the work landed (V390 landed T02's fixes before this check exists, so the check cannot go red at HEAD): the red run is produced in a `git worktree` at `d30a4886c` (the tree before the fixes) carrying the blob files and this check — the flake reads tracked files, so the worktree adds them to its own index — and its stderr names the three gaps the fixes closed (the hionly row `256 <>` where the 7.2.0 golden says `0 <>` at `GOIP_IP730=0` — the pre-fix goip always read the u16 — the `congctl cubic p` rows, the `state UNKNOWN` word); the green run is at HEAD. The task's commit records both runs.

Gates: the check green at HEAD; `nix flake check`'s list grows the attribute; the docs check gains the blob files as artifacts — every file under `internal/goip/testdata/monitor/` named from a `docs/netlink/` document (the plan's tables and the upstream notes' evidence lines name them), the same no-orphans rule the upstream index carries, so a blob cannot rot unexercised; the O5/O9/O10 goldens regenerated in the same commit, `differs` and `sends` at 0.

Done (V392). The check is `nix/checks/goip-monitorfile.nix`, registered — its green run at HEAD: **15 specs regenerated, 60 replays byte for byte, 4 refusals**. The red run, produced in a worktree at `d30a4886c` carrying the blob files and the check: **30 RED lines, every blob at both goip knobs, the two `ip` sides green throughout** — the stderr naming each row with got/want sizes, the three gaps in them: `bond-partner-hibyte/ip720-by-goip` (418 bytes where the golden holds 402 — the pre-fix goip's `256 <>` where the 7.2.0 golden says `0 <>`, the state word besides), the `route-metrics-overlong/ip720-by-goip` rows (86 where the golden holds 84 — the `congctl cubic p` the C-string fix closed), and the `state UNKNOWN` word (14 bytes) in every bond and linkpd row — the presence guard. The refusal row, measured into the check: a file that is not netlink exits 1 with empty stdout on all three tools (both goip knobs — the monitor file walk is the parser's, not the release's), `ip`'s complaint `!!!malformed message: len=… @16`, goip's own `goip: monitor file: !!!malformed message: len=… @16` — non-empty, not equal, the prefix the difference. The no-sandbox-index rule is asserted before the first replay. The docs check names all 60 blob files (the plan's blob file index above, and the upstream notes' evidence lines). The O5/O9/O10 goldens regenerate with no drift.

## Tier EG: goip and the matrix

Planned (V391), measured before written. The grounding probe: the workbench guest, the `evlink-carrier` row's own recipe, three sides — `ip monitor link maddress`, `goip monitor link maddress` at `GOIP_IP730=1` (the parity release), `ip` again — each in a fresh namespace, the row's `pre`, the 224.0.0.250 canary join, the `r0p down; r0p up` trigger, a stop. **goip's live monitor printed `ip`'s lines** — the only differences across the sides were the addresses derived from each namespace's random veth MAC. The control noise is three classes, all measured:

1. **The canary retry's pair.** When a monitor has not printed the canary within its window, the helper deletes and rejoins; the pair of blocks (the `Deleted … mcast 224.0.0.250` block, the join block) appeared on the `ip` side in one run and on goip's in another — whichever side the race hits. The A1-T02 rule (canary blocks pair with their notifications) already carries it, and the row kind keeps the helper's own retry code, so the pair is expected on any side.
2. **The random MAC and its derived addresses.** The solicited-node mcast line (`ff02::1:ff…`) derives from the link's MAC; the comparator's existing MAC rule covers both, the same rule the corpus rows normalize with.
3. **The carrier transient.** The trigger's `down; up` settles asynchronously — the r0p line after the up prints `state LOWERLAYERDOWN` with `NO-CARRIER` when the carrier had not returned, `state UP` when it had; one run measured `ip_a` catching the first shape and `ip_b` the second. That is the D_control for monitor rows: a real control disagreement the comparator subtracts, and the goip row's shape is accepted while it lies inside the controls' own disagreement.

The refusal row's expect is the measured `everr-esrch` record — `rc 1, errno ESRCH, refused_by kernel` — the helper's own discipline (`rc=1` when the trigger fails), not the trigger's own rc; the table's "goip exits the trigger's rc" is corrected to the helper's rc below. What already exists: `goip` ships to the parity guest via `xtcp2AllPackage` (every flavor gets it), `xtcp2-nl-ipmon` is on the driven guests' PATH, the parity guest's `ip` is the 7.3.0 pin (hiPrio, `mkVm.nix`), and the save-blobs machinery ships store paths to the guest — the restore rows' own device, which EG2's blob inputs ride. V382 covers the version: the event print paths are unchanged between 7.2.0 and 7.3.0, so the rows' equality at the parity pin is the corpus's equality too.

- **EG1 — the monitor section.** A new row kind in the parity harness: the row starts `ip monitor <objects>` and `goip monitor <objects>` in the row's namespace, runs the trigger, stops both on the `until` EREs, and the comparator diffs the two monitors' stdout with the D_control discipline (an `ip`-only control run around the goip one, as `ip_a → goip → ip_b` does for dumps). The hygiene rule inverts for these rows only: a notification is the point, not pollution — the monitor section runs with the harness's hygiene check off, its rows carry no pcap and no floor (the until-EREs matched are the row's own floor). Timestamps are excluded (`-t` is never used in a compared row; the `monitor_all_timestamp` row stays a capture, not a comparison).

| description | input | expected |
|---|---|---|
| positive: one row per family, both monitors | the EP1 twelve (`evlink-carrier`, `evaddr-dad`, `evroute-autoconf`, `evneigh-v4`, `evnhb-bucket`, `evconf-del`, `evrule-v4`, `evstats-l3`, `evnsid-live`, `evprefix-route`, `anycast-del`, `evseq-mixed`) | goip's stdout equals ip's, canary blocks and all, on both boots' `ip` controls |
| positive: the refusals | `everr-esrch`, `evnsid-eexist` | the canary blocks, no render of the refused request, and the helper's own rc (1, V391's measured record) on both tools' sides |
| corner: the @peer print | `evlink-carrier`, `evlink-admin` | goip prints `r0@r0p: ` (the @peer notation, V380) |
| corner: the carrier transient | `evlink-carrier` | whichever carrier-settle shape the window caught, as the D_control run around it did |
| negative: a render goip prints differently | a finding | the line, both tools, the version knob |

- **EG2 — the offline section.** The EP2 blobs join the matrix as rows: `ip monitor file` against `goip monitor file` on the same blob, so the crafted inputs are gated, not just checked.

| description | input | expected |
|---|---|---|
| positive: the bond partner states | `0x3f`, `0x3d`, 0, `0xff`, `0x3f01`, `0x0100`, absent, truncated, the actor-stays-u8 pair | each version's print, as EP2's goldens |
| positive: P14's reason u32 | `0x9`, `0x100`, `0x80000001` | the 7.2.0 knob's behavior, both tools |
| positive: the metrics padding | the `cubic\0p\0` route | the padded byte, both tools (UB0-T01) |
| corner: a captured pcap, as a file | the `evnhb-bucket` pcap, via `nlcapgen monfile` | the same lines as the `.out` |
| negative: not a netlink file | a text file | both tools' refusal, rc as each exits |

#### EG1-T01 the row kind, and the probe's row

The harness grows the row kind: the `Command` table (`internal/goipparity/commands.go`) gains the monitor fields — the trigger, the until-EREs, the canary device — and `goip-parity commands` prints them, the guest row format growing past its fixed ten fields (the driver and the table stay one source: `TestCommandTable`'s pair rule). The driver (`goip-parity.exp`) grows the monitor flow: per side a fresh namespace (the Write discipline — the trigger changes state), the row's Setup, then `xtcp2-nl-ipmon <canary> <objects> <trigger> <until>…` with the tool switched for the goip side — the tool by env, one code path, so the canary loop, the trigger, the until-loop and the stop cannot fork between the tools. The comparator grows the monitor rows: stdout compared, D_control subtracted, the three noise classes handled as measured above — the MAC normalized, the canary pairs kept, the transient bounded by the controls' own disagreement.

Gates: the `evlink-carrier` row green through the harness (the three stdouts, the report naming the section); `TestCommandTable` and the harness's own tests over the new fields; the workbench hermetic tests; the full suites; the docs.

Done (V393–V395), the row kind in three halves and the section's gate green through the harness. V393: the `Command` table's monitor fields — Monitor, Trigger, Until, Canary — the row format past its fixed ten fields, the two probe rows in the table. V394: the driver's monitor flow — a fresh namespace per side, the Setup through `setup_step`, the ipmon with the tools by env — and `xtcp2-nlcap mon`, the row's own capture mode (`.out`/`.err`/`.rc` landing together, no pcap, no floor). V395: the comparator's monitor branch — the three rcs required to agree whatever the value (the refusal rows' rc 1 is not a finding; the helper's own complaint in any `.err` is), D_control subtracted, the canary collapse. The gate ran three times, every run `GOIP_PARITY_OVERALL_PASS` + `GOIP_PARITY_DRIVER_PASS`: the first both rows WARN — the comparator's own keep-first bug, the readiness join dropped for its Deleted twin — the second both rows PASS with `CONTROL_NOISY 4` (the carrier transient, the accepted D_control), the keep-last fix's rerun clean throughout (both rows PASS, `HYGIENE_PASS`, `CONTROL_CLEAN`, `UNGATED_CLEAN`, the rcs 0/0/0 per row). Four measurements the plan text carried wrong or unwritten: the MAC noise class is handled by the Setup's pinned veth MACs, the corpus's own device rule — the addresses comparable, not normalized away; the canary collapse keeps the LAST canary block, not the pairs — the helper joins, polls ~1s, deletes and rejoins on timeout, so the join its loop broke on is always the last, the shapes the race prints being the join alone and the Deleted+join pair; the tool switch is monitor-only (`NLCAP_MON_BIN`), goip's write verbs being dry runs that would fail their own canary — the plan's "the tool by env" wording corrected to the measurement; and the rows' Setup is ip argv steps, not the corpus driver's shell snippet (V391's own correction, carried). The first landing attempt's misspelled directory is the landing correction, on the record in the ledger (V395). The two rows gate on their first two clean live runs (V396), the ungated surface `addr show dynamic` alone again.

#### EG1-T02 the rows

The EG1 table's rows — the EP1 twelve, the two refusals, the @peer and carrier corners — each the `ipmon` row's own shape at the parity guest's release, goip at `GOIP_IP730=1`. The refusal rows' rc is the helper's (measured: `rc 1, errno ESRCH, refused_by kernel`), the stdout is the parity claim.

Gates: every row green or its finding recorded (the findings rule: an `ip` print-path difference obligates a `upstream/` note in the same task); the harness's report; O5/O9/O10 regenerated, `differs` and `sends` at 0; the full suites; the checks.

#### EG2-T01 the offline rows

The monitor-file row kind: an empty namespace per side (ifindex 7 misses — the `if7` fallback, the no-sandbox-index rule), `ip -d monitor file <blob>` against `goip -d monitor file <blob>` at `GOIP_IP730=1`, the blobs shipped to the guest by the save-blobs machinery. The `=0` rows hold goip to the committed `.ip720.out` goldens — the measurement of record — because the parity guest carries no 7.2.0 `ip` (T03's check is what gates that knob against the real 7.2.0 binary). The EG2 table's rows, as written at EC4-T01, plus the pcap corner's input: the `evnhb-bucket` capture converted by `nlcapgen monfile` and committed beside the blobs — a real capture's bytes, the fixture rule's own class.

Gates: every row green or its finding recorded; the committed `.nl` regenerated by the spec golden; O5/O9/O10 regenerated; the full suites; the checks; the docs check naming the pcap corner's committed files.

#### EG2-T02 the tier closes

The gates: O5/O9/O10 regenerated, `differs` and `sends` at 0, no row moved away from `same`; the full suites; the checks; the docs. The two decisions the plan's follow-ups leave to EG's close, answered in the task's own ledger row: the kernel spread for events (whether any decoder branch depends on a kernel-conditional attribute first — if none does, the spread is deferred with the reason recorded) and the oneshot's retirement (whether `nlmon-capture` stays as the one baked-in, zero-driver capture; if it goes, `flake.nix`'s header, `nix/README.md`'s fixture table and `docs/netlink/collection.md` name the retirement in the same task).

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
