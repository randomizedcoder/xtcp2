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
  - `app`: `test-<name>-vm-check`, evidence under
    `~/.cache/xtcp2-vm/<name>/<UTC time>`, last N kept.
- The shared part is how a guest is run, not how it is built: the ionic guest
  needs qemu-vm.nix with rocm-ernic's vfio-user module, which microvm.nix's
  constructor does not model. qemu-vm.nix and microvm.nix guests can both use
  the runner.
- The `test-<name>-vm` launchers stay until each check has passed on `l`, and
  the ionic one stays for `--capture`, `--promote` and `--dry-run`.

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

## Tasks

| id | task | state | evidence |
|---|---|---|---|
| VC-T01 | `mkVmCheck.nix` and its table test (`checks.vm-check-runner`, 13 rows) | done | VC001 |
| VC-T02 | goip-argv-paths, embedding, ionic, ionic-faults wired: `checks.vm-*`, `vm-all`, `test-*-vm-check` | done (eval and build, no boot) | VC002 |
| VC-T03 | each check passes once on `l` (one guest at a time, asked first) | open | |
| VC-T04 | retire the duplicated launcher code once VC-T03 passes | open | |
| VC-T05 | `microvm-lifecycle` check: `requiredSystemFeatures` and its log in `$out` | open | |
| VC-T06 | `/tmp` cleanup of the old evidence dirs, after the user has seen the list | open | |

## TODO

- [ ] VC-T03: run `vm-goip-argv-paths`, `vm-linkmonitor-embedding`,
  `vm-linkmonitor-ionic`, then (with `/dev/net/tun`) the fault run.
- [x] After the user's rebuild of `l`: `nix config show` lists
  `/dev/net/tun?`, and a sandboxed build creates a TAP (VC003).
- [ ] `test-linkmonitor-rdma-vm` (microvm.nix, TCP console ports) and
  `test-linkmonitor-performance-vm` (timings, wants a quiet host) stay apps.

## Evidence ledger

| id | date | command | result |
|---|---|---|---|
| VC001 | 2026-10-20 | `nix build --no-link -L path:.#checks.x86_64-linux.vm-check-runner` | pass, 13 of 13 rows |
| VC002 | 2026-10-20 | `nix eval` of the 4 `checks.vm-*` and `vm-all`; `nix build` of the 4 `test-*-vm-check` apps and 4 launchers; `checks.{nix-fmt,statix,deadnix}` | pass; no guest booted |
| VC003 | 2026-10-20 | sandbox probe (`requiredSystemFeatures = [ "kvm" ]`, `unshare --user --map-root-user --net`, `ip tuntap add`) after the rebuild of `l` | TAP created, `/dev/kvm` writable |
