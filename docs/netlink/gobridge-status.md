# gobridge (bridge link, vlan, vni, mst): implementation status

**Last updated:** 2026-11-22

**Summary:**
- The design and task plan are in [gobridge.md](gobridge.md); this document tracks
  progress against them. The arc continues the genl-clones arc
  ([genl-clones-status.md](genl-clones-status.md)), whose ledger ran to V095 / DOC030.
  This one starts at **V096 / DOC031**.
- **6S (wsreplay): done.** The ws-pcap replay core the clones each had a copy of is
  now one package, `internal/wsreplay`. govdpa, gotipc and godevlink keep thin
  wrappers with their own sentinels and messages, and their tests pass unchanged
  (V096).

## Contents

- [Update rules](#update-rules)
- [Phase states](#phase-states)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- A state is one of `not started`, `in progress`, `blocked`, `done` or `deferred`. No percentages.
- A task is `done` only when its gates have passed and its evidence is in the
  [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and
  append-only, and continue the shared sequence.
- Task IDs are never renumbered.

## Phase states

| Increment | Work | State | Evidence |
|---|---|---|---|
| 6 — bridge | wsreplay → capture → parser → tests → `gobridge` clone → bug hunt | in progress | V096 |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | 6S | extract `internal/wsreplay`; govdpa/gotipc/godevlink become thin wrappers | done | V096 |
| [ ] | 6B-0 | `upstream/bug-classes.md`: the bug-class checklist from the 12 prior docs | not started | |
| [ ] | 6C (NC-BR) | `matrix_bridge.go`, two-boot capture | not started | |
| [ ] | 6P (NP-BR) | `xtcpnl_bridge_link.go`, `xtcpnl_vnifilter.go` + table tests + fuzz + O3/O4 | not started | |
| [ ] | 6R (NR-BR) | `gobridge` clone + corpus/unit/imports tests + nix pkg | not started | |
| [ ] | 6B | audit `bridge/*.c` against the checklist, record findings only | not started | |

## Verification ledger

| ID | Task | Evidence |
|---|---|---|
| V096 | 6S | **Package:** `internal/wsreplay` (`replay.go`) holds `Msg`, `Reply`, `SplitMsgs`, `HeaderLen`, the request/reply transaction split, `Replay.Talk`/`Pending`, `New(datagrams, opts)` and `Open(path, opts)`. `Options` carries what differed between the three copies: the clone's `ErrReplay` sentinel (every error wraps it, so `errors.Is` callers are unchanged), the `Canon` hook (`CanonSeqPID` by default; `CanonGenlCmd` also zeroes the genl command and version, as godevlink did), and the two names in the mismatch message, with the shorter padded so the hex lines align as before. **Clones:** `internal/{govdpa,gotipc,godevlink}/source.go` now hold type aliases `Msg`/`Reply`, their `Source` interface and sentinels, `netlinkHeaderLen`, and a `ReplaySource` that embeds `*wsreplay.Replay`. gotipc's still carries `Own`/`Addr`, `OwnAddr` and `NodeName`, so its unit tests' `ReplaySource{Own: …}` literals compile unchanged. **Imports guards:** the three closures gain `internal/wsreplay`. They also gain `pkg/strpolicy`: it was already failing on `main`, because PR #308 made xtcpnl's congestion-name decoder import it after #309's closures were written. gordma's guard already listed it. **Tests:** `TestReplay` has 14 rows, each with `description` and `expected`. Positive: a match; seq/pid ignored. Negative: a body differs; more requests than captured; a missing file and a non-pcap both wrap the sentinel. Boundary: an empty pcap; a DONE-only reply; a request with no reply. Corner: a multi-message datagram split in order; a reply before any request dropped; a truncated trailing message; `CanonGenlCmd` ignores the command byte while the default matches it. `TestMismatchAlignment` has 4 rows: govdpa/vdpa, godevlink/devlink, equal names, tool longer than clone. Coverage is 93.7%. `go test ./internal/wsreplay/ ./internal/govdpa/ ./internal/gotipc/ ./internal/godevlink/ ./internal/goip/` passes, with the three corpus tests unchanged. Also run: vet, gofmt, `lint-baseline`. |

## Decision log

- **2026-11-22: bottom-up order, so bridge before tunnels (user).** With goip covering
  every `ip` object except `tunnel`/`tunl6`/`tuntap`, I had recommended `ip tunnel`. The
  user chose `bridge`: "we should start at the lower layer, so bridging is below
  tunneling". After bridge's link/vlan/vni/mst come `fdb`/`mdb show` (increment 7), then
  the tunnels.
- **2026-11-22: every increment opens with a bug-class sweep (user).** The user turned
  down the first plan because its bug hunt listed only the leads noticed in passing:
  "We have already found multiple iproute2 issues, so based on this we need to make
  sure we are aware of more". So before capturing, the prior findings are distilled
  into `upstream/bug-classes.md`, and every class is applied to the target code. The
  plan lists the leads up front (B1–B17 in [gobridge.md](gobridge.md)).
- **2026-11-22: the replay core is extracted before a fourth copy (6S).** gobridge
  would have been the fourth copy of the same ~150 lines. Each clone keeps its own
  sentinel and message text, so no caller or test changed.
