# Compatibility checks and guest test executables; this derivation boots no VM.
{
  pkgs,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-test-linkmonitor-embedding"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
      # The race detector needs cgo; nothing under test uses it.
      pkgs.gcc
    ];
  }
  ''
    set -euo pipefail
    cp -r ${vendoredSource} ./xtcp2
    chmod -R u+w ./xtcp2
    cd xtcp2
    export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=1
    export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
    export GOLANGCI_LINT_CACHE="$TMPDIR/lint-cache"
    mkdir -p "$out/bin"
    printf '%s\n' '${src}' > "$out/source.txt"
    go version > "$out/go-version.txt"
    uname -srmo > "$out/kernel.txt"
    # Match the existing xtcp test targets: its giouring dependency uses linkname.
    echo 'Running complete xtcp unit suite'
    go test -ldflags=-checklinkname=0 -json -count=1 -timeout=5m ./pkg/xtcp > "$out/xtcp.jsonl" 2>&1 || { cat "$out/xtcp.jsonl"; exit 1; }
    # One build since R4-T02 removed the cgo rdma variant.
    echo "Running embedding race suite and building guest executable"
    go test -ldflags=-checklinkname=0 -race -json -count=10 -timeout=5m \
      ./pkg/xtcp ./pkg/linkmonitor/... -run 'Test(LinkmonitorEmbedding|Embedding|RunFailures|LifecycleIncompleteShutdown)' > "$out/core-race.jsonl" 2>&1 || { cat "$out/core-race.jsonl"; exit 1; }
    CGO_ENABLED=0 go test -tags embedding_vm -c -o "$out/bin/core.test" ./pkg/linkmonitor
    # TestLinkMonitorOnDemandScrape skips here (no /sys); the embedding VM runs it.
    CGO_ENABLED=0 go test -ldflags=-checklinkname=0 -c -o "$out/bin/xtcp.test" ./pkg/xtcp
    echo 'Running embedding vet and comprehensive lint'
    go vet ./pkg/xtcp > "$out/vet.log" 2>&1 || { cat "$out/vet.log"; exit 1; }
    golangci-lint run --config .golangci-comprehensive.yml ./pkg/xtcp > "$out/lint.log" 2>&1 || { cat "$out/lint.log"; exit 1; }
    golangci-lint run --config .golangci-comprehensive.yml --build-tags embedding_vm ./pkg/linkmonitor/... > "$out/guest-lint.log" 2>&1 || { cat "$out/guest-lint.log"; exit 1; }
  ''
