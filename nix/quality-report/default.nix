# nix/quality-report/default.nix
#
# Pedantic code-quality aggregator: runs every static-analysis tool wired
# into the repo, never short-circuits on findings, and emits a single
# markdown report.
#
# Two consumers:
#   - `packages.quality-report` — the hermetic derivation. Produces
#     `result/quality-report.md` plus `result/raw/*` (the per-tool
#     captures, for tooling). Cached by Nix; same source → same hash.
#   - `apps.quality-report` — convenience wrapper exposed in
#     nix/default.nix; runs $out/bin/quality-report which cats the
#     rendered markdown.
#
# Why not depend on the existing `nix/checks/*.nix` derivations:
#   They exit non-zero on findings, which would short-circuit any
#   derivation built from them. The orchestrator re-invokes each tool
#   command with the same flags those check derivations use, but lets
#   non-zero exits live in the captured raw output rather than aborting.
#
{
  pkgs,
  lib,
  vendoredSource,
  src,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-quality-report"
  {
    nativeBuildInputs = [
      versions.go
      versions.golangci-lint
      versions.gosec
      versions.nixfmt
      pkgs.coreutils
      pkgs.findutils
      pkgs.gnugrep
      pkgs.gnused
      pkgs.gawk
    ];
    inherit vendoredSource src;
    # The header timestamp is the one piece of non-determinism we accept.
    # Pin it via SOURCE_DATE_EPOCH so two runs over the same source produce
    # byte-identical reports.
    SOURCE_DATE_EPOCH = "1700000000";
  }
  ''
    # The default nixpkgs builder script runs with `bash -e`, so a non-zero
    # exit from any of the tools below would abort the whole orchestrator
    # before the report could be generated. Disable errexit explicitly —
    # the report itself is the signal, not the script's exit code.
    set +e
    set -u
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2

    export HOME=$(mktemp -d)
    export GOPATH=$HOME/go
    export GOMODCACHE=$HOME/go/pkg/mod
    export GOCACHE=$HOME/go-build
    export GOPROXY=off
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor

    export RAW=$(mktemp -d)
    : > "$RAW/runtimes.txt"
    : > "$RAW/exit-codes.txt"
    : > "$RAW/stderr.log"

    # runtool <label> <outpath> -- <cmd> [args...]
    #
    # Runs <cmd>, capturing stdout+stderr into <outpath>. Records wall-time
    # and exit code into $RAW/{runtimes,exit-codes}.txt. Never propagates a
    # non-zero exit to the surrounding script — the report itself is the
    # signal, not the orchestrator's rc.
    runtool() {
      local label="$1" outpath="$2"
      shift 2
      [ "$1" = "--" ] && shift
      local start end rc
      start=$(date +%s)
      "$@" > "$outpath" 2>>"$RAW/stderr.log"
      rc=$?
      end=$(date +%s)
      echo "$label=$((end-start))" >> "$RAW/runtimes.txt"
      echo "$label=$rc" >> "$RAW/exit-codes.txt"
      return 0
    }

    # ── Header / version metadata ──────────────────────────────────────
    {
      printf 'go=%s\n' "$(go version | awk '{print $3}')"
      printf 'golangci-lint=%s\n' "$(golangci-lint --version 2>&1 | head -1 | awk '{print $4}')"
      printf 'gosec=%s\n' "$(gosec -version 2>&1 | head -1 | awk '{print $2}')"
      printf 'nixfmt=%s\n' "$(nixfmt --version 2>&1 | head -1 | awk '{print $NF}')"
    } > "$RAW/versions.txt"

    # No git in the sandbox; the update-quality-report wrapper outside Nix
    # can inject commit/branch when it copies into docs/.
    : > "$RAW/commit.txt"
    : > "$RAW/branch.txt"

    # ── golangci-lint × 3 tiers ────────────────────────────────────────
    # Mirrors nix/checks/golangci-lint{,-quick,-comprehensive}.nix; adds
    # --max-issues-per-linter=0 + --max-same-issues=0 so the report sees
    # the full set of findings rather than golangci-lint's default 50/3
    # cap. (CI keeps the defaults so it stays fast.)
    #
    # golangci-lint v2 replaced --out-format=json with --output.json.path,
    # and ALSO prints a short summary to stdout. Send the summary to a
    # separate `.summary` file so it doesn't collide with the JSON.
    runtool golangci-quick "$RAW/golangci-quick.summary" -- \
      golangci-lint run --config .golangci-quick.yml --timeout 60s \
      --max-issues-per-linter=0 --max-same-issues=0 \
      --output.json.path "$RAW/golangci-quick.json" ./...
    runtool golangci-standard "$RAW/golangci-standard.summary" -- \
      golangci-lint run --config .golangci.yml --timeout 5m \
      --max-issues-per-linter=0 --max-same-issues=0 \
      --output.json.path "$RAW/golangci-standard.json" ./...
    runtool golangci-comprehensive "$RAW/golangci-comprehensive.summary" -- \
      golangci-lint run --config .golangci-comprehensive.yml --timeout 15m \
      --max-issues-per-linter=0 --max-same-issues=0 \
      --output.json.path "$RAW/golangci-comprehensive.json" ./...

    # ── go vet ─────────────────────────────────────────────────────────
    # Mirrors nix/checks/go-vet.nix
    runtool govet "$RAW/govet.out" -- go vet ./...

    # ── gofmt ──────────────────────────────────────────────────────────
    # Mirrors nix/checks/gofmt.nix exclusions verbatim.
    gofmt_start=$(date +%s)
    gofmt -l . 2>&1 \
      | grep -v -E '(^vendor/|\.pb\.go$|\.pb\.gw\.go$|^gen/|^dart/|^python/)' \
      > "$RAW/gofmt.out" || true
    gofmt_end=$(date +%s)
    echo "gofmt=$((gofmt_end-gofmt_start))" >> "$RAW/runtimes.txt"
    echo "gofmt=0" >> "$RAW/exit-codes.txt"

    # ── gosec ──────────────────────────────────────────────────────────
    # Mirrors nix/checks/go-sec.nix exclusions verbatim.
    runtool gosec "$RAW/gosec.json" -- \
      gosec -exclude=G103,G115,G204,G304 -fmt=json ./...

    # ── nix-fmt ────────────────────────────────────────────────────────
    # Mirrors nix/checks/nix-fmt.nix
    nixfmt_start=$(date +%s)
    : > "$RAW/nix-fmt.out"
    find . -type f -name '*.nix' \
      -not -path './vendor/*' -not -path './.git/*' -not -path './build/*' \
      | while IFS= read -r f; do
          nixfmt --check "$f" 2>/dev/null || echo "$f" >> "$RAW/nix-fmt.out"
        done
    nixfmt_end=$(date +%s)
    echo "nix-fmt=$((nixfmt_end-nixfmt_start))" >> "$RAW/runtimes.txt"
    echo "nix-fmt=0" >> "$RAW/exit-codes.txt"

    # ── Custom audits ──────────────────────────────────────────────────
    runtool netlink-audit "$RAW/netlink-audit.out" -- \
      go run ./tools/netlink-audit -root pkg/xtcpnl
    runtool iouring-audit "$RAW/iouring-audit.out" -- \
      go run ./tools/iouring-audit -root pkg/io_uring
    runtool metrics-audit "$RAW/metrics-audit.out" -- \
      go run ./tools/metrics-audit -root .
    runtool proto-field-audit "$RAW/proto-field-audit.out" -- \
      go run ./tools/proto-field-audit -proto-root proto -go-root pkg

    # ── go test (some tests require KVM/netlink/caps; will fail in sandbox) ─
    runtool gotest "$RAW/gotest.json" -- go test -json -short ./...

    # ── cli-help-smoke is covered by nix flake check; leave empty here ──
    : > "$RAW/cli-help-smoke.out"

    # ── Aggregate ─────────────────────────────────────────────────────
    mkdir -p $out
    go run ./tools/quality-report \
      -raw-dir "$RAW" \
      -repo-root . \
      -known-failures ./tools/quality-report/known-failures.txt \
      > $out/quality-report.md 2>>"$RAW/stderr.log"

    mkdir -p $out/raw
    cp -r "$RAW"/. $out/raw/ || true

    mkdir -p $out/bin
    cat > $out/bin/quality-report <<EOF
    #!${pkgs.runtimeShell}
    exec ${pkgs.coreutils}/bin/cat $out/quality-report.md
    EOF
    chmod +x $out/bin/quality-report
  ''
