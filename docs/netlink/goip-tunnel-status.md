# goip tunnel, tunl6 and tuntap: implementation status

**Last updated:** 2026-11-23

**Summary:**
- The design is in [goip-tunnel.md](goip-tunnel.md); this document tracks progress against it. This increment
  numbers its evidence from **V001 / DOC001**.
- **8S (the sidecar): done.**
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
| [ ] | 8C | `matrix_tunnel.go`, `matrix_tuntap.go`, two batches | not started | |
| [ ] | 8P | `xtcpnl_tunnel_parm.go` + table tests + fuzz + O-check | not started | |
| [ ] | 8R | goip `obj_tunnel.go`, `obj_tuntap.go`; corpus replay | not started | |
| [ ] | 8B | `upstream/iproute2-tunnel-tuntap.md` | not started | |
| [ ] | 8L | live diff | not started | |

## Verification ledger

| ID | Task | Evidence |
|---|---|---|
| V001 | 8S | **Package `internal/tnlsys`:** `Sys` (`Ioctl`, `ReadProp`, `TunFDs`) and the three request constants; the struct sizes 52/76/16/24, `IfName` (cut like `strlcpy`) and `ArgSize`. `Live` makes a raw `SYS_IOCTL` with a 64-byte `0xA5` spill window, reads sysfs under an `os.Root` at `/sys` (so the `class/net` symlinks resolve inside it), and walks `/proc` unsorted, self excluded. `Snapshot` matches each ioctl on family, request, name and input bytes. A miss is `ErrNotRecorded`. `Parse` refuses unknown fields, bad hex, unknown requests, negative errnos and out longer than in. `Trace` (linux/amd64) is a ptrace tracer that follows threads; other arches get a stub. **Tool:** `cmd/tnlsnap` (nix package `tnlsnap` in `nix/binaries.nix`): `tnlsnap -- ARGS` traces `ip ARGS` and prints the snapshot (traced ioctls, the tun devices' props, the tun descriptor walk, and the run's argv, rc, stdout and stderr). It runs nothing but `ip` as PATH resolves it, since it is a root ptrace tool fed from a rows file; `Trace` takes an `*exec.Cmd` and runs the ptrace work on a goroutine that keeps its thread locked until it exits. **goip:** `runCtx.sys` comes from `openSys`: `GOIP_TNLSYS`, else an empty snapshot under `GOIP_REPLAY`, else `Live`. **Tests:** `TestTnlsys`, 12 rows. Positive: a JSON round trip; a fallback device answering per input; the same device with zero input; spill returned; a 16-router PRL. Negative: recorded ENODEV; unrecorded input; unknown device. Boundary: empty snapshot; an argument 1 byte short. Corner: v4 and v6 under one name; a 17-byte name cut to 15. `TestSnapshotProps` 5 rows, `TestSnapshotTunFDs` 3, `TestParseRefuses` 8, `TestIfNameAndArgSize` 10, `TestLoad` 3 and `TestSocketError` 3. `TestLiveIoctl`, 5 rows, unprivileged: ENODEV; a write request refused; an empty argument and one over 64 KiB refused; family 9999 failing at `socket(2)` as `SocketError`. `TestSpillOf` 4. `TestLiveReadProp`, 7 rows over a fake sysfs, including a slash or `..` in the device and a symlink out of the root. `TestLiveTunFDs`, 6 rows over a fake `/proc`: comm present or missing; `12x` scanning as pid 12, as sscanf's `%d` does; `2a` reading fdinfo 2; self, `self`, non-tun links and a missing fdinfo skipped. `TestTrace`, 10 rows, unprivileged: the test binary re-run as a helper makes `SIOCGETTUNNEL` on `lo` (EOPNOTSUPP) and `SIOCGET6RD` on `nosuch0` (ENODEV), and the tracer records both with family, name and input, plus the helper's rc 3, stdout and stderr. `cmd/tnlsnap`: `TestRunUsage` 4 rows (no ip on PATH among them); `TestSnapshot` 8, over a fake sysfs and `/proc`, with `bonding_masters` and a non-tun `d0` skipped; `TestRunIP` 5, a fake `ip` on PATH (the test binary) recorded with its rc 4 and argv. `internal/goip` `TestOpenSys`, 6 rows. **Manual:** in `unshare -rn` on the host, with an ipip `t1`, `tnlsnap -- ip tunnel show` recorded `SIOCGETTUNNEL` for `tunl0` and `t1` (zero input, 52-byte outputs naming each). Coverage: tnlsys 82.3%, tnlsnap 87.2%. Gates: `go test -race ./internal/tnlsys/ ./cmd/tnlsnap/ ./internal/goip/`, vet (amd64 and arm64), gofmt, golangci-lint (comprehensive) 0 issues on the three packages, `CGO_ENABLED=0 go build ./...`. |

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
