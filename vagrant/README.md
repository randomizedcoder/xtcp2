# Red-team harness (Vagrant/libvirt)

The Ubuntu 24.04 + Docker adversary-simulation topology from
[docs/redteam-plan.md](../docs/redteam-plan.md). Every scenario is one
`nix run` from the repo root on `l` (the libvirt harness host):

```sh
nix run .#redteam-rt-1-1                                   # default row (host, auth off)
XTCP2_MODE=bridge XTCP2_AUTH=raw XTCP2_EXPECT=BLOCKED \
  nix run .#redteam-rt-1-1                                  # matrix flip (Phase 0 DoD)
nix run .#redteam-destroy                                   # tear both VMs down
nix build .#checks.x86_64-linux.redteam-lint -L             # the only CI member
```

WIP scenarios are only discoverable when the flake source includes them —
`nix run path:.#redteam-…` during development (same `path:.` rule as
focused-quality; see [../nix/README.md](../nix/README.md)).

## Topology

Two Ubuntu 24.04 guests on the `xtcp2-redteam-mgmt` libvirt network (the
harness never touches libvirt's default network):

- **victim** (6 GB, 4 vCPU) — the "prod host": dockerd + the xtcp2 daemon
  container in the deployment-matrix row, the RT-11 work population
  (`rt-work-1/2`, tcp_server/tcp_client containers), the catcher (a
  systemd socat unit appending the daemon's jsonl to
  `/var/lib/xtcp2-catcher/records.jsonl` — the "victim still reporting"
  evidence, deliberately host-side so a blinded daemon cannot forge it), and
  the co-tenant attacker container (`rt-attacker`).
- **attacker** (2 GB, 2 vCPU) — the host-side attacker vantage: static repo
  client binaries (`/opt/xtcp2-redteam/bin`) + slowhttptest/h2load.

The deployment matrix (`XTCP2_MODE`, `XTCP2_AUTH`, `XTCP2_CAPS`,
`XTCP2_ATTACKER`, `XTCP2_EXPECT`) is validated host-side
([lib/matrix.sh](lib/matrix.sh)), re-read by the Vagrantfile, and
re-validated in [provision/victim.sh](provision/victim.sh) — one `vagrant
provision` re-applies a row change.

## Box pin

`cloud-image/ubuntu-24.04` at `20260926.0.0` — the official Ubuntu
cloud-image team's libvirt box. Note: the Vagrant Cloud catalog has **no
`ubuntu/noble64`** (the name the plan doc originally guessed); this box is
the official 24.04 offering. Vagrant Cloud publishes no checksum for it
(`checksum_type: none`), so the pin is the version number; re-pin when
moving it forward and record the date here:

- `20260926.0.0` — pinned 2026-10-08 (current at pin time)

The box ships no rsync, so the Vagrantfile disables synced folders and the
scenario apps deliver files (image tarballs, attacker tools) via scp over
`vagrant ssh-config` ([lib/ssh.sh](lib/ssh.sh)) before `vagrant provision`.

## Prerequisites (all on `l`)

- KVM + `qemu:///system` libvirtd, user in the `libvirt` group
- nothing else — `nix run .#redteam-*` provides vagrant (the pinned
  nixpkgs bundles the vagrant-libvirt plugin on Linux), the tarballs, and
  the runtime; `nix develop` gets you bare `vagrant` the same way

## Cold boot, warm boot, teardown

- Cold (box download ~1 GB, then provision): ~15 min.
- Warm (`vagrant up --no-provision` + re-provision): ~3 min.
- The scenarios leave the VMs up for fast iteration; teardown:
  `nix run .#redteam-destroy` (leaves `vagrant/.gen/` — box cache — intact;
  remove it to force a cold boot).

## Memory budget

Phase 0 fits `l` easily (8 GB across both VMs). Phase 4's
connection/FD-knee scenarios raise the victim to 10–12 GB — a TODO before
those land, recorded in docs/redteam-implementation.md.
