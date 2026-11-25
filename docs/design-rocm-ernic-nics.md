# rocm-ernic NICs in xtcp2's VMs

How xtcp2 uses [rocm-ernic](https://github.com/ROCm/rocm-ernic), the userspace emulated AMD Pensando ionic NIC, and the plan to pin it to upstream and give the test VMs ionic NICs alongside their existing virtio and e1000e NICs.

**Status (2026-11-25):** phases 0–4 are implemented on one branch; see [Status](#status) for what the phases found.

The audience is xtcp2 contributors and the rocm-ernic maintainers: the section "What xtcp2 needs from rocm-ernic" lists the features we depend on, so a change there can be checked against a real consumer.

## Why an emulated ionic NIC

xtcp2 and go-link-monitor read NIC state through the kernel: ethtool netlink (driver, firmware version, bus address, link speed), sysfs, rtnetlink statistics and RDMA netlink/verbs. QEMU's virtio-net and e1000e exercise only part of that. A real datacenter driver has a different shape: many queues, per-queue and port statistics, RDMA devices and counters, firmware strings, 100G+ link speeds.

rocm-ernic serves a PCI device (`1dd8:1002`, subsystem `1dd8:5400`) over vfio-user, so the guest binds the **unmodified in-tree `ionic` and `ionic_rdma` drivers**. Every reply xtcp2 parses then comes from production kernel code, not a test double. No hardware is needed, and it runs in CI and in the nix build sandbox with KVM.

## What xtcp2 uses today

| Consumer | File | What ernic provides |
|---|---|---|
| `checks.vm-linkmonitor-ionic` / `test-linkmonitor-ionic-vm` | `nix/microvms/linkmonitor-ionic.nix` | ionic netdev + ionic_rdma: link-monitor counters checked against sysfs and ethtool; hot-unplug as guest PCI remove/rescan; the go-native RDMA corpus (`--capture`) |
| `checks.vm-linkmonitor-ionic-faults` | same, `faults = true` | `generic-100g` profile (16 queues), `--inject` eth_rx/eth_tx/stats_vals faults, `--tap` peer in a user+net namespace: counters must match the fault pattern frame for frame |
| `demo-linkmonitor-ionic-profiles` | `nix/microvms/linkmonitor-ionic-demo.nix` | one boot per NIC profile, RDMA ping-pong, `ernic-rdma-errors` |

All of them use `nixosModules.ernic-guest` (a `qemu-vm.nix` guest on q35) and `packages.ernic-run`, wired in `nix/default.nix` (`ernicModule`, `ernicRun`, twice).

The **golden-record VM** (`nix/microvms/mkVm.nix`, flavor `s3parquet-golden`, a microvm.nix guest on the `pc` machine) does not use ernic. Its NICs are virtio `eth0` (SLiRP, speed forced to 10000) and an e1000e uplink 2 (`goldenNicArgs`, restricted SLiRP, default route at metric 2048).

### The pin today

`flake.nix` pins `github:randomizedcoder/rocm-ernic/fe6cf85` (narHash `sha256-2jVx7zJF…`), a fork integration branch: develop plus ROCm/rocm-ernic#219, #221–#224 and four fork branches (eth faults, RDMA error stats, port stats, ernic-run server options).

Checked on 2026-11-24 against upstream `develop` at `bd902345` (#600): every one of those features is upstream (`nixosModules.ernic-guest`, `ernic-run` with `--nic-profile/--inject/--tap/--eth-queues`, NIC profiles, eth and stats fault sites, LIF and port statistics, `ernic-rdma-errors`), merged under different commit ids. **Nothing still needs the fork.**

## Goals

1. Pin to upstream `ROCm/rocm-ernic` by rev and narHash, tracked for drift like the other upstream pins.
2. One modular nix layer for ernic NICs, used by every VM: describe NICs as data, get the QEMU args, the host servers and the guest network config from it.
3. VMs keep their virtio and e1000e NICs and **add** ionic NICs: one VM, several NIC types, no extra VM per driver.
4. The golden records get a real datacenter driver in their uplink columns.

Non-goals: RDMA in the golden VM (xtcp2 records TCP sockets; RDMA stays with the link-monitor VMs); multi-node ernic (the TCP backend).

## Design

### 1. Pin (`flake.nix`, `nix/upstream-pins.json`)

```nix
# rocm-ernic emulates an AMD Pensando ionic NIC over vfio-user (see
# docs/design-rocm-ernic-nics.md). Pinned to upstream develop by rev and
# narHash; `nix run .#check-upstream-pins` reports drift.
rocm-ernic = {
  type = "github";
  owner = "ROCm";
  repo = "rocm-ernic";
  rev = "bd902345b0f8f3863f535a0a09da9f21ce744737";
  narHash = "sha256-vwIFQjGK7rorBfCNwyq4kepVz68A4BJHag2X3y/T+xU=";
  inputs.nixpkgs.follows = "nixpkgs";
  inputs.flake-utils.follows = "flake-utils";
  inputs.microvm.follows = "microvm";
};
```

- `narHash` in `flake.nix` (not only in `flake.lock`) makes the source content part of the declaration: a lock update that fetched different bytes for the same rev fails, and a reviewer sees the hash change in the diff.
- upstream's `libvfio-user` input is left to its own lock: rocm-ernic is tested against that rev.
- `nix/upstream-pins.json` gains a `rocm-ernic` entry (`repo ROCm/rocm-ernic`, `branch develop`, `rev`, `declared_in`, `verify_via flake.lock nodes.rocm-ernic.locked.rev`, `consumed_by`). `check-upstream-pins` then reports how far develop has moved.
- Local iteration stays one flag: `--override-input rocm-ernic path:$HOME/Downloads/rocm-ernic`.

### 2. One ernic attrset (`nix/ernic/default.nix`, new)

Today `nix/default.nix` reaches into `rocmErnic.packages.${system}` in two places. Replace that with one import that every consumer takes as `ernic`:

```nix
ernic = import ./ernic { inherit pkgs lib rocmErnic; };
# {
#   server      = rocm-ernic package        (the vfio-user server)
#   run         = ernic-run                  (one server around a command)
#   runMany     = ernic-run-many             (N servers, see 4)
#   rdmaErrors  = ernic-rdma-errors
#   guestModule = nixosModules.ernic-guest   (qemu-vm.nix guests, unchanged)
#   nics        = import ./nics.nix          (the NIC-list functions, see 3)
#   profiles    = "${rocmErnic}/profiles"    (generic-100g.ini, ...)
# }
```

Only `x86_64-linux` evaluates it (`lib.optionalAttrs`), as today. Packages are built once and shared by every VM and check through the store, so adding NICs to more VMs adds no build cost.

### 3. NICs as data (`nix/ernic/nics.nix`)

A VM declares its ionic NICs as a list:

```nix
ernicNics = [
  {
    name = "ionic0";            # guest ifname (systemd .link by MAC)
    mac = "02:00:00:00:20:01";  # fixed, so the fixture is stable
    pciAddr = "0x0a";           # fixed slot, so bus_info is stable
    profiles = [ "generic-100g" ];  # --nic-profile, may repeat
    tap = null;                 # or { ifname; host; prefix; } for a peer
    inject = [ ];               # --inject specs
    network = { address = "10.0.4.15/24"; gateway = "10.0.4.1"; metric = 512; };
  }
];
```

Pure functions turn the list into the three pieces a VM needs:

| function | output | consumed by |
|---|---|---|
| `qemuArgs nics` | `-device vfio-user-pci,id=…,addr=…,socket.type=unix,socket.path=ernic-<name>.sock` per NIC | microvm.nix `qemu.extraArgs` (golden VM) |
| `serverArgs nic` | `--mac … --nic-profile … [--tap …] [--inject …]` | `runMany` |
| `guestConfig nics` | `boot.kernelModules = [ "ionic" ]`, a systemd `.link` (MAC → name) and `.network` per NIC | the guest's NixOS modules |

Socket paths are relative (`ernic-<name>.sock`) because microvm.nix shell-escapes `extraArgs`, so `$ERNIC_SOCKET` cannot expand there. The launcher runs QEMU from its run directory, where `runMany` created the sockets.

### 4. Several servers (`ernic-run-many`)

One rocm-ernic server serves one device. `ernic-run-many` starts one server per NIC, waits for every socket, runs the command, and stops all servers. It fails if any server exits early, with the same contract as upstream `ernic-run`. It is a thin loop over upstream `ernic-run`'s logic, not a fork of the server.

TAPs (when a NIC has `tap`) are created in one `unshare --user --map-root-user --net` namespace around all servers, as `linkmonitor-ionic.nix` does today. That needs no host root and no host network changes.

### 5. Shared memory and the `pc` machine

vfio-user needs guest RAM shared with the server (`share=on`). The golden VM already has virtiofs shares, so microvm.nix adds `memory-backend-memfd,share=on` (`lib/runners/qemu.nix:282`): no change. `vfio-user-pci` is a plain PCI device and is expected to work on `pc` (i440fx); phase 0 verifies that. If it does not, the golden flavor moves to q35, which also changes every NIC's bus_info, so it is done in the same recapture.

### 6. The golden VM's NICs

| NIC | driver | role | default route metric | columns |
|---|---|---|---|---|
| eth0 | virtio-net | SLiRP management | 4096 | – |
| ionic0 | ionic (ernic, `nix/ernic/profiles/golden-uplink.ini`) | uplink 1 | 512 | `uplink1_nic_*`: driver ionic, vendor 7640 (`0x1dd8`), device 4098 (`0x1002`), speed 100000, fw_version (scrubbed as host identity; the VM checks `xtcp2-golden-1.0`), bus_info |
| eth1 | e1000e | uplink 2 (unchanged) | 2048 | `uplink2_nic_*` unchanged |
| ionic1 | ionic (same profile) | phase 4 peer, moved into netns `golden-ernic-b` | – | – |

ionic0 needs no TAP to be an uplink: `DefaultUplinks` reads routes and ethtool, not traffic. The ionic driver loads without `ionic_rdma` (no RDMA, so the 4-vCPU EQ minimum does not apply). A profile with `eth = 2` queues matches the golden VM's 2 vCPUs.

**Decision D1 (decided):** ionic0 is uplink 1 and e1000e stays uplink 2. eth0 keeps a DHCP default route at metric 4096, so it ranks third and `DefaultUplinks(2)` leaves it out. The fixture then holds a datacenter driver and e1000e, and virtio (not a fleet NIC) leaves the uplink columns. xtcp2 records two uplinks, so three NICs cannot all be uplinks.

### 7. Loss rows without netem (phase 4, exploratory)

The golden workload makes its loss rows with `tc netem loss 2% reorder 1%`, which is random, so twelve columns are exempt from the strict diff (`pkg/goldenrec/diff.go`, `lossyColumns`). ernic's `--inject "eth_tx: every N effect drop"` drops a deterministic frame sequence. A bulk connection routed through an ionic NIC with a TAP peer could then produce the same retransmit, DSACK and RTO counters on every capture, and those columns could return to the strict diff. This is a measurement first: five captures, as for `lossyColumns`, and only columns that come out identical move.

**Result:** the drops are exact at the frame level (ionic0's `tx_dropped` equals `(tx_packets + tx_dropped) / 50` in every capture), but the TCP counters are not stable. How data and ACKs interleave depends on timing, so which segments are every 50th frame changes, and so do the frame totals (14064–14542 across runs). No column moved out of `lossyColumns`. Instead the ionic rows are "fault rows" (`Options.FaultNetns`/`FaultEgress` in `pkg/goldenrec/diff.go`): they skip the `lossyColumns` plus the servers' `total_retrans` and `bytes_retrans`, and the VM checks the clients' retransmits (`ernic_loss`) and the exact drop count (`fault_count`).

### 8. Other VMs

- The link-monitor ionic checks keep `nixosModules.ernic-guest` (q35, RDMA) and move to `ernic.*` from section 2. Their behavior is unchanged by the re-pin; `checks.vm-all` proves that.
- The link-monitor faults VM can add an e1000e beside ionic0 at no cost, so one boot checks that the monitor tells two drivers apart.
- `capture-netlink-fixtures` can record ethtool replies from the ionic driver (`ETHTOOL_MSG_LINKMODES`, `DRVINFO`, ring and channel counts with 16 queues). These are replies `pkg/nicinfo` parses and the fixtures have only from virtio/e1000e.

## What xtcp2 needs from rocm-ernic

These are the interfaces xtcp2 depends on. A change to one of them breaks a check here:

- `nixosModules.ernic-guest` options `virtualisation.ernic.{enable,socket,deviceId,rootPort}` and the `ernic-rdma-up` service.
- `packages.<system>.ernic-run`, its options (`--socket --tap --nic-profile --inject --eth-queues --log-file --stats-file`), its exit-status contract and `ERNIC_SOCKET`.
- `packages.<system>.ernic-rdma-errors`.
- The `profiles/*.ini` names and keys `[identity] fw_version`, `[link] speed_mbps`, `[queues] eth`.
- The `--inject` grammar for `eth_rx`, `eth_tx` and `stats_vals`.
- The PCI identity `1dd8:1002` / `1dd8:5400`, and ethtool reporting the profile's link speed (upstream #215).

Upstream changes that would let xtcp2 drop local code, proposed as small PRs once this plan lands:

1. **Several devices in `ernic-guest`:** a `virtualisation.ernic.devices` list (socket, id, root port, chassis/slot per device) instead of one device with `chassis=1,slot=1` fixed; `ernic-rdma-up` handling every ionic netdev, not the first.
2. **Several servers in `ernic-run`:** repeatable `--device NAME:SOCKET [opts]`, exporting `ERNIC_SOCKET_<NAME>`, which would replace `ernic-run-many`.
3. **microvm.nix guests:** a documented `qemuArgs` helper with a literal socket path, and a note that `pc`/i440fx works (or does not).
4. **Link without a peer:** document the carrier state of an ionic netdev with no `--tap`.

## Phases (one PR for 0–4, off `origin/main`)

| # | PR | Content | Proof |
|---|---|---|---|
| 0 | spike, no PR | Boot the golden VM once with one ionic NIC on `pc`, no TAP: does it bind, is the link up, does ethtool show speed and fw_version | notes in this doc |
| 1 | `build/rocm-ernic-upstream` | Pin per section 1; `nix/ernic` attrset; consumers moved; `upstream-pins.json` entry | `checks.vm-all` passes (ionic, ionic-faults, rdma); `check-upstream-pins` clean |
| 2 | `feat/ernic-nics-lib` | `nics.nix`, `ernic-run-many`, table tests | `checks.ernic-nics` |
| 3 | `feat/golden-ionic-uplink` | Golden VM NICs per section 6, recapture, `coverage.json` (uplink1 ionic) | coverage gate red then green; two captures diff clean |
| 4 | `feat/golden-ernic-loss` | Section 7: deterministic loss rows, `lossyColumns` reduced to what still varies | five captures; per-column evidence |
| 5 | upstream PRs | The four upstream changes above, on the fork first | rocm-ernic's own CI |

## Tests

Every table follows the repo convention: `name`, `category` (positive, negative, boundary, corner), `description`, `input`, `expected`; red first.

### T1 `checks.ernic-nics` (`nix/ernic/nics-test.nix`, `lib.runTests` over the pure functions)

| name | category | description | input | expected |
|---|---|---|---|---|
| one_nic | positive | one NIC gives one device | `[ionic0]` | one `-device vfio-user-pci` with `id=ionic0`, `addr=0x0a`, `socket.path=ernic-ionic0.sock` |
| server_args | positive | profile, MAC and faults reach the server | ionic0 with `inject` | `--mac … --nic-profile generic-100g --inject …` |
| duplicate_name | negative | names must be unique | two `ionic0` | assertion `duplicate ernic NIC name ionic0` |
| duplicate_addr | negative | PCI slots must be unique | two NICs at `0x0a` | assertion naming both NICs |
| empty | boundary | no NICs adds nothing | `[]` | `[]` args, no kernel modules |
| two_nics | boundary | order is kept | `[ionic0 ionic1]` | devices in list order, two sockets |
| no_tap | corner | a NIC without a peer has no `--tap` | `tap = null` | no `--tap` arg, no netns step |
| shell_chars | corner | a name that needs quoting is rejected | `name = "a b"` | assertion `invalid ernic NIC name` |

### T2 `ernic-run-many` (`nix/ernic/run-many-test.nix`, a fake server as upstream's `nix/tests/fake_ernic.py` does)

| name | category | description | input | expected |
|---|---|---|---|---|
| all_up | positive | every socket exists before the command | 2 NICs | the command sees both sockets; exit 0 |
| exit_status | positive | the command's status is returned | command exits 3 | exit 3, servers stopped |
| server_dies | negative | an early server exit fails the run | fake exits before its socket | non-zero, `ionic1 exited before creating its socket` |
| socket_exists | negative | a stale socket is refused | `ernic-ionic0.sock` exists | non-zero, `already exists` |
| zero_nics | boundary | no NICs just runs the command | `[]` | the command runs, exit 0 |
| sigterm | corner | TERM stops every server | TERM during the command | no server left running |

### T3 golden capture rows (`golden-capture.sh`, phase 3)

| name | category | description | input | expected |
|---|---|---|---|---|
| uplink1_ionic | positive | the ionic NIC is uplink 1 | ionic0 | `uplink1_nic_driver` ionic, vendor 7640, device 4098 |
| uplink2_kept | positive | e1000e stays uplink 2 | eth1 | `uplink2_nic_driver` e1000e |
| no_virtio_uplink | negative | eth0 has no default route | eth0 | virtio_net in no uplink column |
| speed_100g | boundary | the profile's speed reaches ethtool | generic-100g | `uplink1_nic_speed_mbps` 100000 |
| fw_from_profile | corner | firmware string from `[identity]` | the profile's fw_version | `uplink1_nic_fw_version` equal to it |
| bus_stable | corner | fixed slot gives fixed bus_info | two captures | same `uplink1_nic_bus_info`; diff exit 0 |

## Risks

- **`pc` machine:** if vfio-user-pci does not work on i440fx, the golden flavor moves to q35 (section 5).
- **Host processes:** each ionic NIC is one more host process. A capture with two NICs is two servers. `runMany` stops them on every exit path, and T2 `sigterm` covers that.
- **Sandbox:** the cached checks run ernic inside the nix build sandbox with KVM, as `checks.vm-linkmonitor-ionic` already does; TAP NICs need `/dev/net/tun` in `extra-sandbox-paths` (already the case on the build host; `VM_CHECK_NO_TUN` skips them elsewhere).
- **Upstream drift:** develop moves quickly (it reached #600 by 2026-11-17). The narHash pin means we move deliberately, and phase 1's `vm-all` run is the gate for each bump.

## Status

Phases 0–4 are on one branch, each test committed red before its implementation.

### Phase 0 findings

- `vfio-user-pci` works on the `pc` (i440fx) machine with microvm.nix's `memory-backend-memfd,share=on`; the golden flavor stays on `pc`.
- A relative `socket.path` resolves in QEMU's working directory, so `ernic-run-many --dir "$STAGE/ernic"` starting `microvm-run` there works.
- `ionic` binds on 2 vCPUs with `[queues] eth = 2`. `ionic_rdma` probes and fails (`Failed to register ibdev`, -EINVAL), harmlessly: the Ethernet device is unaffected and the golden VM wants no RDMA.
- ethtool reports the profile's 100000 Mb/s and `fw_version` `xtcp2-golden-1.0`; the carrier is up with a TAP on the fabric bridge.

### Phase 3 and 4 findings

- **Two NICs, one fabric:** `ernic-run-many` holds both servers, their TAPs and a bridge in one unprivileged user + network namespace; QEMU stays in the caller's namespace, so SLiRP and host ports work. No host root.
- **Noise on the fault counters:** before the transfers, ionic0 sent IGMP reports (systemd-resolved joins `224.0.0.252` for LLMNR on every link; fixed by `LLMNR=no`, `MulticastDNS=no` in `nics.nix`) and, in 3 of 5 captures, an NTP poll with the gateway ARP (ionic0 holds the default route; timesyncd is off in the golden VM). With both gone, ionic0 sends nothing before the transfers. One capture in five still saw one frame reach ionic1 from the fabric; its source is not identified, and `no_noise` reports but does not require ionic1's count.
- **The guest's `tx_packets` excludes injected drops:** the emulator's `eth_tx` events are `tx_packets + tx_dropped`.
- **Measurement:** five captures; with the fault-row rule, captures 3–5 diff clean pairwise.

### Open, outside this change

- **netem pair:** in some captures one bulk direction shows almost no loss (0, 2 or 10 retransmits of about 3500 segments, against about 70 expected at 2%), so `loss_seen` occasionally sees 3 of 4 clients with retransmits and `tcp_info_total_retrans` on the netem rows is not always stable. Seen before any ernic workload ran; `loss_seen` now prints each client's state, segments and bytes acked to find the cause.
- The four upstream rocm-ernic changes above (phase 5).
