# goip ↔ `ip` live forms: implementation status

**Last updated:** 2026-11-03

**Summary:**
- The design and the task plan are in [goip-live-forms.md](goip-live-forms.md). This document tracks progress against them.
- The plan is written (DOC001). It follows [goip-write-keywords.md](goip-write-keywords.md), whose GW1 to GW5 are done ([its status](goip-write-keywords-status.md), DOC006). The 22 forms GW5 left with no live row get a `kwlive` workbench family of 27 rows with an `ip`-only setup, then goip-parity rows and a VM gate.

- LF1-T01 is done: `nlcapgen matrix -family kwlive` writes 36 rows, the 27 forms (23 rc 0, 4 rc 2) and 9 duplicate adds, to `nix/microvms/workbench/kwlive.rows.jsonl` (V001–V003).
- **LF1 is closed.**

- LF2-T01 is done: two fresh-boot batches give 36 of 36 rows `OK` each, with every rc as the generator wrote it and the same rc and message types row for row; the first batch's captures are in `ws/kw*/`, and `TestWsCorpusRows` passes over them (V004–V006).
- LF2-T02 is done: `TestWsKwliveCorpus` checks each of the 36 rows' one request against its axes (`kwliveProblem`), and every form sends what the forms table says. `TestKwliveProblem`'s 34 hand-built rows hold each check, and its negative rows fail (V007–V009).
- LF2-T03 is done: O5 reads the five `kwlive` spaces, and all 36 rows are `same`. Four `kwnh` rows first differed: `ip` sends stack bytes in a nexthop's MPLS encap padding, which O5 now zeroes before comparing, as it does `RTA_METRICS`' (V010–V012).
- **LF2 is closed.**

- LF3-T01 is done: 23 `gw_kw*` goip-parity write rows, one per form and ip rc, picked by the GW5 tables from the `kwlive` rows; every form GW5 left with no row has one at rc 0, and `tos`, `dsfield`, `pref`, `index` and single-nexthop `hw_stats off` one at rc 2 as well. They stay ungated until LF3-T02 (V013–V015).
- LF3-T02 is done. The first `--section route,link,nexthop,rule` run passed 926 of 927 rows; `gw_kwnh_ec_003` diverged on the three pad bytes after `MPLS_IPTUNNEL_TTL` that `ip` takes from its stack (V016). nlparity now compares an `NLA_F_NESTED` attribute past its inner padding, as it already did a top-level attribute (V017, V018). On that, the section run passes 927 of 927 and the full run, with the 23 rows gated, 1304 of 1304 (V019, V020); the rows are in `gated_commands` (V021).
- **LF3 is closed, and with it the plan.**

**Next action:** none in this plan. The next push needs the whole-repo `test-go-race` check and an explicit OK.

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- States are `not started`, `in progress`, `blocked`, `done` or `deferred`. There are no percentages.
- A task is `done` only when its "Done when" has passed and the evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs (`LFn-Tmm`) are never renumbered.

## Phase states

| Phase | Work | Closed by | State | Evidence |
|---|---|---|---|---|
| [LF1](goip-live-forms.md#lf1--the-rows) | the rows | LF1-T01 | done | V001–V003 |
| [LF2](goip-live-forms.md#lf2--the-captures-and-the-oracles) | captures, O3, O5 | LF2-T03 | done | V004–V012 |
| [LF3](goip-live-forms.md#lf3--the-matrix-rows-and-the-vm-gate) | matrix rows, VM gate | LF3-T02 | done | V013–V021 |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | [LF1-T01](goip-live-forms.md#lf1-t01-the-kwlive-family) | the `kwlive` family | done | V001–V003 |
| [x] | [LF2-T01](goip-live-forms.md#lf2-t01-capture) | capture, twice | done | V004–V006 |
| [x] | [LF2-T02](goip-live-forms.md#lf2-t02-o3-for-kwlive) | O3 | done | V007–V009 |
| [x] | [LF2-T03](goip-live-forms.md#lf2-t03-o5-over-kwlive) | O5 | done | V010–V012 |
| [x] | [LF3-T01](goip-live-forms.md#lf3-t01-the-rows) | matrix rows | done | V013–V015 |
| [x] | [LF3-T02](goip-live-forms.md#lf3-t02-the-vm-gate) | the VM gate | done | V016–V021 |

## Verification ledger

| ID | Date | Task | Check | Result | Commit |
|---|---|---|---|---|---|
| DOC001 | 2026-11-03 | plan | The plan and this status doc, from goip-write-keywords-status.md's V049, V052 and V055 and `ip` 7.2.0's `iproute_modify`. doccheck | task IDs one to one, the graph acyclic, no broken link or anchor | f84b1a14 |
| V001 | 2026-11-03 | LF1-T01 | `go vet ./tools/nlcapgen/`, `go test -count=1 ./tools/nlcapgen/`; `nlcapgen matrix -family kwlive -seed 1 -random 0` | all pass: `TestKwliveSpaces` (30 rows and 5 checks: the 27 forms and 9 duplicates, every setup step `ip` with no tolerated status, `pref` 255's axes, the `kwnh` modules, the same bytes twice). The rows file regenerates to the committed bytes | 8d56c16e |
| V002 | 2026-11-03 | LF1-T01 | mutation, as goip-write-keywords-status.md V002, `-run TestKwlive`: a form dropped from `kwroute4`; the setup given a tolerated rc 2; `pref 255` written as `pref 256` | 3 of 3 killed: by the ids after it and the count; the setup check; the boundary row | 8d56c16e |
| V003 | 2026-11-03 | LF1-T01 | `nix build --no-link .#checks.x86_64-linux.lint-baseline` in a worktree of the task's diff | passes first time | 8d56c16e |
| V004 | 2026-11-03 | LF2-T01 | `nix run .#nlcap-ws -- up`, `batch nix/microvms/workbench/kwlive.rows.jsonl --out <scratch>/kw1`, `down` (guest 7.3.0, iproute2 7.2.0) | `NLCAP_BATCH_DONE rows=36 ok=36 not_ok=0`: every rc as written, so no expectation needed correcting. The kernel's messages are the forms table's: `ECN bits must be 0`, `option not available for IPv6`, `File exists` for index 1, `non-group nexthops` | 8c0bc91c |
| V005 | 2026-11-03 | LF2-T01 | the same on a second fresh boot into `kw2`; each row's `ip.rc` and its `ip.pcap` message types compared with the first's | 36 of 36 `OK`; 0 rows differ. The types: a route row's link lookup and its `RTM_NEWROUTE` (a `vrf` row's second lookup is the VRF's), a nexthop row's lookup and `RTM_NEWNEXTHOP`, a link row's `RTM_NEWLINK` (`xdpoffload`'s after the lookup), the rule's `RTM_NEWRULE`, each acknowledged | 8c0bc91c |
| V006 | 2026-11-03 | LF2-T01 | `kw1` copied to `pkg/xtcpnl/testdata/7_3_0/ws/`; `go test -count=1 -race ./pkg/xtcpnl/ ./internal/goip/... ./internal/goipparity/ ./tools/nlcapgen/` | all pass; `TestWsCorpusRows` runs the 36 new rows (O1/O2). The captures are 940 KB | 8c0bc91c |
| V007 | 2026-11-03 | LF2-T02 | `go vet ./pkg/xtcpnl/`, `go test -count=1 -race ./pkg/xtcpnl/` | all pass: `TestKwliveProblem` (34 rows: each form's check positive, its wrong encoding negative, among them `RTA_PREF` as a u32, a table above 255 in the header byte, the bottom bit on the first label, xdp flags without `UPDATE_IF_NOEXIST`; an axis with no check, a space not in the family, a short body), `TestWsKwliveCorpus` (36 requests checked, 0 rows with none) | 3f03f22f |
| V008 | 2026-11-03 | LF2-T02 | mutation, `-run 'TestKwliveProblem\|TestWsKwliveCorpus'` under `MemoryMax=6G`: `FROM` not shifted in the model; the table byte read for a table ≥ 256; the bottom bit ignored | 3 of 3 killed: 12, 5 and 2 failing subtests. The third's first form (the check deleted) didn't build, so it was rerun as `bos` taken from the position | 3f03f22f |
| V009 | 2026-11-03 | LF2-T02 | `nix build --no-link .#checks.x86_64-linux.lint-baseline`, then `.#checks.x86_64-linux.netlink-audit`, in a worktree of the task's diff; codespell | all pass first time | 3f03f22f |
| V010 | 2026-11-03 | LF2-T03 | `kwroute4`, `kwroute6`, `kwlink`, `kwnh`, `kwrule` added to `wsO5Spaces`; `go test ./internal/goip/ -run TestWsGoipDifferential` | first run: 32 `same`, and `kwnh/ec-002`, `ec-003`, `dup-2`, `dup-3` `differs`: `ip`'s two bytes after `NHA_ENCAP_TYPE` were `9b 17`, and its three after `MPLS_IPTUNNEL_TTL` `7f 00 00`, where goip's are zero. With `wsO5ZeroEncapPad` the golden gains the five spaces (13, 9, 5, 8 and 1 `same`, 0 otherwise) and `# encap padding: 4 rows`; every other space's line is unchanged, and the lookup notes are the known `IFLA_EXT_MASK` alone | 1332e184 |
| V011 | 2026-11-03 | LF2-T03 | `TestWsO5ZeroEncapPad` (7 rows); mutation under `MemoryMax=6G`: `NLA_F_NESTED` not masked off `NHA_ENCAP`; `NHA_ENCAP_TYPE`'s padding left | all pass; 2 of 2 killed (5 and 6 failures: the table's positive rows and the four corpus rows) | 1332e184 |
| V012 | 2026-11-03 | LF2-T03 | `go vet ./internal/goip/`, `go test -count=1 -race ./internal/goip/... ./pkg/xtcpnl/`; lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell | all pass first time | 1332e184 |
| V013 | 2026-11-03 | LF3-T01 | `go vet ./internal/goipparity/`, `go test -count=1 -race ./internal/goipparity/ ./pkg/nlparity/` | all pass. The three forms tables read the `kwlive` spaces too: the 20 forms with no row now pick 23 `kwlive` rows (13 route, 4 link, 5 nexthop, 1 rule); the two-hop form's rc 0 moves from `route4/pw-004` to `kwroute4/ec-007`, and `pw-004` stays as the weights form's. Each row's argv, setup and floor (the `ip.pcap` record count: 4, 6 for a vrf row, 2 for a rule or a set by group) is the capture's | 04bb6059 |
| V014 | 2026-11-03 | LF3-T01 | mutation under `MemoryMax=6G`, `-run TestGw`: a vrf row's floor one short; `link set vrf1 up` dropped from `gwWsSetupVrf`; the repeated-argv pass-over dropped; the over-255 vrf form's cmd back to `\bvrf \S+` | 4 of 4 killed (2, 2, 14 and 3 failures) | 04bb6059 |
| V015 | 2026-11-03 | LF3-T01 | lint-baseline, then netlink-audit, in a worktree of the task's diff; codespell over the added lines | all pass first time | 04bb6059 |
| V016 | 2026-11-03 | LF3-T02 | `nix run .#microvm-x86_64-goip-parity -- --timeout 12000 --section route,link,nexthop,rule` at 04bb6059 | 927 rows: 926 `GOIP_PARITY_PASS`, `GOIP_PARITY_OVERALL_PASS`, but `GOIP_PARITY_UNGATED_DIVERGENCES 1`: `gw_kwnh_ec_003`'s `NHA_ENCAP`, `ip=0800010000065100050002000a7f0000 goip=…0a000000`, the decision's watched padding. The other 22 `gw_kw` rows pass | 04bb6059 |
| V017 | 2026-11-03 | LF3-T02 | `samePastPadding` in `diffAttrs`; `go test -count=1 ./pkg/nlparity/ ./internal/goipparity/ ./internal/goip/`, `go vet ./pkg/nlparity/` | all pass: `TestSamePastPadding` (13 rows: V016's two payloads the same; a value, a label, a missing attribute, the order, the `NLA_F_NESTED` bit, a non-nested inner attribute and a short tail each not), `TestDiffAttrsNestedPadding` (4 rows) | 3f4b7049 |
| V018 | 2026-11-03 | LF3-T02 | mutation under `MemoryMax=6G`, `-run 'TestSamePastPadding\|TestDiffAttrsNestedPadding'`: the `Nested` guard in `diffAttrs` dropped; the call dropped; the remainder check, the count check, the type check, the inner `Nested` guard, the recursion each dropped. Then lint-baseline and netlink-audit in a worktree of the diff | 7 of 7 killed; both checks pass first time | 3f4b7049 |
| V019 | 2026-11-03 | LF3-T02 | the section run again, at 3f4b7049 | 927 of 927 `GOIP_PARITY_PASS`, `GOIP_PARITY_OVERALL_PASS`, `GOIP_PARITY_UNGATED_CLEAN`; all 23 `gw_kw` rows pass, `gw_kwnh_ec_003` among them | 3f4b7049 |
| V020 | 2026-11-03 | LF3-T02 | `nix run .#microvm-x86_64-goip-parity -- --timeout 12000`, the full run, at 3f4b7049 with this commit's gating applied, so the 23 rows run gated | 1304 of 1304 `GOIP_PARITY_PASS`, `GOIP_PARITY_OVERALL_PASS`, `GOIP_PARITY_UNGATED_CLEAN` (`GOIP_PARITY_CONTROL_NOISY` as every run). With V019 each section has two consecutive clean runs | this commit |
| V021 | 2026-11-03 | LF3-T02 | the 23 rows in `gated_commands` and `TestAllowlistCommitted`'s earned list; the `gw_kw` exceptions out of the GW matrix tables' gated check and `TestUngatedSurfaceIsNotVacuous`; `go test -count=1 ./internal/goipparity/ ./pkg/nlparity/`; mutation: `link set d0 xdpoffload off` dropped from `gated_commands`; lint-baseline, then netlink-audit, in a worktree of the diff | all pass; the mutant killed by `TestGwLinkMatrixRows`, `TestUngatedSurfaceIsNotVacuous` and `TestAllowlistCommitted`; both checks pass first time | this commit |

## Decision log

- **New rows rather than a wider setup rule.** The selftest rows' `sysctl` steps don't change the forms' requests, but goip-parity runs Setup through `ip` on each side, so a `sysctl` step can't be a Setup step. Rows of our own with an `ip`-only setup reach the same forms without a second tool in the comparison.
- **`xdpoffload off` is rc 0 and an `index` in use `EEXIST`, not the plan's rc 2 and `EBUSY`.** `dev_xdp_attach` calls the driver only when the program changes, and a detach with none attached doesn't; `rtnl_newlink` finds `lo` by index 1 and the add's `NLM_F_EXCL` refuses it. The plan's forms table is corrected; LF2-T01's capture is the check.
- **Up to three duplicates per space, not one per add.** `deriveDupGone`'s limit, as the other spaces; GW5's selection passes duplicates over anyway, so more would add capture time and nothing to the matrix.
- **`xdpoffload off`'s flags are the axis and `XDP_FLAGS_UPDATE_IF_NOEXIST`.** The row's `xdp_flags` 8 is the mode; `xdp_parse` adds `UPDATE_IF_NOEXIST` unless `-force`, so the request sends 9 and O3 expects it.
- **O5 zeroes `ip`'s encap padding rather than calling the rows `differs`.** `ipnexthop_modify` hands `lwt_parse_encap` an uninitialized `char buf[1024]` (`ip/ipnexthop.c:1102`) and copies it with `addraw_l`; `rta_addattr_l` doesn't clear alignment padding, so the bytes are whatever was on the stack. The kernel ignores padding, so they aren't part of the request's meaning; goip writes zeros. It is the `RTA_METRICS` case again (`wsO5ZeroMetricsPad`), and gets its own count line so neither hides the other. Not reported upstream from here.
- **The `kwlive` rows are the last route rows.** The first `encap mpls` request autoloads `mpls_router` and `mpls_iptunnel` (`lwtunnel_valid_encap_type`'s `request_module`), and `mpls_router` adds AF_MPLS to an AF_UNSPEC `netconf show` and to `afstats`. Every row after the block is generic netlink or XFRM, so no row reads a reply the modules changed.
- **The vrf forms name their VRF.** Both vrf rows' setup makes `vrf0` (table 100) and `vrf1` (table 1111), so a pre pattern on the table alone matched both; each form now matches `vrf vrfN` in the cmd and `vrfN type vrf table` in the pre.
- **The one-label MPLS form excludes `ttl`.** `\bencap mpls \d+ ` matched the ttl row's duplicate at rc 2, whose argv then barred `kwnh/ec-003` from the ttl form; the form is now `\bencap mpls \d+ via `.
- **`kwroute6/dup-2` is the `pref` form's rc 2 row.** GW5's rule passes a duplicate over only where a pick has its argv; `pref 2`'s add isn't a pick, so its duplicate is a row, as `route6/gone-3` was.
- **nlparity compares a nest past its padding, rather than holding `gw_kwnh_ec_003` back.** V016 is the watched divergence: `ip`'s three bytes after `MPLS_IPTUNNEL_TTL` are its stack's. A top-level attribute's value already stops at `nla_len`, so its padding never reached a comparison; inside an `NLA_F_NESTED` attribute the payload was one value, padding and all. `samePastPadding` makes the two agree: two nested payloads that parse to the end into the same attributes, in the same order, with the same types and values (descending into inner nests), are the same. Anything else is still the one value finding it was. Only flagged nests: `IFLA_AF_SPEC` and the other unflagged nests stay compared whole. An allowlist entry would instead have suppressed any value difference in `NHA_ENCAP`.
- **Watch `kwnh/ec-003` in the VM.** nlparity compared `NHA_ENCAP` as one value, padding inside it included, and the workbench's `ip` put `7f 00 00` after `MPLS_IPTUNNEL_TTL`. The VM's did too (V016); the decision above settles it.
- **A separate family.** Adding the forms as axis values to `route`, `link`, `nexthop` or `rule` would change those spaces' pairwise rows and force a recapture of every row in them. `kwlive` leaves them byte for byte.
