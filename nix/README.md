# Nix development, builds, captures, and tests

Run commands from the repository root. [flake.nix](../flake.nix) exposes the
packages, checks, apps and development shell assembled by [default.nix](default.nix).
Tool versions come from [versions.nix](versions.nix) and the locked flake inputs.
The flake currently exports `x86_64-linux` only. See
[build flavors](../docs/build-flavors.md) for the binary/container matrix and
[integration testing](../docs/integration-testing.md) for guest internals.

## Choose a workflow

| Goal | Command from the repository root |
|---|---|
| Enter the pinned development environment | `nix develop` (then `xtcp2-help`) |
| Build the complete daemon | `nix build path:.#xtcp2` |
| Test netlink parsing | `nix build path:.#test-go-unit -L` |
| Test goip or enrichment changes | `nix run path:.#focused-quality` |
| Run the focused monitor gates | `nix build path:.#test-linkmonitor -L` |
| Run cached guest checks sequentially | `nix build path:.#checks.x86_64-linux.vm-all -L -j1` |
| Run the daemon lifecycle/parity sweep freshly | `nix run path:.#integration-all` |
| Capture netlink dump fixtures in a guest | `nix run path:.#microvm-x86_64-netlink-dump-capture -- --out /tmp/xtcp2-dumps` |
| Run all registered flake checks | `nix flake check path:. -L -j1` |
| Measure monitor performance freshly | `nix run path:.#bench-linkmonitor -- --output /tmp/xtcp2-bench` |

### Contents

- [Target names, execution, and caching](#target-names-execution-and-caching)
- [Development and maintenance](#development-and-maintenance)
- [Binaries, images, and artifacts](#binaries-images-and-artifacts)
- [Unit, flavor, and quality checks](#unit-flavor-and-quality-checks)
- [Nix integration testing](#nix-integration-testing)
- [Netlink and RDMA fixture collection](#netlink-and-rdma-fixture-collection)
- [Link-monitor verification](#link-monitor-verification)
- [Monitor performance measurements](#monitor-performance-measurements)
- [Guest tests as checks](#guest-tests-as-checks)
- [Module map and keeping this guide current](#module-map-and-keeping-this-guide-current)

## Target names, execution, and caching

Enable Nix's `nix-command` and `flakes` experimental features. Builds may fetch
locked dependencies and substitutes; the flake advertises the microVM binary
cache. `path:.` includes untracked files during development; `.` uses the Git
source view, including edits to tracked files but excluding untracked files.
If a running VM leaves a Unix socket in the checkout, `path:.` can fail with
"unsupported type" while copying the tree. Use `.` when all required source
files are tracked, or wait for the owning runner to clean up its runtime files.

| Output | How to use it | What happens |
|---|---|---|
| `packages.x86_64-linux.NAME` | `nix build path:.#NAME` | Builds a binary, image, runner, or test-result artifact; default symlink is `result` |
| `apps.x86_64-linux.NAME` | `nix run path:.#NAME -- ARGS` | Builds dependencies, then runs the app each time |
| Runnable package | `nix run path:.#NAME -- ARGS` | Falls back to the package executable when no matching app exists, e.g. `test-linkmonitor-embedding-vm` |
| `checks.x86_64-linux.NAME` | `nix build path:.#checks.x86_64-linux.NAME -L` | Runs the check during an uncached build; successful results are reusable |
| `devShells.x86_64-linux.default` | `nix develop` | Enters the pinned tool environment |
| `overlays.default` | Import into another Nix configuration | Adds `pkgs.xtcp2`, `pkgs.xtcp2-all`, and `pkgs.xtcp2-oci`; see [overlays.nix](overlays.nix) |

**A build is not necessarily a test run.** `test-go-unit` executes tests while
building; `test-microvm-lifecycle-x86_64` builds a launcher and guest closure.
Run that launcher with `nix run`, or build the corresponding registered check
where one exists. Likewise, building a `microvm-x86_64-*` package does not boot
the guest, and building `test-go-bench` does not execute benchmarks.

`nix flake check` runs the registered `checks`, not every package or app. It
includes KVM guest checks, all three lint tiers, and the large upstream
`proto-audit-netlink` dependency closure. It is broader than a focused test,
but is not a replacement for `integration-all` or fixture capture.

Use `-o result-NAME` to keep multiple outputs, `--no-link` when no symlink is
needed, `-L` for build logs, and `nix log path:.#NAME` for a derivation's log.
Successful checks are cached by their inputs; use the measurement/VM apps when
you need a fresh execution. Many Go/guest artifacts use filtered Go sources,
so documentation edits need not rebuild them.

Inspect exact exported names without building targets:

```sh
nix eval --json path:.#packages.x86_64-linux --apply builtins.attrNames
nix eval --json path:.#apps.x86_64-linux --apply builtins.attrNames
nix eval --json path:.#checks.x86_64-linux --apply builtins.attrNames
```

There is no public `tests` flake output and no exported `formatter`. Use the
`test-*` packages, explicit `checks` paths, or `nixfmt` from the dev shell.

## Development and maintenance

The shell provides Go, buf/protobuf generators, linters, audit tools, QEMU,
Expect, and supporting utilities; it sets `CGO_ENABLED=0`. Nix race-test targets
enable cgo where needed. [packages.nix](packages.nix) lists shell tools;
[versions.nix](versions.nix) selects versions and hashes.

Run these helpers with `nix run path:.#NAME -- ARGS`. The five lint helpers and
`regen-protos` are also available directly inside `nix develop`.

| Name | Purpose and effects |
|---|---|
| `lint-quick`, `lint`, `lint-comprehensive` | Run lint tiers 0, 1, and 2 against the working tree |
| `lint-new` | Lint changes since `HEAD~1` |
| `lint-fix` | Apply automatic lint fixes to the working tree |
| `lint-fix-one -- LINTER` | Apply fixes from one comprehensive-tier linter |
| `regen-protos` | Updates buf dependencies, lints/builds protos, regenerates code and schema copies; writes the checkout and needs network for module dependencies; generators are Nix-pinned local binaries |
| `quality-report` | Print the built quality report; `nix build path:.#quality-report` retains the artifact |
| `update-quality-report` | Refresh `docs/quality-report.md` in the checkout |
| `update-lint-baseline` | Regenerate `docs/lint-baseline.txt`; review changes as ratchet-policy changes |
| `focused-quality` | Build the four `test-focused-*` targets below; defaults to `path:.`, overridable with `XTCP2_FLAKE_REF` |
| `check-upstream-pins` | Query upstream revisions over the network; `--strict` enables strict reporting; the offline consistency check is `checks.x86_64-linux.upstream-pins` |
| `oci-size-report` | Measure streamed image sizes as CSV; `--default`/`--slim`, `--fat`, or `--all` |
| `coverage-merge` | Merge host and guest coverage: `--host HOST.out --vm-dir COVDIR [--vm-dir COVDIR] --out MERGED.out` |

`lint-baseline-measure` is a buildable JSON findings artifact, not a pass/fail
gate. `buf lint` is available in the shell, but `proto-lint` is not a registered
flake check because its module dependencies need network access.

Local source overrides keep the same workflow:

```sh
nix develop --override-input giouring path:/path/to/giouring
nix build path:.#checks.x86_64-linux.proto-audit-netlink \
  --override-input xdp2 path:/path/to/xdp2
nix run path:.#test-linkmonitor-ionic-vm \
  --override-input rocm-ernic path:/path/to/rocm-ernic
```

## Binaries, images, and artifacts

Use `nix build path:.#NAME` for these packages. Brace notation in tables denotes
a family of exact names; choose one expanded name, rather than passing braces
literally to Nix.

| Package or family | Contents |
|---|---|
| `default`, `xtcp2`, `xtcp2-debug`, `xtcp2-stripped` | Main daemon, with all destination/enrichment code; `default` aliases `xtcp2` |
| `xtcp2-all`, `xtcp2-all-debug`, `xtcp2-all-stripped` | Joins of the packaged command-line binaries |
| `xtcp2-{min,kafka,nats,nsq,valkey,s3parquet}` | Destination flavors; each also has `-asn`, `-locality`, and `-enrich` variants |
| `xtcp2-full-{none,asn,locality,enrich}` | All library destinations with the selected enrichment build tags |
| `xtcp2-cover` | Coverage-instrumented daemon for the guest coverage runs |
| `go-link-monitor`, `go-link-monitor-core` | Standalone monitor; the latter is an alias, both include native RDMA |
| `xtcp2client`, `xtcp2ctl`, `xtcp2_kafka_client` | Streaming/configuration clients and Kafka record consumer |
| `goip`, `goip-parity`, `gordma`, `rtnlmon-probe`, `ns` | Network inspection, parity, events, and namespace tools |
| `ipfeed-collector`, `ipmeta-bootstrap`, `zstd-probe` | IP metadata production, compact bootstrap artifacts, and compression investigation |
| `tcp_client`, `tcp_server`, `nsTest`, `discovery-bench` | Traffic, namespace churn, and discovery benchmark tools |
| `register_schema`, `kafka_to_clickhouse`, `clickhouse_protobuflist`, `clickhouse_protobuflist_db`, `clickhouse_http_insert_protobuflist` | Schema registration and ingestion tools |
| `oci-xtcp2`, `oci-xtcp2-debug`, `oci-xtcp2-stripped` | Full tool-suite OCI images |
| `oci-xtcp2-{min,kafka,nats,nsq,valkey,s3parquet}` | Slim daemon OCI images; each also has `-asn`, `-locality`, and `-enrich` variants |
| `oci-xtcp2client`, `oci-xtcp2ctl`, `oci-ipfeed-collector`, `oci-xtcp2-tcp-stress` | Standalone client, metadata collector, and load-generator images |
| `ipmeta-bootstrap-artifact` | Normalized lookup data from `ipmeta-bootstrap-lock.json`; a real source artifact/lock must be supplied |
| `oci-xtcp2{,-debug,-stripped}-bootstrap` and `oci-xtcp2-DEST-{asn,enrich}-bootstrap` | Bootstrap-enabled images; `DEST` is one of the six slim destination flavors |
| `xtcp-flat-record-desc` | Protobuf FileDescriptorSet for external consumers |
| `proto-audit` | Pinned upstream netlink layout oracle executable |

Enrichment build tags make code available; runtime flags still enable it.
The authoritative flavor/OCI instructions are in
[build flavors](../docs/build-flavors.md). Image results are streaming scripts:

```sh
nix build path:.#oci-xtcp2-min -o result-image
./result-image | docker load
```

## Unit, flavor, and quality checks

### Go and artifact tests

These `test-*` packages run during `nix build`, except `test-go-bench`, which
must be executed with `nix run`. Most emit `test.log` and sometimes
`coverage.out`; `test-go-unit` instead writes its log directly to the output
file. Package names alone do not imply inclusion in `nix flake check`.

| Target | Scope |
|---|---|
| `test-go-unit` | `pkg/xtcpnl/...` only, not the whole repository |
| `test-go-race` | Whole-repository race detector; also a registered check |
| `test-go-bench` | Fresh netlink parser benchmarks via `nix run path:.#test-go-bench` |
| `test-focused-asn-locality` | IP lookup and local-network classification |
| `test-focused-goip` | goip command/library and parity CLI |
| `test-focused-gordma` | RDMA command/library and strace replay |
| `test-focused-xtcp-enrich` | Daemon/package tests with both enrichment tags |
| `test-pkg-{xtcp,xtcpnl,io-uring,misc}` | Individual package trees; io-uring includes race checks |
| `test-cmd-{xtcp2,xtcp2client,xtcp2ctl}` | Individual command packages |
| `test-tools-{quality-report,netlink-audit,iouring-audit,metrics-audit,proto-field-audit}` | Individual audit/report tools |
| `test-go-flavor-{kafka,nats,nsq,valkey,s3parquet,asn,locality,enrich,s3parquet-enrich,all}` | Tag-aware daemon tests; all are registered checks. `all` enables all tags together, not a join of separate runs |
| `test-listener-security` | Listener authentication and security tests |
| `test-proto-deserialize-golden` | Deserialization golden test when `pkg/xtcp/deserialize` exists; otherwise records a skip |
| `test-ipmeta-bootstrap-artifact`, `test-oci-ipmeta-bootstrap-contents` | Fixture-backed bootstrap artifact and OCI content checks |
| `test-oci-link-monitor-env` | Monitor environment/defaults in OCI images |
| `test-linkmonitor` and its leaves | Monitor tests, lint, replay, fuzzing, artifacts, and documentation; [full reference below](#link-monitor-verification) |
| `test-linkmonitor-rdma-native-bench` | Native RDMA benchmark-runner gate, separate from the monitor aggregate |

Some Go checks require working host `io_uring` and deliberately fail instead
of skipping when rings cannot be created. See the target definitions under
[tests/](tests/default.nix) for tag sets and environment requirements.

```sh
nix build path:.#test-focused-goip -L -o result-goip
cat result-goip/test.log
nix build path:.#test-go-flavor-all -L -o result-flavors
```

### Registered static and harness checks

Use the full path: `nix build path:.#checks.x86_64-linux.NAME -L`.

| Check name or family | Purpose |
|---|---|
| `gofmt`, `go-vet`, `go-sec` | Go formatting, vet, and security analysis |
| `golangci-lint-quick`, `golangci-lint`, `golangci-lint-comprehensive` | Lint tiers 0, 1, and 2 |
| `lint-baseline` | Finding ratchet across all three tiers |
| `nix-fmt`, `deadnix`, `statix` | Nix formatting, unused bindings, and static policy |
| `netlink-audit`, `iouring-audit`, `metrics-audit`, `proto-field-audit`, `kernel-citation-audit` | Repository-specific semantic audits |
| `proto-audit-netlink` | Kernel-layout comparison through the pinned xdp2 oracle; cold builds can be substantial |
| `upstream-pins` | Locked-source/manifest consistency without querying upstream |
| `xtcp2-exec`, `xtcp2-schema-version` | NixOS ExecStart quoting and record-epoch/schema agreement |
| `cli-help-smoke-NAME` | Help invocation for the registered binary matrix; enumerate exact names with `nix eval` above |
| `capability-check-no-caps`, `capability-check-names-sys-admin` | Missing-capability rejection and diagnostics |
| `vm-lib-exp`, `vm-check-runner`, `nlcap-wait-linkwatch` | Host-side capture/VM-runner regression checks using stand-ins, not real guests |
| `self-test-flavors`, `self-test-stream-until`, `self-test-jsonl-tally`, `self-test-wait-metric` | Guest self-test script build and helper regressions |
| `vm-linkmonitor-performance-bundle` | Performance evidence-bundle validation |
| `test-go-race`, `test-go-flavor-*`, `test-linkmonitor` | Behavioral suites registered alongside static checks |
| `microvm-lifecycle-x86_64`, `vm-*`, `vm-all` | Actual guest checks described next; `vm-*` here refers to the seven guest checks, not the host helpers above |

## Nix integration testing

### Prerequisites and suite boundaries

The microVM lifecycle/capture runners use `/dev/kvm` and fixed console ports
12055/12056; the LACP partner additionally uses 12057/12058. Run those apps
sequentially on one host. Guests perform privileged networking inside the VM;
the host normally needs KVM access rather than root. Container-based pipeline
guests may need network access for image pulls and several GiB of RAM; the
mixed ClickHouse/Parquet guest is configured for 16 GiB. See
[microvms/constants.nix](microvms/constants.nix) for resource/port assignments.

The newer serial-console `test-*-vm` runners use the shared
[mkVmCheck](microvms/mkVmCheck.nix) framework. Most support
`--accel=auto|kvm|tcg`; the software-RDMA runner requires KVM. Cached guest
checks require a builder advertising the `kvm` system feature. Ionic fault
injection additionally needs `/dev/net/tun` in the sandbox. Missing facilities
are failures/unverified runs, not passing skips.

| Entry point | Runs | Does not cover |
|---|---|---|
| `checks.x86_64-linux.vm-all` | Seven registered monitor/goip guest checks; cached | Daemon lifecycle flavor sweep, captures, long soaks |
| `checks.x86_64-linux.microvm-lifecycle-x86_64` | Base daemon lifecycle guest; cached | Other lifecycle flavors |
| `integration-all` app | Sequential lifecycle flavor sweep plus goip parity; fresh | Monitor `vm-all`, interface-naming/ipmeta-bootstrap lifecycle cases, capture jobs, discovery/rate benchmarks |
| `integration-all -- --soak --duration 1h` | The sweep plus each configured soak for the given duration | Other investigation variants and physical-hardware validation |
| `nix flake check` | All registered checks, including the cached guest groups above | Unregistered lifecycle runners, capture apps, fresh performance/soak measurements |

Use [VM-CHECKS-STATUS.md](VM-CHECKS-STATUS.md) for recorded execution evidence;
building a runner or listing a target is not evidence that its guest passed.

### Daemon lifecycle flavors

Use `nix run path:.#test-microvm-lifecycle-x86_64-SUFFIX` to build and run one
case; the base target omits `-SUFFIX`. These are runnable packages.
`nix build` alone only builds their launchers. A passing run requires the
guest's overall self-test sentinel; failure/timeouts return nonzero.

| Suffix | Integration coverage | In `integration-all` |
|---|---|---|
| Base (no suffix) | Daemon startup, collection, namespace lifecycle, and self-tests | Yes |
| `uds-security` | Unix listeners and access controls | Yes |
| `s3parquet`, `s3parquet-iouring` | Parquet output into in-VM S3 storage, with optional io_uring | Yes |
| `clickhouse-http`, `clickhouse-pipeline` | HTTP ingestion or Kafka/Redpanda-to-ClickHouse pipeline | Yes |
| `valkey`, `nats`, `nsq` | Broker-specific delivery | Yes |
| `tcp-sink`, `udp-sink`, `unix-sink`, `unixgram-sink` | Stream/datagram output to local guest receivers | Yes |
| `udp-sink-iouring`, `unix-sink-iouring` | io_uring destination-send paths | Yes |
| `coverage`, `coverage-iouring` | Instrumented daemon execution and extracted Go coverage | Yes |
| `interface-naming` | Interface/locality and live ASN enrichment checks | No; run explicitly |
| `ipmeta-bootstrap` | Cold bootstrap, delayed feed recovery, cache rotation, bad-update rejection | No; run explicitly |

All the “Yes” rows also have `microvm-x86_64-lifecycle[-SUFFIX]` app aliases.
There are no corresponding lifecycle app aliases for `interface-naming` or
`ipmeta-bootstrap`; use their `test-microvm-lifecycle-*` package names.

```sh
nix run path:.#test-microvm-lifecycle-x86_64-interface-naming
nix run path:.#test-microvm-lifecycle-x86_64-ipmeta-bootstrap
nix run path:.#integration-all
```

Lifecycle runners print transcript paths in temporary storage. The base cached
check preserves `summary.txt`, `serial.log`, and `qemu.log`. Coverage runners
extract into `XTCP2_COVERDIR` (default `/tmp/xtcp2cov`); choose separate directories
for separate runs before using `coverage-merge`.

### Monitor and goip guests

Each pair below offers a fresh runnable package and a cached check. See
[guest tests as checks](#guest-tests-as-checks) for flags, evidence, and sandbox setup.

| Runnable package (`nix run path:.#NAME`) | Check (`checks.x86_64-linux.NAME`) | Coverage |
|---|---|---|
| `test-goip-argv-paths-vm` | `vm-goip-argv-paths` | HTTP command paths versus goip/ip |
| `test-linkmonitor-embedding-vm` | `vm-linkmonitor-embedding` | Library embedding, netlink lifecycle, and xtcp coexistence |
| `test-linkmonitor-ionic-vm` | `vm-linkmonitor-ionic` | Emulated NIC with real ionic/ionic_rdma drivers |
| `test-linkmonitor-ionic-faults-vm` | `vm-linkmonitor-ionic-faults` | Emulated Ethernet/RDMA faults and recovery |
| `test-linkmonitor-rdma-vm` | `vm-linkmonitor-rdma` | Software RoCE, events, and native RDMA I/O |
| `test-linkmonitor-performance-vm` | `vm-linkmonitor-performance` | Privileged live tests, profiles, benchmarks, and nlmon evidence |
| `test-xtcp2-linkmonitor-vm` | `vm-xtcp2-linkmonitor` | Real xtcp2 NixOS service, baseline persistence/reload, missing-link and degradation behavior |

`demo-linkmonitor-ionic-profiles` is a separate interactive/emulator scenario
runner: `nix run path:.#demo-linkmonitor-ionic-profiles -- --out=/tmp/ionic-demo`.
It supports `--accel=auto|kvm|tcg`, `--vcpus=N`, and scenario arguments. It is not
one of the `vm-all` checks.

### Soaks, pipeline inspection, and investigation runners

The following names are `nix run path:.#NAME` apps. Duration-bounded runners
accept `-- --duration 1h`; discovery and rate runners accept `-- --timeout SECONDS`.
The pipeline inspection apps boot a guest for manual use and do not imply a
self-terminating test verdict.

| Name | Purpose |
|---|---|
| `microvm-x86_64-soak` | Sustained namespace/socket churn |
| `microvm-x86_64-tcp-stress` | Container TCP load and resource monitoring |
| `microvm-x86_64-clickhouse-pipeline` | Inspect a live Kafka/Redpanda-to-ClickHouse stack |
| `microvm-x86_64-clickhouse-pipeline-parquet` | Inspect ClickHouse and S3/Parquet together |
| `microvm-x86_64-clickhouse-pipeline-stress` | Duration-bounded end-to-end pipeline stress |
| `microvm-x86_64-clickhouse-pipeline-rate-runner` | Finite throughput verdict grid |
| `microvm-x86_64-s3parquet-pipeline` | Inspect Parquet uploads to in-VM Garage S3 |
| `microvm-x86_64-s3parquet-runner` | Duration-bounded Parquet/S3 soak |
| `microvm-x86_64-s3parquet-stress` | TCP load plus upload/storage/resource checks |
| `microvm-x86_64-s3parquet-lowfreq` | Timer-driven flushing with sparse sockets and infrequent polls; allow about two hours |
| `microvm-x86_64-s3parquet-stress-{nl1,nl4,nl8,iouring,cpuload,cpuload-nl1}` | Decoder-count, io_uring, and CPU-contention investigation variants |
| `microvm-x86_64-discovery-bench` | Namespace-discovery method comparison |

`integration-all --soak` adds `soak`, `tcp-stress`, `clickhouse-pipeline-stress`,
`s3parquet-runner`, `s3parquet-stress`, `s3parquet-stress-iouring`, and
`s3parquet-lowfreq` sequentially, each for `--duration` (default 1h). A short
duration is a wiring smoke, especially for low-frequency timer flushing.

### Raw guest packages

`nix build path:.#microvm-x86_64-SUFFIX` builds a guest closure/launcher.
These raw packages are useful for preparing images and interactive debugging;
prefer the verdict/lifecycle apps for verification. The raw base package is
`microvm-x86_64`. Exported suffixes are:

- `coverage`, `coverage-iouring`, `soak`, `tcp-stress`, `interface-naming`, `ipmeta-bootstrap`, `capcheck-fail`.
- `clickhouse-http`, `clickhouse-pipeline`, `clickhouse-pipeline-rate`, `clickhouse-pipeline-stress`, `clickhouse-pipeline-parquet`.
- `s3parquet-pipeline`, `s3parquet-long`, `s3parquet-stress`, `s3parquet-lowfreq`, plus the six `s3parquet-stress-*` variants above.
- `valkey`, `tcp-sink`, `udp-sink`, `unix-sink`, `unixgram-sink`, `nats`, `nsq`.
- `nlmon-capture`, `netlink-dump-capture`, `goip-parity`, `link-write-capture`, `lacp-a`, `lacp-b`.

An app may share a raw package name but execute a different wrapper. For
example, building `microvm-x86_64-goip-parity` prepares the guest; running that
name selects the parity app that drives it and checks its verdict.

## Netlink and RDMA fixture collection

Use the controlled guest capture paths for reproducible fixtures. They run
privileged commands inside a disposable namespace/VM without requiring host
root. These apps are outside `nix flake check` and can write fixture files to
the checkout. Start with an explicit temporary output directory: default
capture destinations can replace existing fixtures, not just append new ones.

| App (`nix run path:.#NAME -- ARGS`) | Capture or verification | Output / useful arguments |
|---|---|---|
| `microvm-x86_64-nlmon-capture` | rtnetlink link/address/route/neighbor events | `pkg/xtcpnl/testdata/<kernel>/`; `--out DIR`, `--timeout SECONDS` |
| `microvm-x86_64-netlink-dump-capture` | Clean RTM_GET dump corpus, matching text/JSON sidecars, optional bridge/veth mesh | `<kernel>/dumps/`; `--out DIR`, `--timeout SECONDS`, `--skip-mesh` |
| `microvm-x86_64-link-write-capture` | Bond/LACP readbacks with a real partner in two guest namespaces | `<kernel>/link-write/`; `--out DIR`, `--timeout SECONDS` |
| `microvm-x86_64-lacp-pair-capture` | LACP partner in a second VM over virtio-net links, including link loss/recovery | `<kernel>/link-write/lacp-pair/`; `--out DIR`, `--timeout SECONDS` |
| `microvm-x86_64-goip-parity` | Capture ip/goip/ip transactions and compare requests/output inside the guest | `--out DIR`, `--timeout SECONDS`, `--keep-going`, `--no-allowlist`; nonzero on failed verdict |
| `microvm-x86_64-rdma-capture` | Software-RoCE RDMA requests/replies and native collector data | `--out DIR`, `--native-out DIR`, `--timeout SECONDS`, `--dry-run` |
| `microvm-x86_64-rdma-capture-{6_1,6_6,6_12,6_18,7_2}` | Same RDMA capture on the pinned kernel matrix | Same flags; unsuffixed app uses the latest kernel selected by nixpkgs |
| `capture-netlink-fixtures` | Host netlink dump capture, diagnostic fallback | Requires host root; other host traffic can contaminate captures |
| `capture-rdma-testdata` | Host RDMA capture, redaction, and manifest creation | `--dev DEV`, `--source NAME`, `--out ROOT`, `--dry-run`; live capture needs appropriate host/device privileges |

```sh
nix run path:.#microvm-x86_64-netlink-dump-capture -- --out /tmp/xtcp2-dumps
nix run path:.#microvm-x86_64-goip-parity -- --out /tmp/xtcp2-parity --keep-going
nix run path:.#microvm-x86_64-rdma-capture -- \
  --out /tmp/xtcp2-rdma --native-out /tmp/xtcp2-rdma-native
```

`nlmon` does not capture `NETLINK_RDMA`; RDMA fixtures use the separate
strace/native capture path. Buildable/runnable support tools are
`rdma-native-capture` and `rdma-capture-manifest`; the host wrapper handles
capture staging, redaction, and manifests. Host RDMA capture is observational
unless trigger/management options such as `--gid-trigger` or `--umad` are used.
Dry-run output checks tooling, not a real device.

The ionic runner also captures native fixtures:
`nix run path:.#test-linkmonitor-ionic-vm -- --capture --native-out=/tmp/ionic-native`.
Add `--promote` only when ready to copy fixtures into the checkout. See
[netlink fixture documentation](../docs/netlink/collection.md#regenerating-the-fixtures),
[goip coverage](../docs/netlink/coverage-status.md), and
[RDMA implementation status](../cmd/go-link-monitor/GO-NATIVE-RDMA-STATUS.md)
for evidence requirements and coverage limits.

## Link-monitor verification

Run the focused monitor checks with one target:

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

`test-linkmonitor-performance` is included in the monitor aggregate.
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

The benchmark runners accept `--output DIR`; use a new directory for each run.
Other tools have their own output contracts: PGO writes a binary, while the
comparison tool takes two existing benchmark files. Nix caches compilation
and correctness checks, but measurement results are
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
its source provenance under `share/go-link-monitor`. The standalone package
does not change the cgo configuration of xtcp2. The embedded-monitor service
and OCI behavior have separate integration targets described above. Remaining
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
`nlrecord/` in the evidence. It is outside the focused monitor
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
the same guest outside the sandbox (TCG where supported), keeps the last N runs (default
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

## Module map and keeping this guide current

| Module | Responsibility |
|---|---|
| [../flake.nix](../flake.nix) | Inputs, supported system, public outputs, and overlay |
| [default.nix](default.nix) | Export names, package/app/check wiring, `integration-all` membership |
| [versions.nix](versions.nix), [packages.nix](packages.nix), [devshell.nix](devshell.nix) | Tool versions, developer tools, and shell helpers |
| [binaries.nix](binaries.nix), [containers/default.nix](containers/default.nix) | Binary and OCI flavor generation |
| [lib/goSource.nix](lib/goSource.nix), [lib/goModules.nix](lib/goModules.nix) | Source filtering and vendored dependency preparation |
| [checks/default.nix](checks/default.nix) | Static audits, lint tiers, and host harness checks |
| [tests/default.nix](tests/default.nix) | Go, flavor, package, metadata, and monitor test derivations |
| [tests/linkmonitor.nix](tests/linkmonitor.nix), [tests/linkmonitor-rdma.nix](tests/linkmonitor-rdma.nix) | Focused monitor aggregate and native-RDMA gates |
| [tests/linkmonitor-perf/default.nix](tests/linkmonitor-perf/default.nix) | Fresh performance measurement apps |
| [microvms/default.nix](microvms/default.nix), [microvms/mkVm.nix](microvms/mkVm.nix), [microvms/lib.nix](microvms/lib.nix) | Guest flavors, construction, lifecycle/capture/soak runners |
| [microvms/mkVmCheck.nix](microvms/mkVmCheck.nix) | Shared cached guest-check and fresh-runner framework |
| [modules/xtcp2-service.nix](modules/xtcp2-service.nix) | Daemon NixOS service configuration used in guests |
| [modules/broker-server.nix](modules/broker-server.nix), [modules/garage-bucket-bootstrap.nix](modules/garage-bucket-bootstrap.nix), [modules/pyroscope-server.nix](modules/pyroscope-server.nix) | Guest broker, S3 bootstrap, and profiling services |
| [protos/default.nix](protos/default.nix), [quality-report/default.nix](quality-report/default.nix), [ipmeta-bootstrap/default.nix](ipmeta-bootstrap/default.nix) | Code generation, quality reporting, and bootstrap artifacts |

The `modules/` files are imported by the Nix implementation; this flake does
not export a `nixosModules` output. Consult the file's options/arguments when
reusing one.

When adding a target, update its workflow table and the relevant suite membership
description here. Use the three `nix eval ... --apply builtins.attrNames`
commands above to compare **actual exports**, including generated flavor names,
with the guide. Keep build-only runners, cached checks, fresh measurements, and
fixture-writing commands distinct. Record executed VM evidence in
[VM-CHECKS-STATUS.md](VM-CHECKS-STATUS.md); keep detailed testing methodology in
[integration testing](../docs/integration-testing.md) and
[stability testing](../docs/stability-testing.md).
