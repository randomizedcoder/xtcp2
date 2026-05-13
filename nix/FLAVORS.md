# xtcp2 build flavors

Every `nix build` target listed here is exposed by `flake.nix`. The targets fall into two orthogonal axes:

1. **Build variant** — controls whether symbols + DWARF are stripped (`debug` / `default` / `stripped`). See `nix/versions.nix` → `buildVariants`.
2. **Destination flavor** — which message-destination clients are compiled into the binary (`full` / `min` / `kafka` / `nats` / `nsq` / `valkey`). See `nix/versions.nix` → `destinationFlavors`.

Stdlib destinations (`null`, `udp`, `unix`, `unixgram`) are **always compiled** regardless of flavor. Only the library destinations (`kafka`, `nats`, `nsq`, `valkey`) are gated by `//go:build dest_<scheme>` tags.

## Quick reference: every target

### Single-binary builds

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

### Joined builds (every cmd binary)

`xtcp2-all*` is a `symlinkJoin` containing all 10 `cmd/<name>/` binaries under one `/bin/`. Used as the contents of the fat OCI images.

| Target | Variant |
|---|---|
| `nix build .#xtcp2-all` | default |
| `nix build .#xtcp2-all-debug` | debug |
| `nix build .#xtcp2-all-stripped` | stripped |

### Other cmd binaries

The other nine `cmd/<name>/` binaries don't import `pkg/xtcp`, so destination flavors don't apply. Each is exposed at its default variant:

```
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

### OCI images

Eight images. The three "fat" images carry every cmd binary; the five "slim" images carry only the single matching `xtcp2-<flavor>` binary.

| Target | Tag | Contents | Approx image size |
|---|---|---|---|
| `nix build .#oci-xtcp2` | `xtcp2:latest` | all 10 cmds, full destinations | 119 MiB |
| `nix build .#oci-xtcp2-debug` | `xtcp2:debug` | all 10 cmds, debug variant | 171 MiB |
| `nix build .#oci-xtcp2-stripped` | `xtcp2:stripped` | all 10 cmds, stripped | 119 MiB |
| `nix build .#oci-xtcp2-min` | `xtcp2:min` | only `xtcp2-min`, stdlib only | 22 MiB |
| `nix build .#oci-xtcp2-kafka` | `xtcp2:kafka` | only `xtcp2-kafka` | 26 MiB |
| `nix build .#oci-xtcp2-nats` | `xtcp2:nats` | only `xtcp2-nats` | 23 MiB |
| `nix build .#oci-xtcp2-nsq` | `xtcp2:nsq` | only `xtcp2-nsq` | 22 MiB |
| `nix build .#oci-xtcp2-valkey` | `xtcp2:valkey` | only `xtcp2-valkey` | 26 MiB |

All images are built via `pkgs.dockerTools.streamLayeredImage`: the `./result` is a shell script that streams a docker-loadable tarball on stdout.

```
nix build .#oci-xtcp2-kafka
./result | docker load           # load into docker
docker run --rm xtcp2:kafka -help
docker run --rm xtcp2:kafka -dest kafka:broker:9092 -topic xtcp2
```

For the fat images, switch entrypoint to run a non-xtcp2 binary:

```
nix build .#oci-xtcp2
./result | docker load
docker run --rm --entrypoint /bin/register_schema xtcp2:latest -help
```

## When to use which flavor

- **You need every destination at runtime** (e.g. a single binary deployed across environments where the destination is config-driven): `xtcp2` / `oci-xtcp2`. Backward-compatible with the existing `Containerfile`.
- **Unix-domain-socket sink only** (collector reads via `unix:/run/xtcp2.sock` or `unixgram:`): `xtcp2-min` / `oci-xtcp2-min`. UDP and null come along for free; they share Go's `net` package code that's already linked.
- **Kafka producer**: `xtcp2-kafka` / `oci-xtcp2-kafka`. Drops 4 MB by leaving out nats.go, go-nsq, and go-redis.
- **Debugging or profiling**: `xtcp2-debug` / `oci-xtcp2-debug`. Keeps the symbol table and DWARF so `delve` and `go tool pprof -symbolize` work directly on the binary.
- **Smallest possible image**: `xtcp2-stripped` (whole-cmd set) or one of the slim per-flavor images. The `strip` pass after `-s -w` is the trick described at <https://words.filippo.io/shrink-your-go-binaries-with-this-one-weird-trick/>; with modern Go (1.26+) the additional savings are modest (~3 KB / binary).

## Custom destination combinations

The named flavors are single-destination only. For combinations (e.g. kafka + valkey), call `mkGoBinary` directly. From a wrapper flake or via `nix-build -E`:

```nix
let
  flake = builtins.getFlake (toString /home/das/Downloads/xtcp2);
  pkgs = flake.inputs.nixpkgs.legacyPackages.x86_64-linux;
  mkGoBinary = import ./nix/lib/mkGoBinary.nix {
    inherit pkgs;
    lib = pkgs.lib;
    giouring = flake.inputs.giouring;
  };
in
mkGoBinary {
  name = "xtcp2";
  src = ./.;
  variant = "default";
  destinations = [ "kafka" "valkey" ];   # combine any subset
}
```

The same `destinations` knob accepts:
- `null` → all four library destinations (the `full` flavor).
- `[ ]` → none (the `min` flavor; stdlib still works).
- `[ "kafka" ]` etc. → just the listed schemes.

Build tags are derived as `dest_<scheme>` per entry and appended to `versions.buildTags` (`netgo`, `osusergo`).

## Build tag mechanics

Build tags drive what gets compiled. The Go side:

| File | Build tag | Compiled in |
|---|---|---|
| `pkg/xtcp/destinations_core.go` | (none) | always |
| `pkg/xtcp/destinations_null.go` | (none) | always |
| `pkg/xtcp/destinations_udp.go` | (none) | always |
| `pkg/xtcp/destinations_unix.go` | (none) | always |
| `pkg/xtcp/destinations_unixgram.go` | (none) | always |
| `pkg/xtcp/destinations_kafka.go` | `//go:build dest_kafka` | only with `-tags dest_kafka` |
| `pkg/xtcp/destinations_nats.go` | `//go:build dest_nats` | only with `-tags dest_nats` |
| `pkg/xtcp/destinations_nsq.go` | `//go:build dest_nsq` | only with `-tags dest_nsq` |
| `pkg/xtcp/destinations_valkey.go` | `//go:build dest_valkey` | only with `-tags dest_valkey` |

Each tagged file calls `RegisterDestination(scheme, factory)` from its `func init()`. When the tag is off, the file isn't compiled, the init doesn't run, and the package-level registry simply doesn't have that scheme. The runtime CLI error path distinguishes "unknown scheme" from "known but not compiled in":

```
$ xtcp2-min -dest kafka:broker:9092
destination "kafka" is not compiled into this binary; rebuild with
'-tags dest_kafka' (or use the matching `xtcp2-kafka` Nix attribute).
Compiled-in destinations: [null udp unix unixgram]
```

## Building from scratch outside Nix

If you don't have Nix, plain `go build` works. The default tags are baked into `mkGoBinary`'s wrapper but are equivalent to:

```bash
# Full xtcp2 (every destination):
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka,dest_nats,dest_nsq,dest_valkey" \
    -ldflags "-s -w" -trimpath -o xtcp2 ./cmd/xtcp2

# Unix-domain-socket flavor (stdlib only):
CGO_ENABLED=0 go build -tags "netgo,osusergo" \
    -ldflags "-s -w" -trimpath -o xtcp2-min ./cmd/xtcp2

# Kafka only:
CGO_ENABLED=0 go build -tags "netgo,osusergo,dest_kafka" \
    -ldflags "-s -w" -trimpath -o xtcp2-kafka ./cmd/xtcp2
```

The Nix builds add `-X main.commit=...`, `-X main.date=...`, `-X main.version=...` ldflags; for ad-hoc builds those are optional.

## Reference

- Plan: `/home/das/.claude/profiles/runpod/plans/in-this-repo-is-steady-wren.md`
- Source: `pkg/xtcp/destinations_*.go`, `nix/lib/mkGoBinary.nix`, `nix/versions.nix`, `nix/binaries.nix`, `nix/containers/`
- Filippo on stripping: <https://words.filippo.io/shrink-your-go-binaries-with-this-one-weird-trick/>
