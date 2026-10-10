# Compatibility checks and guest test executables; this derivation boots no VM.
{
  pkgs,
  src,
  vendoredSource,
}:
let
  versions = import ../versions.nix { inherit pkgs; };
  rdma = import ../lib/linkmonitor-rdma.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-test-linkmonitor-embedding"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
    ]
    ++ rdma.nativeBuildInputs;
    inherit (rdma) buildInputs;
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
    for tags in "" rdma; do
      name=core
      if [ -n "$tags" ]; then name=rdma; fi
      echo "Running $name embedding race suite and building guest executable"
      go test -tags "$tags" -ldflags=-checklinkname=0 -race -json -count=10 -timeout=5m \
        ./pkg/xtcp ./pkg/linkmonitor/... -run 'Test(LinkmonitorEmbedding|Embedding|RunFailures|LifecycleIncompleteShutdown)' > "$out/$name-race.jsonl" 2>&1 || { cat "$out/$name-race.jsonl"; exit 1; }
      cgo=0
      if [ "$name" = rdma ]; then cgo=1; fi
      CGO_ENABLED=$cgo go test -tags "embedding_vm,$tags" -c -o "$out/bin/$name.test" ./pkg/linkmonitor
    done
    echo 'Running embedding vet and comprehensive lint'
    go vet ./pkg/xtcp > "$out/vet.log" 2>&1 || { cat "$out/vet.log"; exit 1; }
    golangci-lint run --config .golangci-comprehensive.yml ./pkg/xtcp > "$out/lint.log" 2>&1 || { cat "$out/lint.log"; exit 1; }
    golangci-lint run --config .golangci-comprehensive.yml --build-tags embedding_vm ./pkg/linkmonitor/... > "$out/guest-lint.log" 2>&1 || { cat "$out/guest-lint.log"; exit 1; }
    ln -s ${rdma.runtime} "$out/runtime"
  ''
