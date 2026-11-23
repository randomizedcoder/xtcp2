# goip tunnel, tunl6 and tuntap: implementation status

**Last updated:** 2026-11-23

**Summary:**
- The design is in [goip-tunnel.md](goip-tunnel.md); this document tracks progress against it. This increment
  numbers its evidence from **V001 / DOC001**.
- **8S (the sidecar): done.**
- **8C (the captures): done.** 217 rows, `ws/tunnel` (171) and `ws/tuntap` (46), each with a tnlsys sidecar. Two batches
  on one boot agree byte for byte, except the holder pids of the eight `-d` rows with holders (V002).
  - `internal/tnlsys` answers the ioctl, sysfs and `/proc` questions, live or from a JSON snapshot.
  - `tnlsnap` records a snapshot by running the real `ip` under ptrace, keyed by each ioctl's exact input bytes.
  - goip has its `GOIP_TNLSYS` seam (V001).

## Contents

- [Update rules](#update-rules)
- [Task checklist](#task-checklist)
- [Verification ledger](#verification-ledger)
- [Decision log](#decision-log)

## Update rules

- A state is one of `not started`, `in progress`, `blocked`, `done` or `deferred`. No percentages.
- A task is `done` only when its gates have passed and its evidence is in the [ledger](#verification-ledger).
- Evidence IDs (`V###` for code and VM runs, `DOC###` for docs) are immutable and append-only.
- Task IDs are never renumbered.

## Task checklist

| Done | Task | Work | State | Evidence |
|---|---|---|---|---|
| [x] | 8S | `internal/tnlsys`, `cmd/tnlsnap`, goip `runCtx.sys` | done | V001 |
| [x] | 8C | `matrix_tunnel.go`, `matrix_tuntap.go`, two batches | done | V002 |
| [ ] | 8P | `xtcpnl_tunnel_parm.go` + table tests + fuzz + O-check | not started | |
| [ ] | 8R | goip `obj_tunnel.go`, `obj_tuntap.go`; corpus replay | not started | |
| [ ] | 8B | `upstream/iproute2-tunnel-tuntap.md` | not started | |
| [ ] | 8L | live diff | not started | |

## Verification ledger

| ID | Task | Evidence |
|---|---|---|
| V001 | 8S | **Package `internal/tnlsys`:** `Sys` (`Ioctl`, `ReadProp`, `TunFDs`) and the three request constants; the struct sizes 52/76/16/24, `IfName` (cut like `strlcpy`) and `ArgSize`. `Live` makes a raw `SYS_IOCTL` with a 64-byte `0xA5` spill window, reads sysfs under an `os.Root` at `/sys` (so the `class/net` symlinks resolve inside it), and walks `/proc` unsorted, self excluded. `Snapshot` matches each ioctl on family, request, name and input bytes. A miss is `ErrNotRecorded`. `Parse` refuses unknown fields, bad hex, unknown requests, negative errnos and out longer than in. `Trace` (linux/amd64) is a ptrace tracer that follows threads; other arches get a stub. **Tool:** `cmd/tnlsnap` (nix package `tnlsnap` in `nix/binaries.nix`): `tnlsnap -- ARGS` traces `ip ARGS` and prints the snapshot (traced ioctls, the tun devices' props, the tun descriptor walk, and the run's argv, rc, stdout and stderr). It runs nothing but `ip` as PATH resolves it, since it is a root ptrace tool fed from a rows file; `Trace` takes an `*exec.Cmd` and runs the ptrace work on a goroutine that keeps its thread locked until it exits. **goip:** `runCtx.sys` comes from `openSys`: `GOIP_TNLSYS`, else an empty snapshot under `GOIP_REPLAY`, else `Live`. **Tests:** `TestTnlsys`, 12 rows. Positive: a JSON round trip; a fallback device answering per input; the same device with zero input; spill returned; a 16-router PRL. Negative: recorded ENODEV; unrecorded input; unknown device. Boundary: empty snapshot; an argument 1 byte short. Corner: v4 and v6 under one name; a 17-byte name cut to 15. `TestSnapshotProps` 5 rows, `TestSnapshotTunFDs` 3, `TestParseRefuses` 8, `TestIfNameAndArgSize` 10, `TestLoad` 3 and `TestSocketError` 3. `TestLiveIoctl`, 5 rows, unprivileged: ENODEV; a write request refused; an empty argument and one over 64 KiB refused; family 9999 failing at `socket(2)` as `SocketError`. `TestSpillOf` 4. `TestLiveReadProp`, 7 rows over a fake sysfs, including a slash or `..` in the device and a symlink out of the root. `TestLiveTunFDs`, 6 rows over a fake `/proc`: comm present or missing; `12x` scanning as pid 12, as sscanf's `%d` does; `2a` reading fdinfo 2; self, `self`, non-tun links and a missing fdinfo skipped. `TestTrace`, 10 rows, unprivileged: the test binary re-run as a helper makes `SIOCGETTUNNEL` on `lo` (EOPNOTSUPP) and `SIOCGET6RD` on `nosuch0` (ENODEV), and the tracer records both with family, name and input, plus the helper's rc 3, stdout and stderr. `cmd/tnlsnap`: `TestRunUsage` 4 rows (no ip on PATH among them); `TestSnapshot` 8, over a fake sysfs and `/proc`, with `bonding_masters` and a non-tun `d0` skipped; `TestRunIP` 5, a fake `ip` on PATH (the test binary) recorded with its rc 4 and argv. `internal/goip` `TestOpenSys`, 6 rows. **Manual:** in `unshare -rn` on the host, with an ipip `t1`, `tnlsnap -- ip tunnel show` recorded `SIOCGETTUNNEL` for `tunl0` and `t1` (zero input, 52-byte outputs naming each). Coverage: tnlsys 82.3%, tnlsnap 87.2%. Gates: `go test -race ./internal/tnlsys/ ./cmd/tnlsnap/ ./internal/goip/`, vet (amd64 and arm64), gofmt, golangci-lint (comprehensive) 0 issues on the three packages, `CGO_ENABLED=0 go build ./...`. |
| V002 | 8C | **Spaces** (`tools/nlcapgen`, registered in `main.go`): `tunnel`, 171 rows (72 commands plain and `-j`, 27 negatives), modules ipip/ip_gre/sit/ip_vti/ip6_tunnel/ip6_gre/ip6_vti. Fixtures: `tnl4` (ipip1, ipip2, gre1-3 with keys/seq/csum and a multicast remote, sit1, sit2, isa1 and isan with routers, vti1, d0), `prl16` (isa16 with 16 routers), `tnl6` (ip6t1, ip6t2, ip6g1, vti61, gre6x), `ext` (ipip external, gretap, erspan, ip6gretap). `tuntap`, 46 rows (20 commands plain and `-j`, 6 negatives), module tun. Fixtures: `tun` (tap0 user 1000 group 100 one_queue, tun1 multi_queue vnet_hdr, tap2 pi), `proc` (a python `TUNSETIFF` holder on tap0, two on tun1), `xns` (a tap0 holder in another namespace). Every row has the readback `{tnlsys, "tnlsnap -- ARGS"}`. A `-d` tuntap row is serial and first kills the holders earlier rows left, since `/proc` is shared. **Rows:** `nix/microvms/workbench/{tunnel,tuntap}.rows.jsonl`. **Corpus:** `pkg/xtcpnl/testdata/7_3_1/ws/{tunnel,tuntap}`, kernel 7.3.1, iproute2 7.2.0; `TestWsCorpusRows` holds all 217. **Batches:** two on one boot, all 217 rows OK both times. stdout, stderr, rc and sidecar are identical in 209 rows, and in the other 8 (`-d` with holders, plain and `-j`) but for the holder pids. In every sidecar the traced run's stdout, stderr and rc equal the captured `ip`'s. The only spill is gre6x's, 24 bytes (76 − 52, T19). The only ioctl failures are `SIOCGET6RD` EINVAL (no 6rd in the guest kernel, K1, which ip skips) and `SIOCGETTUNNEL` ENODEV in the two named-path negatives. **Recorder fixes the batches found** (V001's tnlsnap): (1) the spill kept `ip`'s own stack past the kernel's write, so the tracer now fills the window with `0xA5` at entry, cuts the spill at the last changed byte and restores the rest; (2) the `/proc` walk ran for every row and recorded other rows' holders, so it now runs only when `ip` would walk (`walksProc`, which mirrors `ip.c`'s option loop and `do_cmd`'s object order). After the guest runs, the holders are killed and the tunnel and tun modules unloaded (tunnel6 stays: xfrm6_tunnel holds it). **Tests:** `TestTunnelSpace`, 24 rows (positive: v4 and v6 lists, v6 named, a gre name, `-j -s`; negative: gre9 rc 1, keys without a mode, an unknown mode 255, `get_addr32` 1, `dev nosuch` kernel ENODEV, `tunnel add` 1, v6 named ENODEV; boundary: fallback devices only, ttl 300, 16 routers, a 21-bit flowlabel; corner: mode any, gre6x, encaplimit 300, `tunl`, batch any-sticks, v6 named then the list on stdin, external kinds). `TestTuntapSpace`, 13 rows, plus serial exactly for the 10 `-d` rows, each with the kill first. Both check determinism, the modules, the readback, writes only in negatives, and the `-j` twins. `TestDetailWords` 6. tnlsnap: `TestWalksProc` 21; `TestSnapshot` gains "no walk asked, none recorded". tnlsys: `TestSpillOf` 6 rows for the cut. **Correction to V001:** its goip gate was not met. `TestGoipImportSetIsHermetic` failed on the new `internal/tnlsys` import, which is now in goip's allowed set and pinned closure, with its reason. Coverage: tnlsys 83.2%, tnlsnap 93.1%. Gates: `go test -race ./internal/tnlsys/ ./cmd/tnlsnap/ ./tools/nlcapgen/ ./internal/goip/ ./pkg/xtcpnl/`, vet (amd64 and arm64), gofmt, golangci-lint (comprehensive) 0 issues. |

## Decision log

- **2026-11-23: record by tracing `ip`, not by asking every question (8S, a change from the plan).**
  - The plan's `Record(sys, links)` would have asked each tunnel with a zeroed argument. But the kernel answers a
    fallback device per the input (`ip_tunnel_ctl` looks up the tunnel the passed parm describes). And
    `ip -6 tunnel show` fills the input with the ifindex and filter name, and the named path with the parsed
    arguments.
  - So tnlsnap runs `ip` itself under ptrace and records each ioctl with its exact input, and the snapshot matches on
    it. A question goip asks that `ip` did not ask is `ErrNotRecorded` (a replay error), not the plan's `ENODEV`. A
    wrong goip input therefore fails loudly instead of replaying another answer.
  - The plan's `TestLiveSys` (root only) became the unprivileged `TestTrace` and `TestLiveIoctl`, plus the manual
    user-namespace run in V001. 8C and 8L exercise the rest in the VM.
- **2026-11-23: the spill window (8S).**
  - A v4 `ip tunnel show NAME` on an ip6gre device passes 52 bytes, and the kernel writes 76 (lead T19). `Live` and
    `Trace` therefore watch 64 bytes past every argument.
  - `Live` does so for memory safety too: a Go slice the kernel overruns would corrupt the heap.
- **2026-11-23: 8C fixture choices.**
  - isatap tunnels come from `ip tunnel add mode isatap`. One made over netlink (`ip link add ... isatap`) reports
    protocol 0, so `ip` never reads its routers (K2). `isan` keeps one such tunnel to pin that.
  - No 6rd rows: the guest kernel has no `CONFIG_IPV6_SIT_6RD`, so `ip tunnel 6rd` is EINVAL and every
    `SIOCGET6RD` fails, which `ip` skips silently (K1).
  - No `tnlnofb` fixture: `net.core.fb_tunnels_only_for_init_net` is one setting for the whole guest, and the
    workbench VM is shared, so setting it would change every other session's new namespaces. `show gre9` (ENODEV on
    the named path) covers the failed ioctl instead.
  - `-d` tuntap rows are serial and kill earlier holders first. `show_processes` walks the shared `/proc`, so a
    parallel row's holder, or one exiting mid-walk, would otherwise show up.
- **2026-11-23: the recorder records only what `ip` asks (8C, a change to 8S's tnlsnap).**
  - Spill: the window past an argument is filled with `0xA5` at entry and cut at the last changed byte at exit, as
    `Live` does. The bytes after the cut go back to the tracee. A kernel reply ending in `0xA5` bytes would be cut
    short; no request here does that.
  - The `/proc` walk is recorded only when `ip` would make it. goip asks `TunFDs` only under `-d tuntap`, so a
    replay of any other row never meets a missing walk.
