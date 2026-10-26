# Focused Go checks with real disposable database fixtures. No VM or root needed.
{ pkgs, vendoredSource }:
let
  versions = import ../../versions.nix { inherit pkgs; };
  # Source tree plus a disposable PostgreSQL on a Unix socket, exported as
  # XTCP_CONTROLLER_TEST_DSN for the store tests.
  postgresFixture = ''
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
  '';
  # Per-package statement coverage from the unit logs as a markdown table,
  # pasted into docs/tcp-controller-testing.md. actuate runs its treatment
  # tests only.
  coverageTable = ''
    go tool cover -func "$out/coverage.out" > "$out/coverage.txt"
    {
      echo "| Package | Statements covered |"
      echo "| --- | ---: |"
      cat "$out/tests.log" "$out/routes.log" | awk -F'\t' '/^ok/ && /coverage:/ {
        pkg = $2; sub("github.com/randomizedcoder/xtcp2/", "", pkg)
        match($0, /coverage: [0-9.]+%/)
        print "| `" pkg "` | " substr($0, RSTART + 10, RLENGTH - 10) " |"
      }'
    } > "$out/coverage-table.md"
  '';
  check =
    race:
    pkgs.runCommand "xtcp2-controller-${if race then "race" else "unit"}"
      {
        nativeBuildInputs = [
          versions.go
          pkgs.gcc
          pkgs.postgresql
          pkgs.clickhouse # clickhouse_test.go finds it on PATH
        ];
      }
      ''
        ${postgresFixture}
        go test -ldflags=-checklinkname=0 ${
          if race then "-race -count=3" else "-count=1 -coverprofile=$out/coverage.out"
        } \
          -timeout 5m -v ./pkg/treatment/... ./tools/tcp-controller-integration \
          > "$out/tests.log" 2>&1 || { cat "$out/tests.log"; exit 1; }
        # These fixtures must execute, never silently skip in the focused checks.
        grep -q -- '--- PASS: TestPostgresConcurrentReviewFencesReplicas' "$out/tests.log"
        grep -q -- '--- PASS: TestClickHouseAnalysisCounterDeltas' "$out/tests.log"
        go test -ldflags=-checklinkname=0 ${if race then "-race -count=3" else "-count=1 -cover"} \
          -timeout 5m -v -run '^TestTreatment' ./internal/goip/actuate > "$out/routes.log" 2>&1 \
          || { cat "$out/routes.log"; exit 1; }
        ${if race then "" else coverageTable}
      '';
in
{
  unit = check false;
  race = check true;
  # Query-plan timings for the discovery indexes, three runs; the source of
  # docs/benchmarks/tcp-controller/postgres-indexes.txt.
  postgres-indexes =
    pkgs.runCommand "xtcp2-controller-postgres-indexes"
      {
        nativeBuildInputs = [
          versions.go
          pkgs.gcc
          pkgs.postgresql
        ];
      }
      ''
        ${postgresFixture}
        {
          echo "# nix build .#bench-tcp-controller-postgres-indexes"
          echo "# $(postgres --version), $(go version)"
          go test -ldflags=-checklinkname=0 -count=3 -timeout 10m -v \
            -run '^TestPostgresExperimentQueryIndexes$' ./pkg/treatment/controller/store/postgres
        } > "$out/postgres-indexes.txt" 2>&1 || { cat "$out/postgres-indexes.txt"; exit 1; }
        grep -q -- '--- PASS: TestPostgresExperimentQueryIndexes' "$out/postgres-indexes.txt"
      '';
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
