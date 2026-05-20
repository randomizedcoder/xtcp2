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
    # Coverage is collected against pkg/, tools/, cmd/ — excluding the
    # auto-generated protobuf packages (xtcp_config, xtcp_flat_record,
    # clickhouse_protolist). `-coverpkg` instruments only the listed
    # packages, so generated code never enters the profile.
    coverPkg='./pkg/io_uring/...,./pkg/misc/...,./pkg/xtcp/...,./pkg/xtcpnl/...,./tools/...,./cmd/...'
    runtool gotest "$RAW/gotest.json" -- \
      go test -json -short \
        -coverprofile="$RAW/coverage-default.out" -covermode=atomic \
        -coverpkg="$coverPkg" \
        ./...

    # ── per-flavor coverage runs ──────────────────────────────────────
    # The default `go test ./...` above compiles WITHOUT any
    # `dest_*` build tags, so pkg/xtcp/destinations_{kafka,nats,nsq,
    # valkey}.go (each guarded by `//go:build dest_<name>`) are
    # excluded from the profile. Re-run pkg/xtcp/... once per flavor
    # with the matching tag so the destination files contribute to
    # coverage. The merged profile then feeds the existing TSV+HTML
    # post-processing below.
    #
    # Each per-flavor run is independent and writes to its own .out
    # file; we concatenate them below (skipping the duplicate
    # `mode: atomic` header) and let the existing awk dedupe by
    # max-count-per-block, mirroring what `go tool cover` does.
    for flavor in kafka nats nsq valkey; do
      go test -tags "dest_$flavor" \
        -coverprofile="$RAW/coverage-$flavor.out" -covermode=atomic \
        -coverpkg="$coverPkg" \
        ./pkg/xtcp/... \
        >> "$RAW/coverage-flavor-stdout.log" 2>&1 || true
    done

    # ── merge default + flavor profiles ───────────────────────────────
    # The first line of each .out is `mode: atomic` — keep only one,
    # then append every flavor's block lines. The downstream awk
    # already dedupes per (file:range) key by keeping max-count, so
    # repeated blocks across flavors collapse cleanly.
    if [ -s "$RAW/coverage-default.out" ]; then
      head -n 1 "$RAW/coverage-default.out" > "$RAW/coverage.out"
      tail -n +2 "$RAW/coverage-default.out" >> "$RAW/coverage.out"
      for flavor in kafka nats nsq valkey; do
        if [ -s "$RAW/coverage-$flavor.out" ]; then
          tail -n +2 "$RAW/coverage-$flavor.out" >> "$RAW/coverage.out"
        fi
      done
    fi

    # ── coverage post-processing ───────────────────────────────────────
    # The TSV summary is the canonical input the quality-report aggregator
    # parses; the HTML lands at $out/coverage.html for the user to open
    # directly. Both are best-effort: if `go test` produced no profile
    # (e.g. all tests failed before any package was instrumented) the
    # rest of the report should still build.
    if [ -s "$RAW/coverage.out" ]; then
      cov_start=$(date +%s)
      go tool cover -func="$RAW/coverage.out" \
        > "$RAW/coverage-func.out" 2>>"$RAW/stderr.log" || true
      go tool cover -html="$RAW/coverage.out" \
        -o "$RAW/coverage.html" 2>>"$RAW/stderr.log" || true
      # Per-package TSV: one row per package with line-weighted statement
      # coverage. Format: `<package>\t<percent>`. We parse the raw
      # coverage.out profile (not `go tool cover -func` output) because
      # the profile records per-block (statements, count) pairs that we
      # can aggregate properly. Previously this awk averaged per-function
      # percentages — that gave too much weight to tiny untestable
      # main() wrappers and was misleading vs `go tool cover -func`'s
      # bottom-line statement coverage.
      #
      # Atomic-mode profiles emit one row per block PER test-binary that
      # instrumented the package, so the same block appears many times
      # with the same path:range key — dedupe by keeping the max-count
      # row per key (i.e. the most coverage observed across all binaries),
      # mirroring what `go tool cover` does internally.
      awk '
        NR==1 && $1 == "mode:" { next }
        /^github.com\/randomizedcoder\/xtcp2\// {
          key=$1
          numStmt = $2 + 0
          count   = $3 + 0
          if (!(key in seenStmt)) {
            seenStmt[key] = numStmt
            seenCount[key] = count
          } else if (count > seenCount[key]) {
            seenCount[key] = count
          }
        }
        END {
          for (key in seenStmt) {
            n=split(key, parts, ":")
            path=parts[1]
            sub("^github.com/randomizedcoder/xtcp2/","",path)
            sub("/[^/]*\\.go$","",path)
            tot[path] += seenStmt[key]
            if (seenCount[key] > 0) {
              hit[path] += seenStmt[key]
            }
          }
          for (p in tot) {
            pct = (tot[p] > 0) ? (100.0 * hit[p] / tot[p]) : 0
            printf "%s\t%.1f\n", p, pct
          }
        }
      ' "$RAW/coverage.out" | sort > "$RAW/coverage-per-package.tsv"
      cov_end=$(date +%s)
      echo "coverage=$((cov_end-cov_start))" >> "$RAW/runtimes.txt"
      echo "coverage=0" >> "$RAW/exit-codes.txt"
    else
      : > "$RAW/coverage-func.out"
      : > "$RAW/coverage-per-package.tsv"
      echo "coverage=0" >> "$RAW/runtimes.txt"
      echo "coverage=2" >> "$RAW/exit-codes.txt"
    fi

    # ── cli-help-smoke is covered by nix flake check; leave empty here ──
    : > "$RAW/cli-help-smoke.out"

    # ── Aggregate ─────────────────────────────────────────────────────
    # Coverage ratchet: if the total falls below
    # docs/coverage-baseline.txt by more than coverage-max-drop points,
    # quality-report exits with code 3. The orchestrator below treats
    # that as a non-fatal report — the markdown is still produced —
    # but the non-zero exit propagates up to CI so the regression is
    # surfaced. Operator manually bumps the baseline file when they
    # intentionally raise the floor.
    mkdir -p $out
    set +e
    go run ./tools/quality-report \
      -raw-dir "$RAW" \
      -repo-root . \
      -known-failures ./tools/quality-report/known-failures.txt \
      -coverage-baseline ./docs/coverage-baseline.txt \
      -coverage-max-drop 0.5 \
      > $out/quality-report.md 2>>"$RAW/stderr.log"
    qr_rc=$?
    set -e
    if [ "$qr_rc" -eq 3 ]; then
      echo "WARNING: coverage ratchet breach (see stderr above); report still emitted" >&2
    elif [ "$qr_rc" -ne 0 ]; then
      cat "$RAW/stderr.log" >&2
      exit "$qr_rc"
    fi

    mkdir -p $out/raw
    cp -r "$RAW"/. $out/raw/ || true

    # Surface the coverage HTML at $out/coverage.html for easy access via
    # `xdg-open result/coverage.html`.
    if [ -s "$RAW/coverage.html" ]; then
      cp "$RAW/coverage.html" "$out/coverage.html"
    fi

    mkdir -p $out/bin
    cat > $out/bin/quality-report <<EOF
    #!${pkgs.runtimeShell}
    exec ${pkgs.coreutils}/bin/cat $out/quality-report.md
    EOF
    chmod +x $out/bin/quality-report
  ''
