# Build flavors

xtcp2 binaries are built along two orthogonal axes, so you can produce anything from a fat debug build with every destination to a 20 MB single-destination image. Every target below is exposed by `flake.nix`; run `nix flake show` for the live list.

1. **Build variant** — whether symbols + DWARF are stripped: `debug` / default / `stripped` (`nix/versions.nix` → `buildVariants`).
2. **Destination flavor** — which message-destination clients are compiled in: `full` / `min` / `kafka` / `nats` / `nsq` / `valkey` (`nix/versions.nix` → `destinationFlavors`).

Stdlib destinations (`null`, `udp`, `unix`, `unixgram`) are **always** compiled regardless of flavor. Only the library destinations (`kafka`, `nats`, `nsq`, `valkey`) are gated by `//go:build dest_<scheme>` tags.

## Table of contents

- [Single-binary builds](#single-binary-builds)
- [Joined builds](#joined-builds)
- [Other cmd binaries](#other-cmd-binaries)
- [OCI images](#oci-images)
- [Choosing a flavor](#choosing-a-flavor)
- [Custom destination combinations](#custom-destination-combinations)
- [Build-tag mechanics](#build-tag-mechanics)
- [Building outside Nix](#building-outside-nix)
- [See also](#see-also)

## Single-binary builds

| Target | Variant | Library destinations | Binary size |
|---|---|---|---|
| `nix build .#xtcp2` | default (`-s -w`) | all four | 28.1 MB |
| `nix build .#xtcp2-debug` | debug (full symbols) | all four | 40.6 MB |
| `nix build .#xtcp2-stripped` | stripped (`-s -w` + `strip`) | all four | 28.1 MB |
| `nix build .#xtcp2-min` | default | none (stdlib only) | 20.1 MB |
| `nix build .#xtcp2-kafka` | default | kafka only | 24.5 MB |
| `nix build .#xtcp2-nats` | default | nats only | 21.3 MB |
| `nix build .#xtcp2-nsq` | default | nsq only | 20.3 MB |
| `nix build .#xtcp2-valkey` | default | valkey only | 24.1 MB |

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

| Target | Tag | Contents | Approx size |
|---|---|---|---|
| `nix build .#oci-xtcp2` | `xtcp2:latest` | all cmds, full destinations | 119 MiB |
| `nix build .#oci-xtcp2-debug` | `xtcp2:debug` | all cmds, debug variant | 171 MiB |
| `nix build .#oci-xtcp2-stripped` | `xtcp2:stripped` | all cmds, stripped | 119 MiB |
| `nix build .#oci-xtcp2-min` | `xtcp2:min` | only `xtcp2-min` | 22 MiB |
| `nix build .#oci-xtcp2-kafka` | `xtcp2:kafka` | only `xtcp2-kafka` | 26 MiB |
| `nix build .#oci-xtcp2-nats` | `xtcp2:nats` | only `xtcp2-nats` | 23 MiB |
| `nix build .#oci-xtcp2-nsq` | `xtcp2:nsq` | only `xtcp2-nsq` | 22 MiB |
| `nix build .#oci-xtcp2-valkey` | `xtcp2:valkey` | only `xtcp2-valkey` | 26 MiB |

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
```

## Choosing a flavor

- **Every destination at runtime** (config-driven destination): `xtcp2` / `oci-xtcp2`.
- **Unix-socket sink only** (`unix:` / `unixgram:`): `xtcp2-min` / `oci-xtcp2-min`. UDP and null come for free since they share Go's already-linked `net` package.
- **Kafka producer**: `xtcp2-kafka` / `oci-xtcp2-kafka` — drops ~4 MB by omitting the nats, nsq, and redis clients.
- **Debugging / profiling**: `xtcp2-debug` — keeps the symbol table and DWARF so `delve` and `go tool pprof` work directly.
- **Smallest image**: `xtcp2-stripped` or a slim per-flavor image.

## Custom destination combinations

The named flavors are single-destination. For combinations (e.g. kafka + valkey), call `mkGoBinary` (`nix/lib/mkGoBinary.nix`) directly with a `destinations` list:

```nix
mkGoBinary {
  name = "xtcp2";
  src = ./.;
  variant = "default";
  destinations = [ "kafka" "valkey" ];   # combine any subset
}
```

The `destinations` knob accepts `null` (all four — the `full` flavor), `[ ]` (none — the `min` flavor), or a list of schemes. Build tags are derived as `dest_<scheme>` per entry and appended to `versions.buildTags` (`netgo`, `osusergo`).

## Build-tag mechanics

| File | Build tag | Compiled in |
|---|---|---|
| `destinations_core.go`, `destinations_null.go`, `destinations_udp.go`, `destinations_unix.go`, `destinations_unixgram.go` | (none) | always |
| `destinations_kafka.go` | `//go:build dest_kafka` | only with `-tags dest_kafka` |
| `destinations_nats.go` | `//go:build dest_nats` | only with `-tags dest_nats` |
| `destinations_nsq.go` | `//go:build dest_nsq` | only with `-tags dest_nsq` |
| `destinations_valkey.go` | `//go:build dest_valkey` | only with `-tags dest_valkey` |
| `destinations_s3parquet.go` | `//go:build dest_s3parquet` | only with `-tags dest_s3parquet` |

Each tagged file calls `RegisterDestination(scheme, factory)` from its `init()`. With the tag off the file isn't compiled, so the registry simply lacks that scheme. The CLI distinguishes "unknown scheme" from "known but not compiled in":

```
$ xtcp2-min -dest kafka:broker:9092
destination "kafka" is not compiled into this binary; rebuild with
'-tags dest_kafka' (or use the matching `xtcp2-kafka` Nix attribute).
Compiled-in destinations: [null udp unix unixgram]
```

## Building outside Nix

```bash
# Full xtcp2 (every destination):
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka,dest_nats,dest_nsq,dest_valkey" \
    -ldflags "-s -w" -trimpath -o xtcp2 ./cmd/xtcp2

# Unix-domain-socket flavor (stdlib only):
CGO_ENABLED=0 go build -tags "netgo,osusergo" -ldflags "-s -w" -trimpath -o xtcp2-min ./cmd/xtcp2

# Kafka only:
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka" -ldflags "-s -w" -trimpath -o xtcp2-kafka ./cmd/xtcp2
```

The Nix builds also inject `-X main.commit=…`, `-X main.date=…`, `-X main.version=…`; those are optional for ad-hoc builds.

## See also

- [Output formats & destinations](output-and-destinations.md) — what each destination does.
- [CONTRIBUTING.md](../CONTRIBUTING.md) — the broader build/test workflow.
- Source: `pkg/xtcp/destinations_*.go`, `nix/lib/mkGoBinary.nix`, `nix/versions.nix`, `nix/binaries.nix`, `nix/containers/`.
