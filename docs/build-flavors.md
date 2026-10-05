# Build flavors

xtcp2 binaries are built along three orthogonal axes, so you can produce anything from a fat debug build with every feature to a stdlib-only single-destination image. Every target below is exposed by `flake.nix`; run `nix flake show` for the live list.

1. **Build variant** — whether symbols + DWARF are stripped: `debug` / default / `stripped` (`nix/versions.nix` → `buildVariants`).
2. **Destination flavor** — which message-destination clients are compiled in: `full` / `min` / `kafka` / `nats` / `nsq` / `valkey` / `s3parquet` (`nix/versions.nix` → `destinationFlavors`).
3. **Enrichment flavor** — whether the two heavyweight enrichers are compiled in: `none` / `asn` / `locality` / `enrich` (both) (`nix/versions.nix` → `enrichmentFlavors`).

Stdlib destinations (`null`, `stdout`, `stderr`, `file`, `tcp`, `http`, `https`, `udp`, `unix`, `unixgram`) are **always** compiled regardless of flavor. Only the library destinations (`kafka`, `nats`, `nsq`, `valkey`, `s3parquet`) are gated, by `//go:build dest_<scheme>`.

Likewise, most metadata enrichers (container, LLDP, NIC, nsid) are **always** compiled and merely toggled at runtime — each is pure Go with no third-party dependency, so carrying them costs nothing. Only **ASN** and **locality** are build-tag gated, by `//go:build enrich_asn` / `//go:build enrich_locality`: `pkg/ipasn` pulls in `parquet-go` and `gaissmai/bart`, and `pkg/localnet` pulls in `bart`. Left unconditional they would land in `xtcp2-min`, whose entire purpose is to be the stdlib-only slim daemon.

> **The enrichment axis is new, and the unsuffixed names moved.** `xtcp2-min`, `oci-xtcp2-s3parquet` and friends are now the **no-enricher** cell. If you were relying on those images for ASN or locality columns, switch to the `-enrich` suffix (`oci-xtcp2-s3parquet-enrich`). The fat `oci-xtcp2` / `-debug` / `-stripped` images are unchanged: they carry every destination and every enricher. Entrypoint, exposed ports and healthcheck are identical across all of them.

Both gated enrichers still default to **off at runtime** (`-enrichAsn` / `-enrichLocality`). The build tag only decides whether the code is in the binary; asking for one that isn't is a fatal startup error, not a silent no-op — see [Build-tag mechanics](#build-tag-mechanics).

## Table of contents

- [Single-binary builds](#single-binary-builds)
- [Joined builds](#joined-builds)
- [Other cmd binaries](#other-cmd-binaries)
- [OCI images](#oci-images)
- [Choosing a flavor](#choosing-a-flavor)
- [Custom destination and enrichment combinations](#custom-destination-and-enrichment-combinations)
- [Which enrichers does this binary have?](#which-enrichers-does-this-binary-have)
- [Build-tag mechanics](#build-tag-mechanics)
- [Building outside Nix](#building-outside-nix)
- [See also](#see-also)

## Single-binary builds

The three unsuffixed `xtcp2*` attrs are the "everything" builds — every library destination and
every enricher:

| Target | Variant | Binary size |
|---|---|---|
| `nix build .#xtcp2` | default (`-s -w`) | 39.9 MB |
| `nix build .#xtcp2-debug` | debug (full symbols) | 57.2 MB |
| `nix build .#xtcp2-stripped` | stripped (`-s -w` + `strip`) | 39.9 MB |

The per-flavor builds are the cross product of the six destination flavors and the four enrichment
flavors. The unsuffixed name is the **no-enrichment** cell; `-asn`, `-locality` and `-enrich` (both)
opt in. All are the `default` variant:

| Destination flavor | none | `-asn` | `-locality` | `-enrich` |
|---|---|---|---|---|
| `xtcp2-min` (stdlib only: null, udp, unix, unixgram) | 22.1 MB | 27.3 MB | 22.4 MB | 27.6 MB |
| `xtcp2-kafka` | 26.1 MB | 30.8 MB | 26.5 MB | 31.1 MB |
| `xtcp2-nats` | 23.2 MB | 28.3 MB | 23.6 MB | 28.7 MB |
| `xtcp2-nsq` | 22.2 MB | 27.4 MB | 22.6 MB | 27.8 MB |
| `xtcp2-valkey` | 26.1 MB | 31.3 MB | 26.5 MB | 31.6 MB |
| `xtcp2-s3parquet` | 30.4 MB | 30.9 MB | 30.8 MB | 31.2 MB |

Two things worth reading off that table:

- **ASN costs ~5.2 MB**, nearly all of it `parquet-go`, which `pkg/ipasn` needs to read the
  artifact. **Except on `s3parquet`, where it costs ~0.5 MB** — that flavor already links
  `parquet-go` for its own destination, so the marginal cost collapses to `pkg/ipasn` itself.
  `s3parquet-enrich` is by far the cheapest way to get ASN.
- **Locality costs ~0.4 MB** on every flavor. `pkg/localnet` does rtnetlink over the already-linked
  `pkg/xtcpnl`, and its only new dependency is `bart` for longest-prefix matching — which the
  `min` → `min-locality` delta shows is small. (`pkg/ipasn` uses `bart` too, so on any `-enrich`
  build it is paid for once.) If you are unsure whether you want locality, take it.

## Joined builds

`xtcp2-all*` is a `symlinkJoin` containing every `cmd/<name>/` binary under one `/bin/`, used as the contents of the fat OCI images.

| Target | Variant |
|---|---|
| `nix build .#xtcp2-all` | default |
| `nix build .#xtcp2-all-debug` | debug |
| `nix build .#xtcp2-all-stripped` | stripped |

## Other cmd binaries

The other `cmd/<name>/` binaries don't import `pkg/xtcp`, so destination flavors don't apply. Each is exposed at its default variant:

```sh
nix build .#clickhouse_http_insert_protobuflist
nix build .#clickhouse_protobuflist
nix build .#clickhouse_protobuflist_db
nix build .#kafka_to_clickhouse
nix build .#ns
nix build .#nsTest
nix build .#register_schema
nix build .#xtcp2client
nix build .#xtcp2_kafka_client
```

## OCI images

The three "fat" images carry every cmd binary; the slim images carry only the single matching `xtcp2-<flavor>` binary.

Sizes below are the uncompressed tar stream (`./result | wc -c`), which is what `docker load`
consumes. A registry stores the layers gzipped, so a push is considerably smaller.

Re-measure with:

```sh
nix run .#oci-size-report                  # slim daemon matrix + standalone images
nix run .#oci-size-report -- --fat         # fat images only
nix run .#oci-size-report -- --all         # both sets
```

The fat images carry every cmd binary and every enricher. They are exposed by the flake, but this
2026-09-28 local tree cannot currently build them because `xtcp2-all` includes `goip`, and
`cmd/goip` is blocked by `internal/goip/dispatch.go:50:23: undefined: runRoute`.

| Target | Tag | Contents | Current status |
|---|---|---|---|
| `nix build .#oci-xtcp2` | `xtcp2:latest` | all cmds, all destinations, all enrichers | build blocked by `goip` |
| `nix build .#oci-xtcp2-debug` | `xtcp2:debug` | as above, debug variant | build blocked by `goip` |
| `nix build .#oci-xtcp2-stripped` | `xtcp2:stripped` | as above, stripped | build blocked by `goip` |

The slim images carry exactly one `xtcp2` binary at the matching destination × enrichment cell. The
image tag is the attr name minus the `oci-xtcp2-` prefix (`oci-xtcp2-kafka-asn` → `xtcp2:kafka-asn`):

| Destination flavor | none | `-asn` | `-locality` | `-enrich` |
|---|---|---|---|---|
| `oci-xtcp2-min` | 25.3 MiB | 31.0 MiB | 25.7 MiB | 31.3 MiB |
| `oci-xtcp2-kafka` | 29.2 MiB | 34.4 MiB | 29.6 MiB | 34.7 MiB |
| `oci-xtcp2-nats` | 26.4 MiB | 32.1 MiB | 26.8 MiB | 32.3 MiB |
| `oci-xtcp2-nsq` | 25.5 MiB | 31.2 MiB | 25.9 MiB | 31.5 MiB |
| `oci-xtcp2-valkey` | 29.2 MiB | 34.9 MiB | 29.6 MiB | 35.2 MiB |
| `oci-xtcp2-s3parquet` | 33.3 MiB | 33.8 MiB | 33.7 MiB | 34.1 MiB |

The standalone single-purpose images:

| Target | Tag | Contents | Size |
|---|---|---|---|
| `nix build .#oci-ipfeed-collector` | `ipfeed-collector:latest` | only `ipfeed-collector`, which builds the ASN Parquet artifact | 27.9 MiB |
| `nix build .#oci-xtcp2client` | `xtcp2client:latest` | only `xtcp2client` (gRPC record-stream client) | 16.0 MiB |
| `nix build .#oci-xtcp2ctl` | `xtcp2ctl:latest` | only `xtcp2ctl` (runtime-control client) | 15.4 MiB |
| `nix build .#oci-xtcp2-tcp-stress` | `xtcp2-tcp-stress:latest` | `tcp_server`, `tcp_client`, and the shell entrypoint used by microVM stress tests | 72.7 MiB |

`ipfeed-collector` is packaged on its own rather than bundled into the enrichment images: it is a
periodic batch job that produces the artifact, not part of the daemon's runtime, and at 27.9 MiB it
would roughly double a slim image. Run it as a sidecar or a cron job and hand the daemon the result
through a mounted volume or S3.

The ASN flavor includes both the representative ASN lookup and the `network_owner` lookup from the
same IP metadata artifact. Bootstrap-capable daemon image attrs append `-bootstrap` to the normal
image attr and Docker tag, for example `oci-xtcp2-kafka-asn-bootstrap` and
`xtcp2:kafka-asn-bootstrap`. All bootstrap images share one Nix artifact,
`ipmeta-bootstrap-artifact`, installed at:

```text
/share/xtcp2/ipmeta/bootstrap.lookup.parquet.zst
```

That artifact is built from `nix/ipmeta-bootstrap-lock.json` when present. The lock can point at a
checked-in file under `nix/` or at an immutable/fixed-hash remote URL such as a GitHub release asset.
`nix/ipmeta-bootstrap-lock.example.json` shows the local-file shape; the build error includes both
local and remote examples when the real lock is missing. The measured images above contain code
support only, because this repo does not yet carry or pin a real fleet bootstrap artifact.

Images are built with `pkgs.dockerTools.streamLayeredImage`: `./result` is a script that streams a docker-loadable tarball on stdout.

```sh
nix build .#oci-xtcp2-kafka
./result | docker load
docker run --rm xtcp2:kafka -help
docker run --rm xtcp2:kafka -dest kafka:broker:9092 -topic xtcp2

# Fat images: switch the entrypoint to a different binary
nix build .#oci-xtcp2
./result | docker load
docker run --rm --entrypoint /bin/register_schema xtcp2:latest -help

# Slim client images (the gRPC clients, for users who only want the client)
nix build .#oci-xtcp2client
./result | docker load
docker run --rm xtcp2client:latest -help
docker run --rm xtcp2client:latest -target daemon-host -port 8889
# xtcp2ctl is the runtime-control client:
nix build .#oci-xtcp2ctl && ./result | docker load
docker run --rm xtcp2ctl:latest -help
```

## Choosing a flavor

- **Everything** (config-driven destination, both enrichers available): `xtcp2` / `oci-xtcp2`; the fat OCI image is currently blocked by the local `goip` compile issue noted above.
- **Unix-socket sink only** (`unix:` / `unixgram:`): `xtcp2-min` / `oci-xtcp2-min`. UDP and null come for free since they share Go's already-linked `net` package.
- **Kafka producer**: `xtcp2-kafka` / `oci-xtcp2-kafka` — the slim image is 29.2 MiB without gated enrichers, by omitting the nats, nsq, redis and s3/parquet clients.
- **Debugging / profiling**: `xtcp2-debug` — keeps the symbol table and DWARF so `delve` and `go tool pprof` work directly.
- **Smallest image**: a slim per-flavor image at enrichment `none` — `oci-xtcp2-min` at 25.3 MiB is the current image floor.
- **ASN / locality enrichment**: append `-asn`, `-locality` or `-enrich` to any destination flavor (`xtcp2-s3parquet-enrich`, `oci-xtcp2-kafka-asn`, ...). Cost is in the table above. For cold-start bootstrap data, use a matching `-bootstrap` OCI attr after adding `nix/ipmeta-bootstrap-lock.json`; otherwise mount the IP metadata artifact at runtime or run `oci-ipfeed-collector` as a sidecar. See [ipfeed-asn-enrichment.md](ipfeed-asn-enrichment.md).

## Custom destination and enrichment combinations

The named flavors are single-destination. For combinations (e.g. kafka + valkey), call `mkGoBinary` (`nix/lib/mkGoBinary.nix`) directly with `destinations` and/or `enrichments` lists:

```nix
mkGoBinary {
  name = "xtcp2";
  src = ./.;
  variant = "default";
  destinations = [ "kafka" "valkey" ];   # combine any subset
  enrichments  = [ "asn" ];              # ditto
}
```

Both knobs follow the same convention:

| Value | `destinations` | `enrichments` |
|---|---|---|
| `null` (the default) | every library destination — the `full` flavor | every enricher |
| `[ ]` | none — the `min` flavor | none |
| a list | exactly those | exactly those |

`null` being the default is what keeps every pre-existing caller — the `xtcp2` attr and all three fat images — carrying every feature exactly as before.

Build tags are derived as `dest_<scheme>` / `enrich_<feature>` per entry and appended to `versions.buildTags` (`netgo`, `osusergo`).

## Which enrichers does this binary have?

Three ways, all reporting the same registry:

```sh
# 1. -help annotates the two gated flags
xtcp2 -help 2>&1 | grep -A1 enrichAsn

# 2. -conf prints the list next to the config
xtcp2 -conf | grep compiledInEnrichers

# 3. a running daemon publishes one gauge per known enricher: 1 or 0
curl -s localhost:9088/metrics | grep compiledInEnrichers
xtcp_gauges{function="InitPromethus",type="asn",variable="compiledInEnrichers"} 0
xtcp_gauges{function="InitPromethus",type="locality",variable="compiledInEnrichers"} 0
```

The absent ones are published as `0` deliberately: a binary built without the tag never touches the `asn` counters, so without this gauge "the image lacks the code" looks identical to "enabled but the artifact never loaded".

## Build-tag mechanics

| File | Build tag | Compiled in |
|---|---|---|
| `destinations_core.go`, `destinations_null.go`, `destinations_udp.go`, `destinations_unix.go`, `destinations_unixgram.go` | (none) | always |
| `destinations_kafka.go` | `//go:build dest_kafka` | only with `-tags dest_kafka` |
| `destinations_nats.go` | `//go:build dest_nats` | only with `-tags dest_nats` |
| `destinations_nsq.go` | `//go:build dest_nsq` | only with `-tags dest_nsq` |
| `destinations_valkey.go` | `//go:build dest_valkey` | only with `-tags dest_valkey` |
| `destinations_s3parquet.go` | `//go:build dest_s3parquet` | only with `-tags dest_s3parquet` |
| `enrich.go`, `enrich_core.go` | (none) | always |
| `enrich_asn.go` | `//go:build enrich_asn` | only with `-tags enrich_asn` |
| `enrich_locality.go` | `//go:build enrich_locality` | only with `-tags enrich_locality` |

Each tagged file calls `RegisterDestination(scheme, factory)` / `RegisterEnricher(name, factory)` from its `init()`. With the tag off the file isn't compiled, so the registry simply lacks that entry — no `!tag` stub file is needed. Both registries panic on a duplicate registration, which can only mean two files claimed the same tag.

The rule that makes this work is that **no tagged package's type may appear on the `XTCP` struct**: `dest` is a `Destination` interface, and the two enrichers are `asnLookuper` / `localityEnricher` interfaces declared in untagged code. A single `*ipasn.Index`-typed field would re-link parquet-go into every flavor and silently undo the whole scheme.

The CLI distinguishes "unknown" from "known but not compiled in", for both:

```
$ xtcp2-min -dest kafka:broker:9092
destination "kafka" is not compiled into this binary; rebuild with
'-tags dest_kafka' (or use the matching `xtcp2-kafka` Nix attribute).
Compiled-in destinations: [null udp unix unixgram]

$ xtcp2-min -enrichAsn -asnDbPath /run/xtcp2-asn/asn.parquet
-enrichAsn requested but the asn enricher is not compiled into this binary;
rebuild with '-tags enrich_asn' (or use a matching `xtcp2-*-asn` /
`xtcp2-*-enrich` Nix attribute). Compiled-in enrichers: []
```

The destination case is *unavailable* — the daemon has nowhere to send records. The enricher case is a deliberate choice to be fatal rather than best-effort: every other enricher degrades because a socket or device might be missing on *this host*, which redeploying the same image cannot fix. A missing build tag is the opposite — a property of the artifact, identical on every host — and the failure it produces (silently empty ASN columns across a whole fleet) is exactly what this gating exists to make visible. The gRPC `ConfigService.Set` rejects the same mistake with `FailedPrecondition`, because `Set` re-execs the *same* binary.

### Testing a tagged build

`go test ./...` compiles none of the tagged files, so their tests need the tag:

```sh
go test -tags 'enrich_asn enrich_locality' ./pkg/xtcp/ ./cmd/xtcp2/
go test -tags 'dest_s3parquet enrich_asn enrich_locality' ./pkg/xtcp/
```

`nix flake check` does this for you via `nix/tests/go-test-flavors.nix`, which has one target per flavor plus an `all` target. The registry and hot-path tests (`enrich_core_test.go`, `enrich_seam_test.go`) are deliberately **untagged** and derive their expectations from `EnricherCompiledIn`, so they assert correct behaviour in every flavor — including the builds that lack the code.

## Building outside Nix

```bash
# Full xtcp2 (every destination + every enricher):
CGO_ENABLED=0 go build \
    -tags "netgo,osusergo,dest_kafka,dest_nats,dest_nsq,dest_valkey,dest_s3parquet,enrich_asn,enrich_locality" \
    -ldflags "-s -w" -trimpath -o xtcp2 ./cmd/xtcp2

# Unix-domain-socket flavor (stdlib only, no enrichers):
CGO_ENABLED=0 go build -tags "netgo,osusergo" -ldflags "-s -w" -trimpath -o xtcp2-min ./cmd/xtcp2

# Kafka only:
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka" -ldflags "-s -w" -trimpath -o xtcp2-kafka ./cmd/xtcp2

# Kafka + both enrichers:
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka,enrich_asn,enrich_locality" \
    -ldflags "-s -w" -trimpath -o xtcp2-kafka-enrich ./cmd/xtcp2
```

The Nix builds also inject `-X main.commit=…`, `-X main.date=…`, `-X main.version=…`; those are optional for ad-hoc builds.

## See also

- [Output formats & destinations](output-and-destinations.md) — what each destination does.
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the broader build/test workflow.
- [IP -> ASN enrichment](ipfeed-asn-enrichment.md) and [locality enrichment](locality-enrichment.md) — what the two gated enrichers do.
- Source: `pkg/xtcp/destinations_*.go`, `pkg/xtcp/enrich_core.go`, `pkg/xtcp/enrich_{asn,locality}.go`, `nix/lib/mkGoBinary.nix`, `nix/versions.nix`, `nix/binaries.nix`, `nix/containers/`.
