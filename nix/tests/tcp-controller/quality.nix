# Focused Go checks with real disposable database fixtures. No VM or root needed.
{ pkgs, vendoredSource }:
let
  versions = import ../../versions.nix { inherit pkgs; };
  check =
    race:
    pkgs.runCommand "xtcp2-controller-${if race then "race" else "unit"}"
      {
        nativeBuildInputs = [
          versions.go
          pkgs.gcc
          pkgs.postgresql
        ];
      }
      ''
        cp -r ${vendoredSource} source
        chmod -R u+w source
        cd source
        export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=1
        export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
        mkdir -p "$out" "$TMPDIR/pg-socket"
        initdb -D "$TMPDIR/pg-data" -A trust --no-locale > "$out/postgres-init.log" 2>&1
        trap 'pg_ctl -D "$TMPDIR/pg-data" -m immediate -w stop >/dev/null 2>&1 || true' EXIT
        pg_ctl -D "$TMPDIR/pg-data" -l "$out/postgres.log" \
          -o "-k $TMPDIR/pg-socket -h ''' -p 55439" -w start
        export XTCP_CONTROLLER_TEST_DSN="host=$TMPDIR/pg-socket port=55439 dbname=postgres sslmode=disable"
        export XTCP_CLICKHOUSE_TEST_BINARY=${pkgs.clickhouse}/bin/clickhouse
        go test -ldflags=-checklinkname=0 ${
          if race then "-race -count=3" else "-count=1 -coverprofile=$out/coverage.out"
        } \
          -timeout 5m -v ./pkg/treatment/... ./tools/tcp-controller-integration \
          > "$out/tests.log" 2>&1 || { cat "$out/tests.log"; exit 1; }
        # These fixtures must execute, never silently skip in the focused checks.
        grep -q -- '--- PASS: TestPostgresConcurrentReviewFencesReplicas' "$out/tests.log"
        grep -q -- '--- PASS: TestClickHouseAnalysisCounterDeltas' "$out/tests.log"
        go test -ldflags=-checklinkname=0 ${if race then "-race -count=3" else "-count=1"} \
          -timeout 5m -v -run '^TestTreatment' ./internal/goip/actuate > "$out/routes.log" 2>&1 \
          || { cat "$out/routes.log"; exit 1; }
        ${if race then "" else ''go tool cover -func "$out/coverage.out" > "$out/coverage.txt"''}
      '';
in
{
  unit = check false;
  race = check true;
  bench = pkgs.runCommand "xtcp2-controller-bench" { nativeBuildInputs = [ versions.go ]; } ''
    cp -r ${vendoredSource} source
    chmod -R u+w source
    cd source
    export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
    export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
    mkdir -p "$out"
    go test -ldflags=-checklinkname=0 -run '^$' -bench . -benchmem -benchtime=1s -count=5 \
      ./pkg/treatment/... > "$out/benchmarks.txt" 2>&1 || { cat "$out/benchmarks.txt"; exit 1; }
    go test -ldflags=-checklinkname=0 -run '^$' -bench '^BenchmarkTreatment' -benchmem -benchtime=1s -count=5 \
      ./internal/goip/actuate >> "$out/benchmarks.txt" 2>&1 || { cat "$out/benchmarks.txt"; exit 1; }
  '';
}
