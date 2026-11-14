# Red-team harness build plan (flake.nix + nix/ wiring)

Concrete implementation spec for the [Phase 0 harness](redteam-implementation.md#phase-0--harness-bootstrap)
described in the [red-team plan](redteam-plan.md): which files are created,
what each derivation contains, exactly where they wire into
[flake.nix](../flake.nix) → [nix/default.nix](../nix/default.nix), and in
what order to build it. Everything below was verified against the tree at
`e360b6f9` — package availability, flags, and existing conventions were
checked, not assumed. Task IDs (0.1–0.9) refer to the implementation plan's
Phase 0 table.

## Contents

- [What the existing infrastructure already provides](#what-the-existing-infrastructure-already-provides)
- [Design decisions](#design-decisions)
- [The wiring map](#the-wiring-map)
- [nix/redteam.nix specification](#nixredteamnix-specification)
- [vagrant/ tree specification](#vagrant-tree-specification)
- [The rt-1-1 walkthrough](#the-rt-1-1-walkthrough)
- [Build sequence](#build-sequence)
- [Open questions](#open-questions)

## What the existing infrastructure already provides

Verified inventory — nothing below needs to be invented:

| Need | Provided by | Notes |
|---|---|---|
| Vagrant + libvirt provider | pinned nixpkgs `pkgs.vagrant` (2.4.9) | **bundles the vagrant-libvirt plugin on Linux** — `pkgs/by-name/va/vagrant/package.nix` defaults `withLibvirt ? stdenv.hostPlatform.isLinux` and generates the plugin's `plugins.d` JSON itself. This retires the implementation plan's "vagrant not packaged cleanly in nixpkgs devshell" risk. A built `vagrant-2.4.9` with the plugin already exists in this machine's store |
| libvirt on the harness host | `l` | `libvirtd` 12.8 active, `qemu:///system` reachable, user in the `libvirt` group — verified |
| Daemon OCI image | `containers.oci-xtcp2` ([nix/containers/oci-xtcp2.nix](../nix/containers/oci-xtcp2.nix)) | `streamLayeredImage` — the build output is a **script that streams the tar to stdout** (`./result \| docker load`, the same pattern the tcp-stress microVM uses) |
| RT-11 work population | `containers.oci-xtcp2-tcp-stress` | ships `tcp_server`+`tcp_client` with a `TCP_MODE={server,client,both}` env-dispatch entrypoint and `TCP_COUNT`/`TCP_SLEEP`/`TCP_PADS` knobs — exactly the victim-socket population the plan needs. **Reuses, not rebuilds** the implementation doc's task 0.6 "binaries from xtcp2-all" |
| Attacker clients | `binaries.xtcp2client`, `binaries.xtcp2ctl` | all repo Go binaries are `CGO_ENABLED=0` → static ELF, runs on stock Ubuntu unmodified |
| gRPC scraper | `versions.grpcurl` (nixpkgs) | ⚠️ `cmd/grpcurl` is **README-only, not buildable** (nix/binaries.nix:42) — the attacker tooling must use nixpkgs' grpcurl, not a repo build |
| Deployment target semantics | `xtcp2SocketSinkArgs` in [nix/microvms/mkVm.nix:2414](../nix/microvms/mkVm.nix) | `-dest tcp:localhost:13001 -marshal jsonl -frequency 2s -timeout 1s` — jsonl, one record per line, is the catcher format the scenario asserts on |
| docker-in-a-VM precedent | `tcpStressLoadScript` (mkVm.nix:1154) | `docker load` from a stream script, dockerd readiness loop — the same shape `provision/victim.sh` follows |
| Shellcheck discipline | `writeShellApplication` + `checks` | every runner derivation is shellcheck'd at build; plain repo scripts get `checks.redteam-lint` |
| App/runner conventions | `lintFixOne`, `focusedQuality`, `oci-size-report` (nix/default.nix) | runners require repo root (`flake.nix` present), so WIP scenarios run from the working tree; `path:.` vs `.` source-view semantics documented in [nix/README.md](../nix/README.md) |

Two consequences worth calling out because they amend the merged
implementation doc (small PR rides with Phase 0):

1. Task 0.3's "vagrant-libvirt vendored via the plugin gem set" → just
   `pkgs.vagrant`, plugin included.
2. Task 0.6's "2 containers running tcp_server/tcp_client (from
   `binaries.xtcp2-all`)" → `docker load` the `oci-xtcp2-tcp-stress` tarball
   and run `TCP_MODE=both` containers; smaller, and it is the same image the
   microVM tcp-stress flavor uses.

## Design decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Scenario *drivers* are plain scripts in `vagrant/scenarios/`; each gets a `writeShellApplication` flake-app wrapper from `nix/redteam.nix` | drivers stay WIP-runnable from the working tree (the `path:.` principle); wrappers get built-in shellcheck and pinned `runtimeInputs`; scenario files themselves are plain so `vagrant` also accepts them unchanged |
| D2 | The image tarballs are `packages.redteam-oci`/`redteam-oci-tcp-stress` — the streamLayeredImage script piped through `gzip -n` inside the derivation — and the app wrapper exports their store paths; the scenario scps them into the VM (via `vagrant ssh-config` + ssh/scp, [lib/ssh.sh]) before `vagrant provision` | a streamLayeredImage script cannot execute inside the VM — its nix store deps don't exist there — and the box image ships no rsync, so synced folders are off and scp over vagrant's own ssh config is the one delivery path that needs no provider magic |
| D3 | `VAGRANT_CWD` and `VAGRANT_HOME` are pinned to `vagrant/.gen/.vagrant.d` by the app wrappers | no state leaks into `~/.vagrant.d`; `.gen/` is gitignored so boxes/keys/disks live in one disposable dir |
| D4 | Box: `cloud-image/ubuntu-24.04` at `config.vm.box_version = "20260926.0.0"`, pin log in `vagrant/README.md` | the official Ubuntu cloud-image team's libvirt box — the plan doc's `ubuntu/noble64` guess does not exist in the Vagrant Cloud catalog. Vagrant Cloud publishes no checksum for this box, so the pin is the version number |
| D5 | The deployment matrix (`XTCP2_MODE`, `XTCP2_AUTH`, `XTCP2_CAPS`, `XTCP2_ATTACKER`) is read by the **Vagrantfile from ENV** and passed to provisioners as arguments | one `vagrant up` shape; scenarios set the env (or default it), `vagrant provision` re-applies cheaply, and `XTCP2_MODE=bridge XTCP2_AUTH=raw nix run .#redteam-rt-1-1` satisfies the DoD matrix-flip checkbox |
| D6 | Catcher = host-side `socat TCP-LISTEN:9999,reuseaddr,fork OPEN:/var/lib/xtcp2-catcher/records.jsonl,append` on the victim VM (systemd unit), NOT a container | survives attacker-container actions (the RT-11 point is that attacker containers must not be able to kill delivery); the assertion greps per-netns records and counts line growth across the poll cadence |
| D7 | Work population = 2 `oci-xtcp2-tcp-stress` containers (`TCP_MODE=both TCP_COUNT=25`), plus the base-OS sockets the VM itself accrues | both non-attacker netns and base-OS coverage, per the RT-11 victim-population contract |
| D8 | Attacker VM tools via apt (`socat jq curl tcpdump strace iperf3 nghttp2-client` for h2load, `slowhttptest` — universe); repo static binaries (`xtcp2client`, `xtcp2ctl`) shipped as a tools tarball the derivation static-asserts | no nix closure is dragged into the VM. Verified: the repo binaries are static ELF, but nixpkgs' grpcurl is **dynamic** — it is NOT in the Phase 0 tar; the gRPC scenarios (Phase 1) get a CGO_ENABLED=0 grpcurl build of their own. `ghz` is **not** in Ubuntu apt — same treatment as grpcurl, or the gRPC storms stay on h2load |
| D9 | `redteam-oci` / `redteam-oci-tcp-stress` / `redteam-attacker-tools` are **packages** (deterministic `gzip -n` output, cacheable); the app wrappers invoke `nix build` at run time | tarballs are pure functions of already-cached derivations, so `nix build .#redteam-oci` is fast and the DoD checkbox "`nix build .#redteam-oci` produces a loadable tarball" is a real build target |
| D10 | `checks.redteam-lint` = shellcheck over `vagrant/**/*.sh` (+ `bash -n`) — hermetic, no network, the only red-team CI gate | VM scenarios need KVM + network, so they are apps, not checks; `nix-fmt`/`statix` checks already cover the new `.nix` files automatically via `src` |

## The wiring map

Three touched files, one new nix file, one new tree. Exact insertion points:

**1. `nix/redteam.nix` (new)** — see [spec below](#nixredteamnix-specification).

**2. `nix/default.nix`** — import beside the other builders (after
`linkmonitorPerformance`, ~line 300):

```nix
redteam = import ./redteam.nix {
  inherit pkgs lib src;
  inherit (binaries) xtcp2client xtcp2ctl;
  inherit (containers) oci-xtcp2 oci-xtcp2-tcp-stress;
};
```

then merge the namespaces at the existing aggregation points, following the
`// linkmonitorPerformance.apps` precedent at the end of `apps`:

- `packages = ... // redteam.packages;`
- `apps = { ... } // linkmonitorPerformance.apps // (lintTiers ...) // redteam.apps;`
- `checks = ... // { redteam-lint = redteam.checks.redteam-lint; };`

`src` (the full tree), not `goSource` — Go-only filtering must not strip the
`vagrant/` tree out of the store copy that shellchecks it.

**3. `nix/packages.nix`** — `devTools` += `vagrant` (one line; brings the
plugin). Dev shell users then have `vagrant` on PATH inside `nix develop`.

**4. `Makefile`** — three thin wrappers after `protos:` (same style as the
existing `nix run .#regen-protos` targets):

```make
redteam-lint:
	nix build .#checks.x86_64-linux.redteam-lint -L

redteam-smoke:
	nix run .#redteam-rt-1-1

redteam-destroy:
	nix run .#redteam-destroy
```

**5. Docs** — `nix/README.md` gets a "Red-team harness" row in the
workflow table; the dev shell `xtcp2-help` banner gets three lines;
`docs/redteam-implementation.md` gets the two amendments from the inventory
section; `docs/README.md` links this doc.

## nix/redteam.nix specification

```nix
# nix/redteam.nix
#
# Red-team harness wiring (docs/redteam-plan.md, docs/redteam-implementation.md).
# Runners execute from the repo root on the harness host (l); they need KVM,
# libvirtd, and the network (vagrant box downloads). checks.redteam-lint is the
# only hermetic member.
{
  pkgs,
  lib,
  src,              # full tree — Go-source filtering must not strip vagrant/
  xtcp2client,      # binaries.xtcp2client (static)
  xtcp2ctl,         # binaries.xtcp2ctl     (static)
  oci-xtcp2,        # containers.oci-xtcp2        (stream script)
  oci-xtcp2-tcp-stress, # containers.oci-xtcp2-tcp-stress (stream script)
}:
```

Attrs it returns:

### packages

| attr | derivation | output |
|---|---|---|
| `redteam-oci` | `runCommand`: `${oci-xtcp2} \| gzip -n > $out` | single file `xtcp2-oci.tar.gz`, loadable by `docker load` (DoD 1) |
| `redteam-oci-tcp-stress` | same for `oci-xtcp2-tcp-stress` | `xtcp2-tcp-stress.tar.gz` |
| `redteam-attacker-tools` | `runCommand`: linkFarm of `xtcp2client`, `xtcp2ctl`, `versions.grpcurl` → tar; **asserts each binary is static** (`file` says "statically linked") before tarring | `xtcp2-attacker-tools.tar` — unpacks to `/opt/xtcp2-redteam/bin/` on the attacker VM and in the co-tenant container |

The static assertion is a hard failure, not a warning: a dynamically-linked
binary shipped to Ubuntu would fail at runtime with a missing `/nix/store`
loader — the exact class of bug the derivation should catch at build time.

### apps

Generated per scenario + fixed helpers. Discovery pattern:

```nix
scenarioApps = lib.listToAttrs (
  map
    (file: let
      id = builtins.head (lib.splitString "-" (builtins.baseNameOf file));
      # rt-1-1-unauth-scrape.sh -> redteam-rt-1-1
    in lib.nameValuePair "redteam-rt-…" {
      type = "app";
      program = "${mkScenarioRunner file}/bin/redteam-rt-…";
    })
    (lib.filesystem.listFilesRecursive "${src}/vagrant/scenarios")
);
```

`mkScenarioRunner` is a `writeShellApplication` that, per D1/D2/D3:

1. requires repo root (`[ -f flake.nix ]`) — the repo's runner convention;
2. `export VAGRANT_CWD="$PWD/vagrant" VAGRANT_HOME="$PWD/vagrant/.gen/.vagrant.d"`;
3. materializes tarballs if missing or stale (`nix build --no-link --print-out-paths .#redteam-oci .#redteam-oci-tcp-stress .#redteam-attacker-tools` → `gzip`-copy into `vagrant/.gen/`) — idempotent, so a second run reuses the same files;
4. exports `XTCP2_OCI_TAR=…`, `XTCP2_TCP_STRESS_TAR=…`, `XTCP2_ATTACKER_TOOLS_TAR=…` (baked-in store paths, not run-time `nix build` output);
5. `exec bash vagrant/scenarios/<file>`.

Fixed helpers: `redteam-destroy` (`vagrant destroy -f` both VMs + prune
`.gen/`), `redteam-status` (`vagrant status` + docker ps via `vagrant ssh`),
`redteam-matrix` (Phase 1: loops scenario × `XTCP2_MODE` × `XTCP2_AUTH`
rows, one TAP stream per cell), `redteam-lint` (the same shellcheck as the
check, for local iteration).

### checks

`redteam-lint` — `runCommand` with `pkgs.shellcheck` in `nativeBuildInputs`:

```sh
find "$src/vagrant" -name '*.sh' -print0 |
  xargs -0 shellcheck -x   # -x: follow external sources (lib/*.sh sourcing)
find "$src/vagrant" -name '*.sh' -print0 |
  xargs -0 -n1 bash -n
```

Both must pass; `-x` matters because `scenarios/*.sh` source `../lib/*.sh`.

## vagrant/ tree specification

```text
vagrant/
  Vagrantfile
  README.md          prerequisites, box pin + version, cold-boot time, teardown
  .gitignore         .gen/  redteam/evidence/
  provision/
    common.sh        apt-get update, docker-ce from Docker's apt repo (noble),
                     chrony, jq socat tcpdump strace iperf3 nghttp2-client
    victim.sh        docker load the three uploads; run catcher (systemd socat
                     unit, port 9999, /var/lib/xtcp2-catcher/records.jsonl);
                     xtcp2 daemon container per matrix env; work population
                     (2 × TCP_MODE=both TCP_COUNT=25); attacker tier container
    attacker.sh      unpack attacker-tools tar to /opt/xtcp2-redteam/bin
  lib/
    assert.sh        ok/not_ok (TAP), done_testing, wait_for <cmd> <timeout>,
                     expect_outcome <actual> <expected>
    matrix.sh        XTCP2_MODE/XTCP2_AUTH/XTCP2_CAPS/XTCP2_ATTACKER validation
                     + docker run argument factories
    evidence.sh      run-id dirs (date+git sha), capture(), redact tokens
  scenarios/
    rt-1-1-unauth-scrape.sh
  redteam/
    findings.md      seed table
    evidence/        gitignored run outputs
```

**Vagrantfile** (two VMs to start; the Phase 1 router VM is a third
definition behind `REDTEAM_ROUTER=1`):

- `config.vm.box = "cloud-image/ubuntu-24.04"`, `config.vm.box_version = "20260926.0.0"` (pin log in vagrant/README.md);
- `victim`: 6 GB RAM, 4 vCPU; `attacker`: 2 GB, 2 vCPU; provider
  `libvirt` (`driver = "kvm"`, `storage_pool = "default"`);
- private network `xtcp2-redteam-mgmt` (e.g. `192.168.142.0/24`, DHCP) for the
  attacker↔victim vantage — distinct from libvirt's default net so the DoD
  "no networks beyond the defined ones" assertion is `virsh net-list`-checkable;
- `victim` provisioning: `common.sh` → upload `.gen/*.tar.gz` → `victim.sh
  <MODE> <AUTH> <CAPS> <ATTACKER>` (matrix as argv, validated by
  `lib/matrix.sh` — env → argv translation happens in the Vagrantfile so
  `vagrant provision` alone re-applies a matrix change);
- mounts: the daemon container gets `-v /var/lib/xtcp2:/var/lib/xtcp2`,
  `-v /run/netns:/run/netns:ro,rshared -v /run/docker/netns:/run/docker/netns:ro,rshared`
  — matching TODO-SOON §29 and the RT-11 poison/drop scenario shapes.

**lib/assert.sh** — the TAP contract from the implementation doc, verbatim:
`ok`/`not ok` lines, one `RESULT rt-N-M <OUTCOME> [severity] [evidence]`
summary, `exit 0` iff actual == `EXPECT` and zero `not ok`.

**Scenario skeleton** (contract from the implementation doc, "Conventions
every phase follows" §2): `set -euo pipefail`, `EXPECT=` declared at the top,
`trap cleanup EXIT`, idempotent (`vagrant provision` safe), token generation
via `openssl rand` into the evidence dir only.

## The rt-1-1 walkthrough

The first scenario end-to-end (task 0.7):

1. **Preflight** (fail fast): repo root, `vagrant` on PATH, `virsh -c
   qemu:///system version` succeeds, tarballs materialized (the app wrapper
   did this), `RESULT` dir created.
2. **Boot**: `vagrant up victim attacker` (cold: box download + provision,
   target ≤ 15 min; warm: `vagrant provision` path).
3. **Population check** (pre-attack baseline): catcher file has ≥1 line per
   work netns within 2 × poll cadence — this is also the "victim still
   reporting" helper the RT-11 invariant reuses after every attack.
4. **Attack**: plain-tier co-tenant container on the victim's dockerd:
   `docker run -d --name redteam-attacker --network bridge ubuntu:24.04 sleep infinity`,
   then `docker exec redteam-attacker bash -c 'apt-get update -qq && apt-get
   install -y -qq curl && curl -sS -m 5 http://172.17.0.1:9088/metrics'`
   (host-mode daemon bound on the docker0 gateway). Evidence: full scrape body.
5. **Assertions** (TAP): HTTP 200 from the co-tenant vantage; body contains
   a record-bearing metric with the victim netns label (not the attacker's);
   catcher keeps growing for one more cadence (invariant).
6. **Verdict**: outcome CONFIRMED == EXPECT → `RESULT rt-1-1 CONFIRMED …`,
   exit 0. The DoD matrix flip is then proven by rerunning with
   `XTCP2_AUTH=raw` (401 from the co-tenant → BLOCKED == new EXPECT).
7. **Cleanup**: kill the attacker container; `vagrant destroy -f` unless
   `REDTEAM_KEEP=1` (debugging).

## Build sequence

One PR (or two, harness/first-scenario, per the implementation doc's
convention 1). Commits in reviewable order — each is independently verifiable:

| # | Commit | Implements | Verification |
|---|---|---|---|
| C1 | `nix/redteam.nix` packages + wiring into `nix/default.nix`, `nix/packages.nix` | tasks 0.3 (packages half), 0.8 (lint) | `nix build .#redteam-oci .#redteam-oci-tcp-stress .#redteam-attacker-tools`; `nix flake check` still green |
| C2 | `checks.redteam-lint` + Makefile targets + `nix/README.md`/help-banner rows | tasks 0.3 (apps/checks half), 0.8 | `nix build .#checks.x86_64-linux.redteam-lint -L`; `make redteam-lint` |
| C3 | `vagrant/` skeleton: Vagrantfile, `provision/*.sh`, `.gitignore`, README | tasks 0.1, 0.2 | `vagrant up victim attacker` cold boots; `docker ps` shows daemon + population + catcher; daemon reachable at 9088 from the VM |
| C4 | `lib/` helpers + `redteam/findings.md` seed | tasks 0.4, 0.8 | `redteam-lint` shellchecks the new files green |
| C5 | matrix wiring (`XTCP2_AUTH=raw` token files, `-auth` flags) + population/catcher systemd units | task 0.5, 0.6 | `XTCP2_MODE=bridge XTCP2_AUTH=raw vagrant provision` → daemon 401s anonymous scrapes |
| C6 | scenario `rt-1-1` + app + docs amendments | task 0.7, 0.9 | the full Phase 0 DoD list, including the cold-boot `nix run .#redteam-rt-1-1` and the `virsh net-list` leak assertion |

This maps onto the implementation plan's Phase 0 DoD checkboxes one-to-one;
C6 is the PR exit gate. Findings.md row for rt-1-1 lands with C6 (the
scenario must run before its row is truthful).

## Open questions

1. **Box choice** — resolved during the build: the catalog has no
   `ubuntu/noble64`; the official Ubuntu cloud-image team's
   `cloud-image/ubuntu-24.04` (libvirt provider) is the box, pinned at
   `20260926.0.0` with the pin log in `vagrant/README.md`. No NixOS-style box
   is considered — the victim must be **Ubuntu** (TODO-SOON §29 is the entire
   reason this harness exists alongside the NixOS microVM checks).
2. **slowhttptest via apt (universe)** vs `universe` enablement on noble —
   verify `apt-get install slowhttptest` works in C3; if not, ship it in the
   attacker-tools tarball (verify static like the repo binaries).
3. **`ghz` static linkage** — Phase 1 (rt-4-3 needs it); if nixpkgs ghz is
   dynamically linked, h2load (apt) covers the storm shape and ghz is dropped.
4. **Memory budget on `l`** — 8 GB (victim) + 2 GB (attacker) is fine for
   Phase 0; Phase 4's FD/conntrack knees may raise the victim to 10–12 GB.
   Note it in `vagrant/README.md` when C3 lands.
