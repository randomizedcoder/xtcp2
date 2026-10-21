# Guest tests as nix checks: status

Branch `nix/vm-checks`, off `docs/ip-argv-paths` (3c950fc2). Started 2026-10-20.

## Why

The serial-console guest tests were `nix run` launchers only. Each run left a
`mktemp` directory in `/tmp`, `cat` the whole serial log (terminal size
queries included) to the console, and could not be part of an unattended
build. The rocm-ernic ionic guest exists so that real-driver validation runs
from a nix target; it should be a check like any other.

## Design

- `nix/microvms/mkVmCheck.nix` takes a launch command, sentinels, a fail
  pattern, a timeout and an optional post-check, and returns:
  - `runner`: `vm-run-<name> EVIDENCE_DIR [auto|kvm|tcg]`. stdin is
    `/dev/null` (the SIGTTIN fix from ff670868), the serial log is stripped of
    escapes and CRs, scratch goes to a work directory that is removed, and only
    the summary is printed.
  - `check`: the runner in the build sandbox with `requiredSystemFeatures =
    [ "kvm" ]`, evidence in `$out`, cached by its inputs.
  - `app`: `test-<name>-vm`, evidence under
    `~/.cache/xtcp2-vm/<name>/<UTC time>`, last N kept.
- The shared part is how a guest is run, not how it is built: the ionic guest
  needs qemu-vm.nix with rocm-ernic's vfio-user module, which microvm.nix's
  constructor does not model. qemu-vm.nix and microvm.nix guests can both use
  the runner.
- Since VC-T04 the `test-<name>-vm` apps are mkVmCheck's (goip-argv-paths,
  embedding, ionic-faults). The ionic launcher keeps `--capture`, `--promote`
  and `--dry-run` but boots through the same runner, passing
  `xtcp2.rdmacapture=1` as `$VM_KERNEL_PARAMS`.

## Sandbox facts (probe, 2026-10-20, this host)

| what | result |
|---|---|
| `/dev/kvm` with `requiredSystemFeatures = [ "kvm" ]` | present, writable |
| `unshare --user --map-root-user --net` | works |
| `ip tuntap add` (needs `/dev/net/tun`) | fails: not in the sandbox; after the rebuild with `"/dev/net/tun?"`, works (VC003) |

So `vm-linkmonitor-ionic-faults` needs `extra-sandbox-paths = /dev/net/tun`
on the builder. On 2026-10-20 the user agreed to add `"/dev/net/tun?"` to
`nix.settings.extra-sandbox-paths` in `~/nixos/desktop/l/configuration.nix`
(applied by the user's `make`), and `vm-all` now includes the fault run.
`l` has `max-jobs = 1`, so `vm-all` boots one guest at a time there.

## Integration results

The latest pass of each check (VC019, operator-run on `l` at main a9cf7dba,
KVM in the nix build sandbox, QEMU 11.0.2). Go test counts are `--- PASS` lines in
the guest's serial log; no run had a `--- FAIL`, and only the rdma guest's
three expected skips (veth has no rings, pause or FEC).

| check | guest kernel | sentinels | result | what it covers |
|---|---|---|---|---|
| `vm-goip-argv-paths` | Linux 6.18.39 | 8 of 8 | 51 rows pass, 0 fail | every `/ip` design example and handler row against `goip` (byte-identical) and `ip` (no named gaps) |
| `vm-linkmonitor-embedding` | Linux 6.18.39 | 4 of 4 | 12 Go tests pass | the embedding API in-process and inside xtcp2, `TestLinkMonitorOnDemandScrape` against guest sysfs |
| `vm-linkmonitor-ionic` | Linux 7.1.4 | 11 of 11 | 99 Go tests pass | real ionic/ionic_rdma on rocm-ernic: discovery, request contracts, monitor, CLI differential, events, recorded events, remove, unplug, hotplug, recovered events |
| `vm-linkmonitor-ionic-faults` | Linux 7.1.4 | 3 of 3 | 18 Go tests pass | generic-100g profile with injected faults; eth_rx fired 28 of 281, eth_tx 31 of 310, stats_vals 1 of 16; the monitor's counters agree with sysfs, ethtool and the fault pattern |
| `vm-linkmonitor-rdma` | Linux 7.1.4 | 10 of 10 | 61 Go tests pass, 3 skip (`TestLiveRequestShapes` ethtool RINGS, PAUSE, FEC on veth) | software RoCE (rxe): discovery, rename, netlink events against the recorded ones, faults, no rdma-core library opened, umad unloaded and loaded, denied uverbs, delete and recreate, recordings of every request shape (8 files in `nlrecord/`) |
| `vm-linkmonitor-performance` | Linux 6.18.39 | 3 of 3 | live tests pass (`live_exit=0`), RDMA native benchmarks pass (`rdma_native_exit=0`) | root-only live netlink tests, `BenchmarkPerformance{LocalMachine,RealisticFleet}` with CPU, heap, block and mutex profiles, an nlmon pcap, `bench-linkmonitor-rdma-native --native`; 912 KiB of evidence |

Outputs (GC-rooted by the operator's `result-vm-all`):

- `/nix/store/66mzxm82vnfy574515ikga8v4zjzj3jm-vm-check-goip-argv-paths`
- `/nix/store/5ygwl768h2bfmi0cg6a0cldkbghjnlcj-vm-check-linkmonitor-embedding`
- `/nix/store/jnw9npd8vc0b0zb0ls28fgy4lfb65b4k-vm-check-linkmonitor-ionic`
- `/nix/store/w619nl5iyygn6zwqdygkr4vza9p889wi-vm-check-linkmonitor-ionic-faults`
- `/nix/store/9a7frgdp7fharmb4z7b6hcdsisp397kv-vm-check-linkmonitor-performance`
- `/nix/store/v217524k1wydf193l2wma0224j8mkv72-vm-check-linkmonitor-rdma`

## Tasks

| id | task | state | evidence |
|---|---|---|---|
| VC-T01 | `mkVmCheck.nix` and its table test (`checks.vm-check-runner`, 13 rows) | done | VC001 |
| VC-T02 | goip-argv-paths, embedding, ionic, ionic-faults wired: `checks.vm-*`, `vm-all`, `test-*-vm-check` | done (eval and build, no boot) | VC002 |
| VC-T03 | each check passes once on `l` (one guest at a time, asked first) | done | VC004 |
| VC-T04 | retire the duplicated launcher code once VC-T03 passes | done | VC009, VC011 |
| VC-T05 | `microvm-lifecycle` check: `requiredSystemFeatures` and its log in `$out` | done | VC010, VC012 |
| VC-T06 | `/tmp` cleanup of the old evidence dirs, after the user has seen the list | done | VC007 |
| VC-T07 | Go source filtering, so a change outside the Go inputs keeps the guests cached (separate commits) | done | VC008 |
| VC-T08 | `vm-linkmonitor-rdma`: the rxe microVM through mkVmCheck (every step's sentinel, `nlrecord/` in the evidence) | done | VC013, VC014 |
| VC-T09 | `vm-linkmonitor-performance`: the performance guest through mkVmCheck, its bundle unpacked and its manifest checked; `checks.vm-linkmonitor-performance-bundle` | done | VC013, VC014, VC015, VC016 |
| VC-T10 | `vm-xtcp2-linkmonitor` (`nix/tests/xtcp2-linkmonitor.nix`): the xtcp2 unit with the embedded monitor, 7 sentinels ([design](../docs/design-linkmonitor-in-xtcp2.md)) | built, not booted; the user runs it | VC020 |

## TODO

- [x] VC-T03: all four checks pass in the sandbox (VC004).
- [x] Rerun `vm-all` after VC005's escape fix: all pass, 0 ESC and 0 CR
  bytes in each `serial.log` (VC006).
- [x] Caching: `flake.nix` passes `src = ./.`, so goip, the monitor and the
  test artifacts (and so every `vm-*` check) rebuilt on any change to the
  repo (VC006). `nix/lib/goSource.nix` now feeds the binaries,
  go-link-monitor, `rdma-capture-manifest`, the RDMA leaves and the
  embedding artifact; repo-wide checks keep the full tree (VC008).
- [x] After the user's rebuild of `l`: `nix config show` lists
  `/dev/net/tun?`, and a sandboxed build creates a TAP (VC003).
- [x] Rerun `vm-all` after VC009/VC010 (the runner changed, so all four
  boot): all pass (VC011).
- [x] Boot `checks.x86_64-linux.microvm-lifecycle-x86_64` once in the
  sandbox (VC-T05): all 20 self-test checks pass (VC012).
- [ ] The lifecycle check prints nothing on success under `-L` (the VM
  checks print `PASS: <name>: N sentinels`); a one-line `PASS` would make
  it consistent, at the cost of one more boot.
- [x] `test-linkmonitor-rdma-vm` and `test-linkmonitor-performance-vm` are
  checks too (VC-T08, VC-T09): the sandbox's own netns frees the console
  ports (VC012), and the performance check proves the guest runs while the
  app stays the way to take a measurement.
- [x] Boot `vm-all` (now six guests; the merge of main changed the Go
  inputs, so all six booted): all pass (VC014).
- [x] The performance artifact (`tests.test-linkmonitor-performance`) was
  still built from the full tree, so a docs edit or a new `result` link
  re-booted the performance guest; it now uses goSource (VC015).
- [x] Boot `checks.vm-linkmonitor-performance` once more, since VC015
  changed its artifact's inputs (not its Go content): pass, and the other
  five stayed cached (VC016).
- [x] Boot `vm-all` again: merging #269's lint fix (IV042, goip keyword
  constants) changed the Go inputs, so all six guests rebuild (`nix build
  --dry-run` lists every `vm-check-*`). No guest code changed. The first
  boot failed `vm-linkmonitor-ionic-faults` (VC017), the rerun passed all
  six (VC018).
- [ ] `vm-linkmonitor-ionic-faults` failed once on main (VC017) and passed on
  the rerun with nothing changed; the cause is unknown. If it recurs, run
  the app form (`nix run path:.#test-linkmonitor-ionic-faults-vm`), which
  keeps the full serial and ernic logs under `~/.cache/xtcp2-vm/`.
- [ ] A failed check prints only the last 60 lines of serial.log, which for
  VC017 were all systemd shutdown, and keeps no evidence. Print the lines
  matching the fail pattern, `--- FAIL` and `_test.go:` first. This changes
  every check's derivation, so all six guests boot again.
- [ ] The performance guest's `LINKMONITOR_BENCHTIME` never reached the
  guest from the host (the README said it did; corrected). Wire it through
  `VM_KERNEL_PARAMS` if a longer profile window is wanted.
- [x] Boot `vm-all` after #273 (listener TTL and IPv6 hop limit default to
  3 in go-link-monitor and xtcp2), which changed the Go inputs of all six:
  all pass with the same counts as VC018 (VC019). Every derivation except
  the `vm-run-*` and `vm-check-*` ones was prebuilt first, in 64 s.
- [ ] `pkg/linkmonitor` `TestRDMAEnabledLifecycle` failed twice in the
  `test-linkmonitor-rdma-unit` leaf ("snapshot condition timed out", rows
  `close` then `native`) at load average 24-36 on `l`, and passed
  `-count=5` when run directly. Its 5 s wait may be too short under load;
  if it recurs on a quiet host or in CI, lengthen the wait or make the
  condition event-driven.

## Evidence ledger

| id | date | command | result |
|---|---|---|---|
| VC001 | 2026-10-20 | `nix build --no-link -L path:.#checks.x86_64-linux.vm-check-runner` | pass, 13 of 13 rows |
| VC002 | 2026-10-20 | `nix eval` of the 4 `checks.vm-*` and `vm-all`; `nix build` of the 4 `test-*-vm-check` apps and 4 launchers; `checks.{nix-fmt,statix,deadnix}` | pass; no guest booted |
| VC003 | 2026-10-20 | sandbox probe (`requiredSystemFeatures = [ "kvm" ]`, `unshare --user --map-root-user --net`, `ip tuntap add`) after the rebuild of `l` | TAP created, `/dev/kvm` writable |
| VC004 | 2026-10-20 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (at b441fa3b) | pass: goip-argv-paths 8 sentinels, linkmonitor-embedding 4, linkmonitor-ionic 11, linkmonitor-ionic-faults 3 (eth_rx fired 28 of 282, eth_tx 31 of 310, stats_vals 1 of 17); outputs 44-96 KiB each. 5-6 ESC bytes survived per serial.log (ESC c, ESC 7, ESC 8, a DCS `+q` query) |
| VC005 | 2026-10-20 | `checks.vm-check-runner` with the DCS and two-byte escape row, `LC_ALL=C` on the strip (a UTF-8 locale's `[ -~]` skipped ESC c); the new strip over VC004's four logs | pass, 13 of 13; 0 ESC bytes left in each log |
| VC006 | 2026-10-20 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (at 4785ecad) | pass: 8, 4, 11 and 3 sentinels; eth_rx fired 28 of 281, eth_tx 31 of 310, stats_vals 1 of 17; 0 ESC and 0 CR bytes in each serial.log. goip and the embedding artifact rebuilt although only nix and docs files changed (`src = ./.`) |
| VC007 | 2026-10-20 | with the user's OK: removed the old launchers' `/tmp/{goip-argv-paths,linkmonitor-embedding,linkmonitor-ionic,linkmonitor-ionic-faults}-XXXXXX` | 46 directories (about 4.9 MB) removed, 0 left; other `/tmp` content untouched |
| VC008 | 2026-10-20 | `nix/lib/goSource.nix` (no `.md` or `.nix` outside testdata bar `cmd/go-link-monitor/METRICS.md`; no `flake.lock`, `docs/`, `.git`, `.github`, `.vscode`, `.ruff_cache`, `result*`). `nix eval` of `checks.vm-all.drvPath` after a README.md edit, a `.nix` comment edit and a `.go` edit; `nix build path:.#test-linkmonitor path:.#goip path:.#go-link-monitor path:.#rdma-capture-manifest` and `checks.{nix-fmt,statix,deadnix}` | drv unchanged by the Markdown and Nix edits, changed by the Go edit; all 14 `test-linkmonitor` leaves and the builds pass |
| VC009 | 2026-10-20 | VC-T04: `checks.vm-check-runner` (15 rows, with `VM_KERNEL_PARAMS` passthrough and a stale `QEMU_KERNEL_PARAMS`); `nix build` of the four `test-*-vm` apps, `checks.{nix-fmt,statix,deadnix}`, `test-linkmonitor-docs`; `test-linkmonitor-ionic-vm --capture --dry-run --native-out=DIR` and its usage errors (`--dry-run` alone, `--keep=0`, an unknown flag, `--promote` on the fault run) | pass, 15 of 15; dry run writes the manifest and stages; each usage error exits 2; no `/tmp` directory left; no guest booted |
| VC010 | 2026-10-20 | VC-T05: `microvm-lifecycle-x86_64` gains `requiredSystemFeatures = [ "kvm" ]`, stdin from `/dev/null`, and a directory `$out` (`summary.txt`, `serial.log`, `qemu.log`, escapes stripped by the shared `stripEscapes.nix`); comments claiming a check cannot have `/dev/kvm` corrected. `nix derivation show` of the check; `checks.{vm-check-runner,nix-fmt,statix,deadnix}`, `microvm-x86_64` | pass; `requiredSystemFeatures` is `kvm`; no guest booted |
| VC011 | 2026-10-20 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (at 8c6b50eb; `vm-all` out path matches `nix eval` at HEAD) | pass: goip-argv-paths 8 sentinels and 51 rows, linkmonitor-embedding 4 and 12 Go tests, linkmonitor-ionic 11 and 99, linkmonitor-ionic-faults 3 and 18 (eth_rx fired 28 of 282, eth_tx 31 of 310, stats_vals 1 of 18); no `--- FAIL`; 0 ESC and 0 CR bytes in each serial.log; Linux 6.18.39 and 7.1.4; outputs 44-100 KiB |
| VC012 | 2026-10-20 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-lifecycle path:.#checks.x86_64-linux.microvm-lifecycle-x86_64` on `l` (at 8c6b50eb; out path matches `nix eval` at HEAD) | pass: `XTCP2_SELF_TEST_OVERALL_PASS`, 20 of 20 checks (systemd through rtnetlink events), "PASS: all checks passed"; `$out` has `summary.txt`, `serial.log` (970 lines, 0 ESC and 0 CR bytes) and `qemu.log` (only the SIGTERM at teardown); the TCP console ports 12055/12056 sit in the sandbox's own netns |
| VC013 | 2026-10-20 | VC-T08/T09: `nix eval` of `checks.vm-linkmonitor-{rdma,performance}` (`requiredSystemFeatures` kvm) and `vm-all` (six entries); `nix build` of both apps, `checks.{vm-check-runner,vm-linkmonitor-performance-bundle,nix-fmt,statix,deadnix}`, `test-linkmonitor-docs`; usage errors (`--bogus`, two DIRs, `--keep=0`, `--accel=hvf`, an rdma positional) | pass; bundle test 5 of 5 (its first run failed all three bundle rows: the stand-in's `while read` dropped fold's unterminated last chunk, fixed in the stand-in); each usage error exits 2; no guest booted |
| VC014 | 2026-10-20 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (at a2aab875, with main merged in) | pass, all six: goip-argv-paths 8 sentinels and 51 rows; embedding 4 and 12 Go tests; ionic 11 and 99; ionic-faults 3 and 18 (eth_rx fired 28 of 282, eth_tx 31 of 311, stats_vals 1 of 14); rdma 10 and 61 Go tests, 3 expected skips, 8 nlrecord files, kernel 7.1.4; performance 3, `live_exit=0`, `rdma_native_exit=0`, kernel 6.18.39. 0 ESC and 0 CR bytes in each serial.log. Five outputs match `nix eval` at HEAD; the performance one does not (VC015) |
| VC015 | 2026-10-20 | `nix/tests/linkmonitor.nix`: `test-linkmonitor-performance` from goSource. `nix-diff` of the VC014 performance drv against HEAD's (the leaf: `xtcp2-test-linkmonitor-performance`'s full-tree source, changed by the new `result-vm-all` link); `nix build path:.#test-linkmonitor-performance`; `nix eval` of `checks.vm-all.drvPath` before and after adding a `.md` file and a `result-*` link; `checks.{nix-fmt,statix,deadnix}` | pass; `vm-all` drv unchanged by both; no guest booted |
| VC016 | 2026-10-20 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (at dc0d1362) | pass; only the performance guest booted, the other five came from the store. Performance 3 of 3 sentinels, `live_exit=0`, `rdma_native_exit=0`, kernel 6.18.39, 912 KiB of evidence, 0 ESC and 0 CR bytes in serial.log; `result-vm-all` equals `nix eval` of `checks.vm-all.outPath` at HEAD. Benchmarks (one run, loaded host, from the check's `bench.txt`): RealisticFleet gather 2834520 ns/op and 30171 allocs/op, http 3806872 ns/op; LocalMachine gather 791760 ns/op and 8464 allocs/op, http 1145648 ns/op; within 4% of VC014's |
| VC017 | 2026-10-21 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (main bb237c9e, after #269, #271) | fail: `vm-linkmonitor-ionic-faults` guest printed `LINKMONITOR_IONIC_FAIL rc=1` 23 s after boot, in its first steps (`ernic-rdma-up` active, discovery, `TestIonicFaultStatsVals`); the printed tail is all shutdown and the failed check kept no evidence, so the failing step is unknown. goip-argv-paths and embedding passed; nix stopped the rest. Host load was 14 to 16 shortly before |
| VC018 | 2026-10-21 | user: the same command, rerun with nothing changed | pass, all six, `result-vm-all` equals `nix eval` of `checks.vm-all.outPath` at main. goip-argv-paths 8 sentinels; embedding 4 and 12 Go tests; ionic 11 and 99; ionic-faults 3 and 18 (eth_rx fired 28 of 281, eth_tx 31 of 310, stats_vals 1 of 16); rdma 10 and 61, 3 expected skips, 8 nlrecord files; performance 3, `live_exit=0`, `rdma_native_exit=0`. No `--- FAIL`; 0 ESC and 0 CR bytes in each serial.log. Benchmarks (one run, loaded host): RealisticFleet gather 2843965 ns/op and 30171 allocs/op, http 3863391 ns/op; LocalMachine gather 834679 ns/op and 8464 allocs/op, http 1189724 ns/op |
| VC019 | 2026-10-21 | user: `env -u LD_LIBRARY_PATH nix build -L -o result-vm-all path:.#checks.x86_64-linux.vm-all` on `l` (main a9cf7dba, after #273) | pass, all six, `result-vm-all` equals `nix eval` of `checks.vm-all.outPath` at main. goip-argv-paths 8 sentinels; embedding 4 and 12 Go tests; ionic 11 and 99; ionic-faults 3 and 18 (eth_rx fired 28 of 281, eth_tx 31 of 310, stats_vals 1 of 16); rdma 10 and 61, 3 expected skips, 8 nlrecord files; performance 3, `live_exit=0`, `rdma_native_exit=0`. No `--- FAIL`; 0 ESC and 0 CR bytes in each serial.log. Benchmarks (one run, loaded host): RealisticFleet gather 3155046 ns/op and 30171 allocs/op, http 3983351 ns/op; LocalMachine gather 824010 ns/op and 8464 allocs/op, http 1191457 ns/op. Allocations match VC018; RealisticFleet gather is 11% slower and the rest within 3%. #273 changes only listener setup, not the gather path, so this is host noise |
| VC020 | 2026-10-21 | VC-T10: `nix build --no-link -L path:.#test-xtcp2-linkmonitor-vm` (the runner app and the guest image, its guest script through shellcheck; no boot) and `nix eval` of `checks.vm-xtcp2-linkmonitor` in `vm-all` | pass; boot pending on `l` |
