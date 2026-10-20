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

The latest pass of each check (VC011, operator-run on `l` at 8c6b50eb, KVM in
the nix build sandbox, QEMU 11.0.2). Go test counts are `--- PASS` lines in
the guest's serial log; every run had no `--- FAIL` or `--- SKIP`.

| check | guest kernel | sentinels | result | what it covers |
|---|---|---|---|---|
| `vm-goip-argv-paths` | Linux 6.18.39 | 8 of 8 | 51 rows pass, 0 fail | every `/ip` design example and handler row against `goip` (byte-identical) and `ip` (no named gaps) |
| `vm-linkmonitor-embedding` | Linux 6.18.39 | 4 of 4 | 12 Go tests pass | the embedding API in-process and inside xtcp2, `TestLinkMonitorOnDemandScrape` against guest sysfs |
| `vm-linkmonitor-ionic` | Linux 7.1.4 | 11 of 11 | 99 Go tests pass | real ionic/ionic_rdma on rocm-ernic: discovery, request contracts, monitor, CLI differential, events, recorded events, remove, unplug, hotplug, recovered events |
| `vm-linkmonitor-ionic-faults` | Linux 7.1.4 | 3 of 3 | 18 Go tests pass | generic-100g profile with injected faults; eth_rx fired 28 of 282, eth_tx 31 of 310, stats_vals 1 of 18; the monitor's counters agree with sysfs, ethtool and the fault pattern |

Outputs (GC-rooted by the operator's `result-vm-all`):

- `/nix/store/ak6rz3j4a7lrbqgbhr7v3a03pglgq5cv-vm-check-goip-argv-paths`
- `/nix/store/v0z7391bi53q1n81ypvszmn1c3chi1gd-vm-check-linkmonitor-embedding`
- `/nix/store/87nmczf1zd1i772n1l8kz6lgq5c122h0-vm-check-linkmonitor-ionic`
- `/nix/store/6pvasypvcz6z2l74fxkfb79nlzgxiq83-vm-check-linkmonitor-ionic-faults`

## Tasks

| id | task | state | evidence |
|---|---|---|---|
| VC-T01 | `mkVmCheck.nix` and its table test (`checks.vm-check-runner`, 13 rows) | done | VC001 |
| VC-T02 | goip-argv-paths, embedding, ionic, ionic-faults wired: `checks.vm-*`, `vm-all`, `test-*-vm-check` | done (eval and build, no boot) | VC002 |
| VC-T03 | each check passes once on `l` (one guest at a time, asked first) | done | VC004 |
| VC-T04 | retire the duplicated launcher code once VC-T03 passes | done | VC009, VC011 |
| VC-T05 | `microvm-lifecycle` check: `requiredSystemFeatures` and its log in `$out` | done (boot pending) | VC010 |
| VC-T06 | `/tmp` cleanup of the old evidence dirs, after the user has seen the list | done | VC007 |
| VC-T07 | Go source filtering, so a change outside the Go inputs keeps the guests cached (separate commits) | done | VC008 |

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
- [ ] Boot `checks.x86_64-linux.microvm-lifecycle-x86_64` once in the
  sandbox (VC-T05).
- [ ] `test-linkmonitor-rdma-vm` (microvm.nix, TCP console ports) and
  `test-linkmonitor-performance-vm` (timings, wants a quiet host) stay apps.

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
