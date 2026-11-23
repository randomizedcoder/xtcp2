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
- **6B-0 (bug classes): done.** `upstream/bug-classes.md` distils the prior iproute2
  findings into fifteen classes, each with examples and a recipe for finding it. The plan's
  leads B1-B17 came from it, and 6C adds more (DOC031).
- **6C (capture): in progress.** `matrix_bridge.go` writes 113 rows over nine fixtures,
  and `pkg/xtcpnl/testdata/7_3_1/ws/bridge/` holds the corpus. Two batches on one boot
  agree on rc, stdout, stderr, message types and flags. The rows confirm B1, B2, B3, B5,
  B6, B10, B21, B22 and B24 live, and add B25 and B26 (V097). Left: a batch on a fresh boot,
  which waits until the shared workbench VM is free.
- **6P (parsers): done.** `ParseBridgeLink` (AF_BRIDGE RTM_NEWLINK), `ParseTunnelMsg`
  (RTM_NEWTUNNEL) and `ParseBrVlanTables` (RTM_NEWVLAN as tables), over a new
  `AttrTable` that mirrors iproute2's `tb[]`. Two new request builders,
  `BuildDumpBrVlanRequest` and `BuildDumpTunnelRequest`. The corpus test rebuilds all
  214 requests bridge sent, decodes 213 replies and checks 41 `-j` outputs against
  them (V098).
- **6R (clone): done.** `gobridge` (`cmd/gobridge`, `internal/gobridge`, the nix package
  `gobridge`) replays all 113 corpus rows byte for byte (stdout, stderr, rc), with every
  recorded request asked for. Without `GOBRIDGE_REPLAY` it asks the kernel over
  NETLINK_ROUTE. It reproduces bridge's bugs as bridge has them: B1, B2, B3, B4, B5, B6,
  B7, B10, B11, B12, B13, B21, B22, B24, B25 and B26, and the B18/B19/B20 NULL reads as
  exit 139. A diff against `bridge` live in the VM is still to do (V099).

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
| 6 — bridge | wsreplay → capture → parser → tests → `gobridge` clone → bug hunt | in progress | V096, DOC031, V097, V098, V099 |

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | 6S | extract `internal/wsreplay`; govdpa/gotipc/godevlink become thin wrappers | done | V096 |
| [x] | 6B-0 | `upstream/bug-classes.md`: the bug-class checklist from the 12 prior docs | done | DOC031 |
| [ ] | 6C (NC-BR) | `matrix_bridge.go`, two-boot capture | in progress (fresh-boot batch left) | V097 |
| [x] | 6P (NP-BR) | `xtcpnl_bridge_link.go`, `xtcpnl_vnifilter.go` + table tests + fuzz + O3/O4 | done | V098 |
| [x] | 6R (NR-BR) | `gobridge` clone + corpus/unit/imports tests + nix pkg | done (live diff left) | V099 |
| [ ] | 6B | audit `bridge/*.c` against the checklist, record findings only (DOC032) | not started | |

## Verification ledger

| ID | Task | Evidence |
|---|---|---|
| V096 | 6S | **Package:** `internal/wsreplay` (`replay.go`) holds `Msg`, `Reply`, `SplitMsgs`, `HeaderLen`, the request/reply transaction split, `Replay.Talk`/`Pending`, `New(datagrams, opts)` and `Open(path, opts)`. `Options` carries what differed between the three copies: the clone's `ErrReplay` sentinel (every error wraps it, so `errors.Is` callers are unchanged), the `Canon` hook (`CanonSeqPID` by default; `CanonGenlCmd` also zeroes the genl command and version, as godevlink did), and the two names in the mismatch message, with the shorter padded so the hex lines align as before. **Clones:** `internal/{govdpa,gotipc,godevlink}/source.go` now hold type aliases `Msg`/`Reply`, their `Source` interface and sentinels, `netlinkHeaderLen`, and a `ReplaySource` that embeds `*wsreplay.Replay`. gotipc's still carries `Own`/`Addr`, `OwnAddr` and `NodeName`, so its unit tests' `ReplaySource{Own: …}` literals compile unchanged. **Imports guards:** the three closures gain `internal/wsreplay`. They also gain `pkg/strpolicy`: it was already failing on `main`, because PR #308 made xtcpnl's congestion-name decoder import it after #309's closures were written. gordma's guard already listed it. **Tests:** `TestReplay` has 14 rows, each with `description` and `expected`. Positive: a match; seq/pid ignored. Negative: a body differs; more requests than captured; a missing file and a non-pcap both wrap the sentinel. Boundary: an empty pcap; a DONE-only reply; a request with no reply. Corner: a multi-message datagram split in order; a reply before any request dropped; a truncated trailing message; `CanonGenlCmd` ignores the command byte while the default matches it. `TestMismatchAlignment` has 4 rows: govdpa/vdpa, godevlink/devlink, equal names, tool longer than clone. Coverage is 93.7%. `go test ./internal/wsreplay/ ./internal/govdpa/ ./internal/gotipc/ ./internal/godevlink/ ./internal/goip/` passes, with the three corpus tests unchanged. Also run: vet, gofmt, `lint-baseline`. |
| DOC031 | 6B-0 | **Doc:** `docs/netlink/upstream/bug-classes.md`, marked "Not submitted to netdev". It reads the twelve prior finding docs (bond, devlink, fou, mptcp, tipc, vdpa; and P, lwt, nh, rt, rta, ethtool, which are still working-tree docs) and sorts their ~70 findings into fifteen classes: C1 wrong attribute width, C2 print width or sign, C3 no presence or length check, C4 unbounded index, C5 `-j` ignored, C6 JSON broken on an error path, C7 JSON keys or types that vary, C8 wrong exit status, C9 argument parsing, C10 message text, C11 buffers and request bytes, C12 crash on a reachable path, C13 output before success (new with bridge), C14 filter logic, C15 kernel quirks. Each class lists its prior examples and a grep or reading recipe. "How to apply it" sets the order: read the code, cross-check widths against the kernel, pin every lead with a row, confirm against the newest upstream code, and record only. Applied to `bridge/*.c` it gave the plan's B1-B17 ([gobridge.md](gobridge.md)). |
| V097 | 6C | **Rows:** `tools/nlcapgen/matrix_bridge.go` generates `nix/microvms/workbench/bridge.rows.jsonl`: 113 rows, 44 commands × plain/`-j` plus 25 negatives, registered as family `bridge` in `nlcapgen/main.go`. Every row is `proto route` with modules `bridge`,`vxlan`, in its own namespace (no `initns`, no `serial`). **Fixtures:** `bare`; `br` (br0 `vlan_filtering 1`, dummy ports d1/d2, d1 vid 10 pvid untagged and 20-25, d2 vid 30, br0 vid 10 self, d2 `learning off cost 7 priority 5 hairpin on`); `brglob` (`mcast_vlan_snooping 1`, vid 10 `mcast_snooping 0 mcast_igmp_version 3`); `brmst` (`mst_enabled 1` at creation, vid 10 → MSTI 3, d1 state learning in it); `brmstnov` (MST on, d2 with no VLAN); `vxtun` (external vxlan vx0 as a port, `vlan_tunnel on`, vid 100 → 1000 and 200-202 → 2000-2002); `brwide` (d3 with vids 2-4094); `vnif` (vnifilter vxlan vx1 over dummy d9, VNIs 100-102, 200 group 239.1.1.1, 300 remote 2001:db8::1, 16777214-16777215); `brmaster` (a port named `master`). Every device has a fixed MAC and `addrgenmode none` before up. **Setup lessons:** MST can be enabled only while no port has VLANs (EBUSY, "MST mode can't be changed while VLANs exist"), so `brmst` sets it when br0 is made. A `tunnel_info` for a VLAN not yet on the port is a bare EINVAL with no extack (K2), so the VLAN is added first. `ip` reads a bare `master` as its keyword, so `brmaster` uses `name master`/`dev master`. **Options:** `-c` is `-color` (matched before `-compressvlans`) and compression is `-com`; the `-c vlan show` rows capture the ANSI colour, and under `-j` there is none. **Pinned by** `TestBridgeSpace`: 31 outcome rows (positive, negative, boundary and corner, each with description and expected), 6 fixture-order rows, a plain/`-j` pairing check, an addrgenmode-before-up check over every step, the row count, and a determinism check. **Live outcomes:** all 113 rows match their expected rc. Unknown option, unknown object and `help` exit 255 before any request. Unknown sub-commands, `vni sh` (strcmp), `vlan global sh` (B21), `vlan help`, `link show dev` (incomplete), duparg on `dev`/`vid`, and B1 are ack-then-fail, rc 255. nodev on each object's filter is kernel ENODEV from `ll_link_get`, rc 255. `vni show dev d1` is kernel EINVAL with extack "The device is not a vxlan device", rc 1, and prints "Dump ternminated" (B3). `vlan show dev if999` is rc 0 with the kernel's ENODEV ignored (B24). B10: with MST off, `mst show` prints its header and "Dump terminated", rc 255 (`-j` prints `[]`). On `brmstnov`, `mst show dev d1` prints d1's entry before aborting on d2. **JSON:** every `-j` stdout parses. `-j -s vlan show` keys VLANs `"vid"` where the other shows use `"vlan"` (B25), and compressed ranges print `"flags": []` (B26). **Corpus test:** a failed dump's extack comes in NLMSG_DONE (the int error, then TLVs, with no echoed request), which `wsObserve` did not read. `wsDoneExtack` now reads it, pinned by `TestWsDoneExtack` (8 rows). `go test ./pkg/xtcpnl/ -run Ws` passes over the new corpus. **Determinism:** batches A and B ran back to back on one boot of the shared workbench VM (113/113 OK each). Batch B is committed: 113 row dirs, 360 KB. `ip.rc`, `ip.err` and `ip.out` are identical on all 113 rows, and so are message types and flags. Bodies differ only in RTM_NEWLINK (IFF_RUNNING and IFLA_OPERSTATE from linkwatch timing, in `ll_init_map`'s AF_UNSPEC dump, which bridge never prints; IFLA_BRPORT_FORWARD_DELAY_TIMER, a live timer) and in the NLMSG_ERROR of the nodev rows, which echoes iproute2's time-based seq. **Not yet done:** the fresh-boot batch. Another session had the VM up and in use, so it was not rebooted under them. |
| V098 | 6P | **Table:** `pkg/xtcpnl/xtcpnl_attrtable.go` `AttrTable` is `parse_rtattr_flags`'s `tb[]`: types 0..max, first occurrence wins, `NLA_F_NESTED` masked, larger types dropped. `Has`, `Bytes` and the width-checked `U8`/`U16`/`U32`/`U64`/`Str` stand for `tb[X]` and `rta_getattr_*`; a getter's ok is false where iproute2 would read past the attribute (class C3). `TestAttrTable`: 15 rows. **Decoders:** `ParseBridgeLink` (`xtcpnl_bridge_link.go`) takes the ifinfomsg, the top-level table, PROTINFO as a nested `IFLA_BRPORT_*` table when it has `NLA_F_NESTED` (else the bare u8 state `print_protinfo` reads), and AF_SPEC: its table (MODE, FLAGS), the VLAN list in wire order with ranges unpaired, the tunnel nests, and the MST entries (first `IFLA_BRIDGE_MST` wins). `ParseTunnelMsg` (`xtcpnl_vnifilter.go`) takes tunnel_msg and each `VXLAN_VNIFILTER_ENTRY` as a table, with its STATS nest. `ParseBrVlanTables` (`xtcpnl_brvlan.go`) gives each RTM_NEWVLAN entry and global-options nest as a table, plus an entry's STATS nest. Every malformed stream is an error at any level, and so is a short `IFLA_BRIDGE_VLAN_INFO`, which bridge casts unchecked (B27, `vlan.c:1295`). **Builders:** `BuildDumpBrVlanRequest(flags)` (RTM_GETVLAN + `BRIDGE_VLANDB_DUMP_FLAGS`, always present) and `BuildDumpTunnelRequest(ifindex, flags)`. Link dumps reuse `BuildDumpLinkRequestExt` with the new `RtextFilterBrvlan`/`BrvlanCompressed`/`Mst` masks; stats reuse `BuildDumpStatsRequest`; lookups reuse `BuildGetLinkByNameRequest`. **Tests:** `TestParseBridgeLink` has 17 rows. Positive, from the corpus: d2's protinfo (cost 7, priority 5, learning off, hairpin on), d1's VLAN list, the `-com` range pair, vx0's tunnel map, d1's MST entries, and K1 (br0's own entry names itself as master). Negative: a short body, a truncated attribute, a 2-byte VLAN_INFO, a truncated nest. Boundary: vid 1 and 4094, an empty AF_SPEC, B10's reply with no AF_SPEC. Corner: legacy u8 PROTINFO, an unknown `IFLA_BRIDGE_*`, MST twice, a negative ifindex. `TestParseTunnelMsg` has 9 rows (vnif's entries and stats; short, truncated, a truncated entry; empty and vni 16777215; a skipped non-ENTRY; a v6 group). `TestParseBrVlanTables` has 7 rows (`-d`, `-s -d` and global replies; short, truncated; empty; a skipped type). Fuzzers `FuzzParseBridgeLink`, `FuzzParseTunnelMsg` and `FuzzParseBrVlanTables` are seeded from the corpus; 20 s each found nothing. Benchmarks: `BenchmarkParseBridgeLink`, `BenchmarkParseTunnelMsg`, `BenchmarkParseBrVlanTables`. Coverage of the new code is 84-100% per function. **O3/O4** (`xtcpnl_ws_bridge_test.go`): `TestBridgeParseCmd` (25 rows) models `main`'s options (`-c` is colour, `-com` compression), `do_cmd`'s prefix dispatch (`m` is mdb), each object's sub-command dispatch (matches or strcmp), and the show loops (dev and master not else-if, vid via atoi, unknown words ignored). `TestWsBridgeCorpus` rebuilds every request each row sent, byte for byte with seq aside: `ll_init_map`'s AF_UNSPEC dump (mask 0x109); a by-name RTM_GETLINK for each filter name missing from that dump, its answer deciding nodev, or `ifN` (B24); then the object's request(s). It decodes every object reply and checks each `-j` output against them: link (filters, master names, mtu), vlan (ranges), tunnelshow, mst, vni, global, and `-s vlan show` (pure port entries skipped; VLANs keyed `"vid"`, B25). The counts are pinned at 214 requests, 5 lookups, 213 replies and 41 JSON outputs. **Universe:** RTM_GETTUNNEL was an exclusion ("bridge only"); it is captured now, so the exclusion is gone. Gates: `go test ./pkg/xtcpnl/`, vet, gofmt, `lint-baseline` (tiers 0-2 clean), `netlink-audit`, `kernel-citation-audit`. |
| V099 | 6R | **Clone:** `internal/gobridge` follows bridge/*.c function by function. `gobridge.go`: main's option loop (`matches`, `-c` colour before `-com`, the unknown-option text), `do_cmd`'s prefix dispatch, the usages, `atoi`, and the show-argument loops. `link.go`: `brlink_show` with B1's missing else, `print_linkinfo`, `print_link_flags`, `print_name_and_link`, `print_protinfo` (`-d`, `-o`), `print_af_spec`. `vlan.go`: `vlan_show`'s three paths (BRVLAN/COMPRESSED link dump, the RTM_GETVLAN `-d` dump, the two `-s` stats dumps, tunnelshow included, B22), `print_vlan_info`, `print_vlan_tunnel_info`, `print_vlan_rtm`/`_opts`/`_global_opts` with querier state and router ports, and `print_vlan_stats_attr` (pure port entries skipped). `vni.go`: `vni_show` ("Dump ternminated"), `print_vni` with glibc's `inet_ntop` text, `print_vnifilter_entry_stats`. `mst.go`: `mst_show` (return -1, B4), `print_msts` (B10's early -1), `print_mst_entry` (B11). `print.go` is json_print/json_writer/color.c: callers format the text as the C format would and pass the JSON token, which keeps each print's width and sign (B12: mtu is signed JSON, unsigned text). `rtnl.go` is `rtnl_dump_filter` (DONE's extack whatever the flags, ENOENT/EOPNOTSUPP silent, EMSGSIZE, perror), `rtnl_dump_error`, the suppressed `ll_link_get` (by name or index), `ll_name_to_index` with the `ifN` fallback (B24), and `ll_index_to_name`. Raw-type reads use three new xtcpnl helpers, because `print_vlan` (`parse_rtattr`, no mask) and `print_msts` (`parse_rtattr_one(NLA_F_NESTED|IFLA_BRIDGE_MST)`) depend on the flag bits: `ParseAttrTableFlags`, `WalkRawAttrs` and `FindRawAttr`, pinned by `TestAttrTableRaw` (8 rows). **Divergences, by design:** a NULL read bridge makes (B18, B19, B20) is exit 139 with stdout lost, as bridge dies. An attribute bridge reads past (B8, B9, B23, B27) is refused with a message naming the lead, rc 1, since the bytes past it are not in the reply. `-s` with multicast router ports (`br_print_router_port_stats`), `-netns`, `-batch` and every write are not implemented (rc 255). **Sources:** `ReplaySource` (the `wsreplay` core) and `LiveSource` (one NETLINK_ROUTE socket with `NETLINK_EXT_ACK`, read until DONE/ERROR or a get's one reply). **Tests:** `TestGobridgeCorpus` replays all 113 rows. stdout, stderr and rc match bridge's and no request is left unasked; the 45 `-j` outputs are valid JSON. A check that the test bites: fixing the "ternminated" typo fails 1 row, and a space in the stats header fails 3. `TestGobridgeUnit` has 41 rows over a scripted source. Positive: `-com` ranges, `-j link show`, hwmode, IFLA_LINK peers, `-o`, `-j -p`, querier state and router ports. Negative: unknown object, nodev, a refused write, an NLMSG_ERROR mid-dump, B4, an exhausted script, link help, B27. Boundary: empty dumps, `[]`, vid 4094, IFLA_LINK 0, the DONE errnos. Corner: `vni sh`, B1, `vid abc`, B10, B11, B13 (state and hwmode), B12, B7, B15, B16, B18, B19, B20, colour (dark palette) and none under `-j`, B22, B3, and an uncached master looked up by index. `TestHelpers` has 22 rows (`atoi` wrap, `ifN`, negative padding widths, glibc IPv6 text, `%#llx` of 0, `print_time64`, s64 wrap, COLORFGBG, `check_ifname`). `TestGobridgeImports` mirrors gotipc's guard and adds `pkg/cint`. Coverage is 80.8%; the live socket is the main gap. **Mutants** (each under `systemd-run --user --scope -p MemoryMax=6G`, `go test -timeout=180s`) were killed: drop range compression, swap PVID/Untagged, skip the vni header, drop `RTEXT_FILTER_MST`, fix B1 with else-if, mst failure exit 1 (fix B4), stats key `"vlan"` (fix B25), drop the `ifN` fallback. One survived and is equivalent: colour left on under `-j`, which can never print, since colour applies only to text. **Package:** `nix build .#gobridge` builds; its binary, with `GOBRIDGE_REPLAY`, reproduces ec-038 (`-com vlan show`), pw-042 (`-j -s vni show`) and ng-020 (B10) byte for byte. **Left:** the live run beside `bridge` in a VM netns, which waits for the shared VM. Gates: `go test ./internal/gobridge/ ./pkg/xtcpnl/`, vet, gofmt, `lint-baseline` (tiers 0-2 clean), `netlink-audit`, `kernel-citation-audit`, deadnix, statix, nix-fmt. |

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
- **2026-11-22: tables, not typed fields, for bridge's replies (6P).** The clone has
  to make bridge's decisions on bridge's bytes: which `tb[X]` is tested, at what width
  it is read, and what is printed when it is missing or short. A first-wins
  `AttrTable` with width-checked getters gives the renderer that read for read,
  without a typed field for each of the dozens of attributes bridge reads. `ParseBrVlanTables` sits
  beside `ParseBrVlan` rather than changing it, because `ParseBrVlan`'s tests compare
  whole structs.
- **2026-11-22: ranges stay unpaired (6P, a change from the plan).** The plan had a
  RANGE_END without a BEGIN as a decode error. bridge prints from whatever VID came
  before it (`print_vlan_info` sets `last_vid_start` on every entry that is not an
  END), so the decoder keeps the list as sent and the clone pairs it as bridge does.
  A short VLAN_INFO is still an error: there bridge reads past the attribute (B27),
  and no byte-exact copy of that exists.
- **2026-11-22: bridge's crashes and overreads (6R).** Where bridge reads through a NULL
  pointer (B18-B20), gobridge exits 139 with stdout lost, as gotipc does for tipc's
  segfault. Where it reads past a short attribute (B8, B9, B23, B27), the output
  depends on memory that isn't in the reply, so gobridge refuses with the lead's name.
  The replay test cannot reach either; the unit rows pin both.
- **2026-11-22: the replay core is extracted before a fourth copy (6S).** gobridge
  would have been the fourth copy of the same ~150 lines. Each clone keeps its own
  sentinel and message text, so no caller or test changed.
