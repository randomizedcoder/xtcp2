# goip ↔ `ip` parity events: implementation status

**Last updated:** 2026-11-13

**Summary:**
- The design and the task plan are in [goip-parity-events.md](goip-parity-events.md). This document tracks progress against them.
- The plan is written (DOC031). It follows the closed plans: [goip-parity-next.md](goip-parity-next.md) and those before it.
- Tier UB is closed (V376, DOC032, DOC033): the three owed notes are written and every claim in them re-verified against the pinned tags; `upstream/README.md` indexes the record and carries the findings procedure; `test-goip-docs` is a registered check whose first red run caught five pre-existing docs faults before it caught the three dead links it was built for. Writing the notes found one new iproute2 fault (a NULL deref in `print_proto_down`; [upstream/iproute2-print-json-bugs.md](upstream/iproute2-print-json-bugs.md)) — the record paying for itself on day one.
- EC0-T01 is done (V377): the five monitor helpers put `NLCAP_IP_BIN` first on their PATH, `xtcp2-nl-ipmon` has hermetic rows for the first time (it was missing from `xtcp2-nlcap`'s passthru), and `nlcap_batch.py`'s `--ip-bin` contract now names the helpers too.
- EC0-T02 is done (V378): `nlcapgen -family ev` writes `ev.rows.jsonl`; the row struct gained `floor` (the ev rows are the first to set one); the seeds are the rtnl-write corpus's four proven event rows, ported with the floors the corpus measured. The probe batch (EC1's first step) runs them and measures the workbench's own counts.
- The EC1 probe has run twice on fresh boots: all four rows OK both times, so the ported floors (14/20/11/36) hold, and the workbench measured 22/31/17/54 then 31/31/17/54 datagrams. Three rows are boot-deterministic; `ev/nsid-del` varies (22 vs 31), so its floor stays the ported 14 and the variance source is the first EC1 diagnosis, before the rest of the tier's rows are written from their own probes.
- The first tasks left, in order:
  1. the `ev/nsid-del` variance diagnosis (a third boot, decode the two shapes), then the EC1/EC2 rows from the measurement;
  2. [EP1](goip-parity-events.md#ep1-o10--the-monitor-replay-golden)'s O10 skeleton while the boots run.

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. There are no percentages.
- A task is `done` only when its gates have passed and the evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs (`ECn-Tmm`, `EPn`, `EGn`, `UB0-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [Tier EC](goip-parity-events.md#tier-ec-captures) | the event rows, both versions, verdiff | EC4-T01 | in progress | V377 |
| [Tier EP](goip-parity-events.md#tier-ep-replay-and-parsers) | O10 and the crafted monitor files | EP2 | not started | — |
| [Tier EG](goip-parity-events.md#tier-eg-goip-and-the-matrix) | the monitor section | EG2 | not started | — |
| [Tier UB](goip-parity-events.md#tier-ub-the-iproute2-findings-record) | the three owed notes, the index, the check, the procedure | UB0-T03 | done | V376, DOC032, DOC033 |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [EC0-T01](goip-parity-events.md#ec0-t01-the-monitor-helpers-take---ip) | the monitor helpers take `--ip` | done | V377 |
| [x] | [EC0-T02](goip-parity-events.md#ec0-t02-the-ev-family) | the `ev` family | done | V378 |
| [ ] | [EC1](goip-parity-events.md#ec1--the-four-base-families-at-731) | the four base families at 7.3.1 | not started | — |
| [ ] | [EC2](goip-parity-events.md#ec2--the-notification-families-with-no-capture) | the notification families with no capture | not started | — |
| [ ] | [EC3](goip-parity-events.md#ec3--both-iproute2-versions-and-verdiff) | both iproute2 versions, verdiff | not started | — |
| [ ] | [EC4-T01](goip-parity-events.md#ec4-t01) | recount; write Tier EP and EG tables | not started | — |
| [ ] | [EP1](goip-parity-events.md#ep1-o10--the-monitor-replay-golden) | O10, the monitor replay golden | not started | — |
| [ ] | [EP2](goip-parity-events.md#ep2-the-crafted-monitor-files-at-two-versions) | the crafted monitor files, `test-goip-monitorfile` | not started | — |
| [ ] | [EG1](goip-parity-events.md#tier-eg-goip-and-the-matrix) | the monitor section (outline until EC4) | not started | — |
| [ ] | [EG2](goip-parity-events.md#tier-eg-goip-and-the-matrix) | the offline section (outline until EC4) | not started | — |
| [x] | [UB0-T01](goip-parity-events.md#ub0-t01-the-three-owed-notes) | the three owed notes | done | DOC032 |
| [x] | [UB0-T02](goip-parity-events.md#ub0-t02-the-index-and-the-check) | the index, `test-goip-docs` | done | V376 |
| [x] | [UB0-T03](goip-parity-events.md#ub0-t03-the-findings-procedure) | the findings procedure | done | DOC033 |

## Verification ledger

| ID | Date | Task | Checks | Result | Commit |
|---|---|---|---|---|---|
| DOC031 | 2026-11-13 | — | the plan | [goip-parity-events.md](goip-parity-events.md) written: five verified facts, four tiers, gates inherited from the closed plans | this commit |
| V376 | 2026-11-13 | UB0-T02 | `python3 -m unittest discover -p test_goip_docs.py` (11 tests); `python3 nix/tests/goip_docs.py docs .` red, then green; `nix build .#checks.x86_64-linux.test-goip-docs`; `nix-fmt`, `statix`, `deadnix` on the diff | All pass. The check is registered in `nix/checks/default.nix` and runs the 11 unit tests plus the scan in the sandbox. The red runs, in order: `coverage-status.md` two stale anchors (`nine`→`ten`, `six`→`five`), `goip-parity-gaps-status.md` V202's link to the since-merged `render/link_xstats_test.go`, `link-state-tracking.md`'s two links into the since-folded `nix/netlink-capture/` tree, the bond note's shorthand `#decode-side` anchor — five pre-existing faults, each fixed in the same task — and then the three dead links this task existed for, one per red run as each note landed. Green: 35 documents, 12 artifacts listed, 4 notes linked | this commit |
| DOC032 | 2026-11-13 | UB0-T01 | the three notes, every iproute2 claim re-measured | [iproute2-rta-addattr-uninit-padding.md](upstream/iproute2-rta-addattr-uninit-padding.md): `rta_addattr_l` at `lib/libnetlink.c:1462-1483`, unfixed at `v7.3.0`, fixed by `c5b76cbb` (2026-11-09, this project's patch, in no tag). [upstream/iproute2-print-json-bugs.md](upstream/iproute2-print-json-bugs.md): `print_proto_down` at `ip/ipaddress.c:903-941`, three faults, unfixed at `v7.3.0` and HEAD; the corpus rows quoted (ec-004, pw-001, pw-004, pw-006, rn1-004). [upstream/iproute2-rt-names-table-load.md](upstream/iproute2-rt-names-table-load.md): the off-by-one at `lib/rt_names.c:118`, the built-in `"0"` at `:588`, the lazy load at `:623`, unfixed at HEAD; `protodown_reason_tab[32]` measured on the way (also off-by-one) | this commit |
| DOC033 | 2026-11-13 | UB0-T03 | the findings procedure | `upstream/README.md`: the index (one line per artifact: finding, versions, fixed-upstream, evidence, sent) and the "when a note is owed" procedure — both boots agree + `ip`'s bytes ⇒ a note in the same task, before goip changes | this commit |
| V377 | 2026-11-13 | EC0-T01 | `nix build .#checks.x86_64-linux.vm-lib-exp` (22 + 227 + 59 rows and the rest); `python3 -I test_nlcap_batch.py` (14); `nix-fmt`, `statix`, `deadnix`, `netlink-audit`, `lint-baseline` on the diff | All pass. The five helpers (`xtcp2-nl-ipmon`, `xtcp2-nl-brmon`, `xtcp2-nl-xfrmmon`, `xtcp2-nl-ioam`, `xtcp2-nl-mptcp`) put `NLCAP_IP_BIN` first on their PATH, `xtcp2-nlcap`'s way; a bad directory is a usage error before any monitor or socket. addr-topology-test.exp gains 10 rows (217→227): a logging stand-in `ip` proves the monitor, ipmon's canary join and xfrmmon's trigger all resolve from the directory; brmon resolves `bridge` from it when `NLCAP_BRIDGE` names nothing; ioam's and mptcp's checks run before any socket. The kernel-building `nlcap-ws` check was left to the pre-push run (its closure is the workbench VM; the comment-only `nlcap_batch.py` change is covered by its 14 unit tests) | this commit |
| V378 | 2026-11-13 | EC0-T02 | `go test -count=1 ./tools/nlcapgen/` (TestEvFamily, TestEvSeedsAreTheCorpusRows); `go vet`; `gofmt` check, `lint-baseline`, `netlink-audit`; the generated `ev.rows.jsonl` through the batch's own `load_rows` (4 rows clean); `go test ./pkg/xtcpnl/ -run TestWsCorpus` | All pass. `nlcapgen matrix -family ev` writes 4 seed rows — the rtnl-write corpus's nsid-del, anycast-del, prefix and monitor-all, ported with the floors the corpus measured (14, 20, 11, 36) and the same pre steps, triggers and until-EREs. The row struct gains `floor` (json `floor`, omitempty): the first family to set one; a read's dump always answers, so the generated families take the default 0 | this commit |

## Decision log

- **Decided: the event corpus is rows in the workbench, not a rebuilt `nlmon-capture` oneshot (2026-11-13, from the plan's own findings).** The oneshot's shape is the recorded reason the driven machinery exists ("a rebuild per attempt makes that a chore rather than a question"; no stop path), and every corpus since the addr plan has been rows. The oneshot's retirement is a follow-up decision, not this increment's.
- **Decided: addrlabel, neightbl and linkprop *events* are exclusions with reasons, measured in the kernel tree (2026-11-13).** `net/ipv6/addrlabel.c` has no notify; `net/core/neighbour.c` answers `RTM_SETNEIGHTBL` with an ACK only; `rtnl_linkprop` notifies an `RTM_NEWLINK` with `IFLA_PROP_LIST` (`net/core/rtnetlink.c:4503`). The rows that exist are the altname `RTM_NEWLINK` event and the write-request rows.
- **Decided: crafted `.nl` blobs live in `internal/goip/testdata/monitor/`, not `pkg/xtcpnl/testdata/` (2026-11-13).** The "positive fixtures are real captures" rule holds for pcaps; the blobs are inputs the kernel cannot produce, the bond note's carve-out, and are replayed through both tools' `monitor file` paths, which is the only place they mean anything.
- **Found while writing the notes: `print_proto_down` can be crashed, not just misprinted (2026-11-13, DOC032).** The reason gate tests `tb[2]` (the link's `IFLA_BROADCAST`) instead of the just-parsed `preason` table, so it is the only thing standing between the code and `rta_getattr_u8(NULL)` — `*(__u8 *)(NULL + 4)` — on a message whose reason nest lacks `VALUE` while the link carries `IFLA_BROADCAST`. No corpus row can hold it (every linkpd link carries IFLA_BROADCAST and the kernel always sends `VALUE`); it is recorded in [upstream/iproute2-print-json-bugs.md](upstream/iproute2-print-json-bugs.md) and is exactly what EP2's crafted rows exist to exercise.
- **Found while writing the notes: `protodown_reason_tab[32]` shares the off-by-one (2026-11-13, DOC032).** `rtnl_tab_initialize_dir("protodown_reasons.d", protodown_reason_tab, 32)` with the table declared `[PROTODOWN_REASON_NUM_BITS]` (32) — a `32 foo` line writes one past. Measured into [upstream/iproute2-rt-names-table-load.md](upstream/iproute2-rt-names-table-load.md)'s table of callers.
- **Decided: the padding fix is this project's own, so its note is a record, not a series to send (2026-11-13, DOC032).** `c5b76cbb` (2026-11-09) is authored here and sits in the iproute2 tree untagged; [upstream/iproute2-rta-addattr-uninit-padding.md](upstream/iproute2-rta-addattr-uninit-padding.md) records the sightings, the bytes and the O5 tolerance it justifies, and notes that the first release carrying it retires the bug.
- **The check's first red run caught five pre-existing docs faults, all fixed in the same task (2026-11-13, V376).** Two stale anchors in `coverage-status.md` (`nine`/`six` vs the headings' `ten`/`five`), V202's link to the since-merged `render/link_xstats_test.go`, `link-state-tracking.md`'s links into the since-folded `nix/netlink-capture/` tree, and the bond note's shorthand `#decode-side` anchor. Findings are fixed in the docs, never by weakening the checker — the same rule as lint-baseline.
- **Found: `xtcp2-nl-ipmon` was missing from `xtcp2-nlcap`'s passthru, so it had no hermetic rows at all (2026-11-13, V377).** The passthru comment named the helpers vm-lib-exp runs and left ipmon out. EC0-T01 adds it, and with it ipmon's first ten rows — before this, ipmon's only coverage was the capture runs themselves.
- **Found: a canary join that fails ends `xtcp2-nl-ipmon` at `ip`'s own exit, not after the ten tries (2026-11-13, V377).** `writeShellApplication` runs the helper under errexit, and the canary's `ip addr add` is unredirected, so a device that does not exist fails the helper on the first try with ip's own rc and message. That is honest guest behavior (a row bug, named loudly and fast); the boundary row pins it instead of the "never printed the canary" message the first draft expected.
- **Decided: `NLCAP_BRIDGE` still wins the monitor for `xtcp2-nl-brmon`; `NLCAP_IP_BIN` must hold `bridge` when it does not (2026-11-13, V377).** The stand-in seam keeps its precedence so the hermetic rows need no change; an iproute2 bin always holds both binaries, and the helper refuses a directory that holds neither.
- **Decided: the ev seeds are the rtnl-write corpus's four event rows, ported whole (2026-11-13, V378).** Their pre steps, triggers, until-EREs and floors (14, 20, 11, 36) are measurements the corpus already made, so `ev.rows.jsonl` is a probe file, not a guess: EC1's probe batch runs it twice and its own counts become the floors the corpus keeps. The plan's EC0-T02 rule — floors and EREs from measurement — is met by porting measurements, and the probe renews them.
- **Decided: the row struct gains `floor`, and the ev family is the first to set one (2026-11-13, V378).** The batch and `xtcp2-nlcap write` have carried a per-row floor since W0, but no family used it: a read's dump always answers, so 0 was harmless. An event row's pcap must hold at least the canary, so every ev row sets one and `TestEvFamily` holds them to ≥ 1.
- **Decided: the ledger renumbers to V376+ (2026-11-24).** The rebase onto origin/main (PRs #307-#309) revealed the minted V079/V080/V081 collide three ways in the merged tree already (gaps C2-T08, link-kinds KP1/KP2, genl-clones 1C-1R); the plans number their ledgers per-arc, but this plan takes the collision-free block above the tree's maximum, gaps' V375, so V376/V377/V378 and the events arc continues from there. DOC001-003 renumber to DOC031-033 the same way, above genl-clones' DOC030. The five notes the rebase brought are adopted into the findings index (they were the first red run of the merged tree: an unindexed artifact and DOC030's unlinked ledger row).
