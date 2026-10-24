# Nix builds and checks

Run commands from the repository root. [flake.nix](../flake.nix) exposes the
packages, checks, apps and development shell assembled by [default.nix](default.nix).
Tool versions come from [versions.nix](versions.nix) and the locked flake inputs.
See [FLAVORS.md](FLAVORS.md) for binary and container variants.

## Link-monitor verification

Run all fifteen focused monitor checks with one target:

```sh
nix build path:.#test-linkmonitor -L
```

Use `path:.` during development to include untracked implementation and Nix
files. Once files are tracked, `nix build .#test-linkmonitor -L` also works.
Nix builds missing results and reuses successful cached results for the same
inputs. Any failing check fails the aggregate.

The targets are defined in [tests/linkmonitor.nix](tests/linkmonitor.nix):

| Target | Scope |
|---|---|
| `test-linkmonitor-unit` | Complete command, `pkg/linkmonitor/...`, `pkg/nlconn` and `pkg/xtcpnl` suites with CGO disabled |
| `test-linkmonitor-race` | Same packages with CGO and the race detector |
| `test-linkmonitor-vet` | Go vet over both package trees |
| `test-linkmonitor-lint` | Existing comprehensive lint policy over both package trees |
| `test-linkmonitor-format` | Gofmt verification of both package trees |
| `test-linkmonitor-replay` | All 40 link-state kernel/scenario fixture combinations; no skips |
| `test-linkmonitor-fuzz` | One 30-second session, two workers, per `fuzzTargets` entry |
| `test-linkmonitor-rdma-abi` | Go-native RDMA ABI guard: `nix/tests/linkmonitor-abi.c` against the pinned uapi and rdma-core headers must match the committed layout, then `TestABILayout` |
| `test-linkmonitor-parse-guards` | Go-native RDMA parsing rules: no package of go-link-monitor or pkg/linkmonitor has cgo files under any build tag (R4-T02), no bounds check outside the length hint in each listed decoder file (`bce.txt`), the `-m` report (`escape.txt`), zero-allocation and manifest tests |
| `test-linkmonitor-rdma-capture` | Go-native RDMA capture tooling: the `rdmacapture` redaction and manifest tests, a dry run of `rdma-native-capture` through `rdma-capture-manifest` with no real value left, `TestCaptureManifests` on the result, and a tampered copy that must fail verify |
| `test-linkmonitor-docs` | Monitor documentation links, metric references and tracker consistency; checker regression tests |
| `test-linkmonitor-rdma-unit` | Full monitor suite with `CGO_ENABLED=0`, then race tests (cgo only for the race detector), RDMA fuzzing and benchmarks |
| `test-linkmonitor-rdma-lint` | Vet, including the VM tags, and unchanged comprehensive lint policy |
| `test-linkmonitor-rdma-runtime` | Test tooling only: the uapi constant header check and libibmad's PortInfo decoder, which rdmacaps' decode is checked against |
| `test-linkmonitor-rdma-build` | Public-library smoke executable and the VM test binaries, none with `NEEDED` entries; every remaining tag combination builds with `CGO_ENABLED=0`; explicit missing-sysfs diagnostics with continued host publication |
| `test-linkmonitor-command` | The single `CGO_ENABLED=0` executable (`go-link-monitor-core` is an alias): help/version, no `NEEDED` entries, no rdma-core and no source tree in its closure (`share/go-link-monitor/source.txt` names the source without referencing it), binary and closure sizes, clean-environment live HTTP/host metrics, signals, shutdown/restart and command tests |
| `test-linkmonitor-embedding` | Actual xtcp collector coexistence, repeated embedding races, xtcp unit/vet/lint and compiled disposable guest tests; xtcp's `TestLinkMonitorOnDemandScrape` needs `/sys`, so it skips in the sandbox and the guest runs it |

The aggregate compiles the embedding guest test executables but does not boot a
VM. Execute the disposable integration gate separately:

```sh
nix run path:.#test-linkmonitor-embedding-vm -- --accel=auto
```

`auto` tries KVM and falls back to QEMU software emulation. Use `--accel=tcg`
to force software execution or `--accel=kvm` to require acceleration. The runner
has a 15-minute deadline, isolates temporary files, preserves its serial log
and artifact paths, and requires the guest success sentinel.
Missing KVM never counts as a passing skipped integration test. The guest tests
real veth events, classification exclusion, test-only eligible inventory,
missed-event recovery, baseline ownership and restart in disposable namespaces,
and runs xtcp's on-demand scrape test against the guest's sysfs.
No host network changes or physical/RDMA hardware claims are involved.

The four RDMA targets and executable gate are defined in `tests/linkmonitor-rdma.nix`.
Since R4-T02 (cmd/go-link-monitor/GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md) every
build is `CGO_ENABLED=0`: RDMA events come from rtnetlink and the NLDEV
monitor, and InfiniBand PortInfo from a native umad exchange, so no binary
links libibverbs or libibumad. rdma-core from the existing nixpkgs lock
remains as test tooling only (the uapi header check, libibmad's PortInfo
decoder, and `rdma`/`ibv_devinfo` in the VM guests). The build output
contains the smoke executable and the VM test binaries. Device enumeration
opens no HCA contexts and explicitly does not count as hardware validation.
The unit target adds capability fuzzing, repeated races and cached-counter
benchmarks. These software checks send no real management packets.

### Monitor performance measurements

`test-linkmonitor-performance` joins the run-all target as its fifteenth leaf.
It builds the `CGO_ENABLED=0` benchmark executables and checks equivalence, synthetic
lifecycle behavior, races, benchmark probes and report validation with pinned
tools. Build caching applies to these correctness checks and executables.

The repeatable performance commands are assembled in
[tests/linkmonitor-perf](tests/linkmonitor-perf), while the compiled artifact
remains the cached `test-linkmonitor-performance` leaf:

| Command | Purpose |
|---|---|
| `nix run path:.#bench-linkmonitor-smoke` | Short runner wiring check; evidence is explicitly smoke-only |
| `nix run path:.#bench-linkmonitor` | Ten-repetition microbenchmarks and the bounded synthetic scenario matrix |
| `nix run path:.#bench-linkmonitor-soak` | Fresh ten-minute warmed large-schema profiles and resource samples |
| `nix run path:.#bench-linkmonitor-rdma-native -- --output DIR` | Go-native RDMA benchmarks: the native decoders, event path, netlink receive, umad codec and exchange, and the existing rtnetlink decoder baseline, with `-count=10`, CPU and heap `pprof -top`, and `benchstat`; `--baseline DIR` compares the event path and umad exchange with the committed cgo output of an earlier run (the cgo backends were removed in R4-T02); `--smoke` checks the wiring, and `nix build path:.#test-linkmonitor-rdma-native-bench` gates it |
| `nix run path:.#bench-linkmonitor-startup -- --output DIR LABEL=BINARY...` | Start-up of whole `go-link-monitor` builds, e.g. this tree against an earlier revision's package: exec to the first healthy `/metrics` reply, `-version` to exit and RSS at ready, alternating runs (`--count`, default 20), compared with `benchstat`. The monitor's logs are not kept, since they name host interfaces |
| `nix run path:.#siege-linkmonitor -- --url URL --output DIR` | External Siege load: 90% `/metrics`, 5% `/healthz`, 5% `/readyz` |
| `nix run path:.#profile-linkmonitor -- --url URL --output DIR --profile` | Capture and validate endpoint CPU/heap/block/mutex pprof data |
| `nix run path:.#pgo-linkmonitor -- --profile CPU.pprof --output BIN` | Build with pinned Go profile-guided optimization |
| `nix run path:.#compare-linkmonitor-pgo -- BASE.txt PGO.txt` | Compare identical benchmark outputs with `benchstat` |

Each command accepts `--output DIR`; the directory must not already exist.
Nix caches compilation and correctness checks, but measurement results are
written to the requested directory outside the store. The ten-minute soak uses
256 ports, 64 fields, one scraper and a 15-second cadence so RSS/thread
plateaus are measurable on the shared host. The 256-port/1,024-field case and
ten-scraper slow/disconnected cases remain in the bounded diagnostic matrix;
ten concurrent 524,288-sample gathers exceed this host's memory budget.

Run fresh measurements separately, without other builds or tests:

```sh
nix run path:.#bench-linkmonitor -- --output /tmp/linkmonitor-baseline
```

The output directory must not exist. The default is a unique temporary directory.
The runner uses ten one-second microbenchmark repetitions, a separate warmup,
and sequential synthetic scenarios with five-second warmup and thirty-second
measurement. `--gomaxprocs` defaults to at most four permitted CPUs; affinity is
recorded, not changed. `--smoke` exercises the runner but produces **smoke-only**
evidence. `--baseline /path/to/previous` requires a compatible completed manifest
and runs the pinned benchstat comparison. Raw logs, per-command provenance,
CPU/resource summaries, scenario reports and benchstat variance are retained.
Incorrect metrics, failed tests and incomplete execution leave an incomplete
manifest and return nonzero. A scenario that has not populated its full schema
after warmup is retained as `capacity-limited`, with present/expected counts;
this can reflect cold-start cost or host contention, not just steady-state
capacity. Its latency is not an equal-size steady-state comparison. Benchstat
compares the fully populated microbenchmarks only. P09-T02 investigates these
limitations rather than the runner silently dropping the workload.
Individual latency probes and final reconciliation have five-minute safety
bounds within the ten-minute scenario timeout. Actual elapsed time is reported; this is not a
recovery-latency service-level objective. Recovery must still produce a fresh
complete reconciliation and the exact expected link count.

These are synthetic source measurements, not physical Ethernet, RoCE or
InfiniBand latency. The umad exchange benchmark runs the native transport over
a socketpair fake device; it performs no management I/O.
P09-T02 retains detailed profiles, syscall accounting and ten-minute soaks.

For root-only live-netlink tests and reproducible kernel evidence, run the
privileged disposable guest:

```sh
nix run path:.#test-linkmonitor-performance-vm            # ~/.cache/xtcp2-vm/linkmonitor-performance/<UTC>
nix run path:.#test-linkmonitor-performance-vm -- DIR     # or into DIR, e.g. to promote with --from DIR/linkmonitor-performance
```

The guest reuses the cached `test-linkmonitor-performance` artifact, boots with
four CPUs and 4 GiB of memory, captures rtnetlink traffic through `nlmon0`,
runs the live tests and representative benchmarks as root, and returns a
compressed evidence bundle containing the serial log, benchmark output, CPU /
heap / block / mutex profiles and `netlink.pcap`, plus the go-native RDMA
native benchmarks from `bench-linkmonitor-rdma-native --native` in `rdma-native/`. The bundle is
unpacked as `linkmonitor-performance/` beside `serial.log`. Pass `--accel=tcg`
when KVM is unavailable. Each benchmark runs for the guest's default 5 s:
`LINKMONITOR_BENCHTIME` is read inside the guest, and the host's environment
does not reach it. `checks.vm-linkmonitor-performance` is the same guest as a
cached check: it proves the live tests pass and every benchmark and profile
completes, and its timings are evidence of that build, not a fresh
measurement. For a measurement, run the app on a quiet host.

Build the standalone artifact:

```sh
nix build path:.#go-link-monitor       # CGO_ENABLED=0, RDMA events from netlink
nix build path:.#go-link-monitor-core  # an alias of the same package
```

It installs `bin/go-link-monitor`; `nix run path:.#go-link-monitor -- -help`
prints the flags without opening devices or baseline files. The package retains
its source provenance under `share/go-link-monitor`. The package does not change
the cgo configuration of xtcp2 or add monitor OCI/service deployment. Remaining
netclass metadata coverage and physical RDMA validation are tracked in the
monitor STATUS.

`checks.vm-linkmonitor-rdma`, or by hand `nix run path:.#test-linkmonitor-rdma-vm`, uses the existing microVM
constructor and lifecycle runner for software RoCE discovery/association,
the netlink event source against recorded expectations, denied uverbs access,
deletion/recreation, native umad, and a strace check that the monitor opens no
libibverbs, libibumad or provider library. It requires
accessible `/dev/kvm`; the app (not the check, whose sandbox has its own
network namespace) also needs the standard microVM console ports free. Each
step's sentinel must appear, and the guest's netlink recordings are kept as
`nlrecord/` in the evidence. It is outside the thirteen-check offline
aggregate. Missing KVM is unverified, not pass.
Physical InfiniBand capability validation remains separate. Production device classification still excludes virtual links.

The opt-in `nix run path:.#test-linkmonitor-ionic-vm` boots a qemu-vm.nix guest
with the `rocm-ernic` flake input's `nixosModules.ernic-guest`, under its
`ernic-run` wrapper, so the in-tree `ionic` and `ionic_rdma` drivers bind to an
emulated AMD Pensando NIC. It needs `/dev/kvm` and no console ports. Hot-unplug
is a guest PCI remove and rescan: QMP `device_del` of the vfio-user device aborts
QEMU 11.0.2 (`memory_region_del_subregion` assertion). `--promote` copies the
recorded netlink fixtures into the linuxio testdata. Try a local rocm-ernic
checkout with `--override-input rocm-ernic path:$HOME/Downloads/rocm-ernic`.

### Guest tests as checks

The serial-console guest tests are also flake checks, built by
`nix/microvms/mkVmCheck.nix`: the guest runs inside the build sandbox with
`/dev/kvm` (`requiredSystemFeatures = [ "kvm" ]`), the result keeps
`serial.log` (terminal escapes stripped) and `summary.txt`, and a pass is
cached by its inputs, so a rerun with nothing changed boots nothing:

```sh
nix build --no-link -L -j1 path:.#checks.x86_64-linux.vm-all   # one guest at a time
nix build -L path:.#checks.x86_64-linux.vm-linkmonitor-ionic -o result-vm-ionic
cat result-vm-ionic/summary.txt
```

| check | guest |
|---|---|
| `vm-goip-argv-paths` | `/ip/...` paths against `goip` and `ip` |
| `vm-linkmonitor-embedding` | the embedding API, in-process and in xtcp2 |
| `vm-linkmonitor-ionic` | the real ionic/ionic_rdma drivers on rocm-ernic |
| `vm-linkmonitor-ionic-faults` | rocm-ernic fault injection through a TAP |
| `vm-linkmonitor-rdma` | software RoCE (rxe) on the microVM constructor; netlink recordings in `nlrecord/` |
| `vm-linkmonitor-performance` | root-only live tests, benchmarks, profiles and an nlmon pcap, unpacked as `linkmonitor-performance/` |
| `vm-xtcp2-linkmonitor` | the real xtcp2 NixOS unit with the monitor on by default: paths, baseline modes, persistence across restart, `systemctl reload`, a missing link on `/metrics`, a corrupt baseline degrading, clean stop |

`vm-all` builds all seven. The fault run's TAP needs `/dev/net/tun`, which a
build sandbox has only when the builder sets `extra-sandbox-paths =
[ "/dev/net/tun?" ]` (host `l` does); without it the check fails with
`VM_CHECK_NO_TUN`. A failing check prints the log's last 60 lines;
`--keep-failed` keeps the tree.

`nix run path:.#test-<name>-vm -- [--accel=auto|kvm|tcg] [--keep=N]` runs
the same guest outside the sandbox (TCG too), keeps the last N runs (default
5) under `~/.cache/xtcp2-vm/<name>/` and prints only the summary, plus the
log's tail on a failure. `test-linkmonitor-ionic-vm` adds `--capture`,
`--promote` and `--dry-run`, which write into the checkout.
`test-linkmonitor-performance-vm` also takes a `DIR` in place of the cache.
`checks.vm-check-runner` covers the runner itself with stand-in guests, and
`checks.vm-linkmonitor-performance-bundle` the performance guest's bundle
check.
`checks.microvm-lifecycle-x86_64` (the xtcp2 self-test microVM) also requires
`kvm` and keeps `summary.txt`, `serial.log` and `qemu.log` in its result.

Individual gate example:

```sh
nix build path:.#test-linkmonitor-race -L --out-link result-linkmonitor-race
cat result-linkmonitor-race/check.log
```

The aggregate exposes logs under `result/test-linkmonitor-*/`. Each check
retains its command, tool version, build environment, kernel identity and
immutable source paths. Tests use pinned tools and vendored dependencies;
the live netlink tests perform read-only queries in the build namespace.
See [VALIDATION.md](../cmd/go-link-monitor/VALIDATION.md) for detailed evidence
formats and coverage limits.

The aggregate is also registered as `checks.x86_64-linux.test-linkmonitor`.
It does not include the separate Nix formatting/static-analysis targets. To
run the monitor suite and those three checks together:

```sh
nix build path:.#test-linkmonitor \
  path:.#checks.x86_64-linux.nix-fmt \
  path:.#checks.x86_64-linux.deadnix \
  path:.#checks.x86_64-linux.statix -L
```

`nix flake check path:. -L` runs every registered repository check, including
the monitor aggregate and those Nix policy checks. It is broader than the
focused monitor suite, and it includes the `vm-*` guest checks, which boot
as many guests at once as `max-jobs` allows; add `-j1` for one at a time.
