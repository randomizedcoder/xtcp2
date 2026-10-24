# One runner for every guest test that reports through its serial console,
# whatever builds the guest (nixpkgs qemu-vm.nix or microvm.nix). It gives:
#
#   runner  vm-run-<name> EVIDENCE_DIR [auto|kvm|tcg]: launch the guest with
#           stdin from /dev/null under a timeout, write serial.log (terminal
#           escapes and carriage returns stripped) and summary.txt into
#           EVIDENCE_DIR, and print only the summary. Exit 0 pass, 1 fail.
#           $VM_KERNEL_PARAMS, if set, becomes the guest's QEMU_KERNEL_PARAMS.
#   check   a derivation that runs the runner in the build sandbox with KVM
#           (requiredSystemFeatures), keeping serial.log and summary.txt in
#           $out. Nix caches a pass by its inputs; a failure prints the log's
#           tail and keeps nothing (nix build --keep-failed keeps the tree).
#   app     test-<name>-vm [--accel=auto|kvm|tcg] [--keep=N]: the same
#           run by hand, its evidence under
#           ${XDG_CACHE_HOME:-~/.cache}/xtcp2-vm/<name>/<UTC time>, the last
#           N (default 5) kept. Nothing goes to /tmp and the console gets
#           only the summary, plus the log's tail on a failure.
#
# The guest's scratch (TMPDIR, qemu-vm.nix's xchg, sockets) lives in a short
# work directory, removed on exit, under $XDG_RUNTIME_DIR, else $TMPDIR (the
# build sandbox's /build), else /tmp. Not beside the evidence: qemu and
# virtiofsd bind Unix sockets in it, sun_path holds 108 bytes, and qemu-vm.nix
# nests nix-vm.XXXXXXXXXX/tmp.XXXXXXXXXX under TMPDIR, which a check's
# /nix/store/<hash>-vm-check-<name> evidence directory pushed past the limit.
{ pkgs, lib }:
{
  # Used in the derivation, runner and app names.
  name,
  # Shell text that starts the guest. It runs in the work directory with
  # QEMU_OPTS set, $VM_EVIDENCE naming the evidence directory, and stdout and
  # stderr captured as the serial log.
  launch,
  runtimeInputs ? [ ],
  # Fixed strings, each of which must appear in the serial log.
  sentinels,
  # An extended regular expression; any matching line fails the run.
  failPattern,
  # A coreutils timeout duration for the whole guest run.
  timeout ? "15m",
  # Store paths recorded as <key>.txt in the evidence.
  evidence ? { },
  # Shell text run after the sentinels pass, with $run_dir set; it calls
  # fail MESSAGE to fail the run.
  postCheck ? "",
  requiredSystemFeatures ? [ "kvm" ],
  appName ? "test-${name}-vm",
}:
let
  stripEscapes = import ./stripEscapes.nix { inherit pkgs; };
  launcher = pkgs.writeShellApplication {
    name = "vm-launch-${name}";
    inherit runtimeInputs;
    text = launch;
  };
  runner = pkgs.writeShellApplication {
    name = "vm-run-${name}";
    runtimeInputs = with pkgs; [
      coreutils
      gnugrep
      gnused
    ];
    text = ''
      if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
        echo 'usage: vm-run-${name} EVIDENCE_DIR [auto|kvm|tcg]' >&2
        exit 2
      fi
      run_dir=$1
      case "''${2:-kvm}" in
        auto) qemu_opts='-machine accel=kvm:tcg' ;;
        kvm) qemu_opts='-machine accel=kvm' ;;
        tcg) qemu_opts='-machine accel=tcg' ;;
        *) echo "vm-run-${name}: accel must be auto, kvm or tcg" >&2; exit 2 ;;
      esac
      mkdir -p "$run_dir"
      run_dir=$(cd "$run_dir" && pwd)
      work=$(mktemp -d "''${XDG_RUNTIME_DIR:-''${TMPDIR:-/tmp}}/vm.XXXXXX")
      trap 'rm -rf -- "$work"' EXIT
      ${lib.concatStrings (
        lib.mapAttrsToList (key: path: ''
          printf '%s\n' ${lib.escapeShellArg "${path}"} > "$run_dir/${key}.txt"
        '') evidence
      )}
      printf '%s\n' '${launcher}' > "$run_dir/launcher.txt"
      printf '%s\n' "$qemu_opts" > "$run_dir/qemu-options.txt"
      kernel_params=''${VM_KERNEL_PARAMS:-}
      if [ -n "$kernel_params" ]; then
        printf '%s\n' "$kernel_params" > "$run_dir/kernel-params.txt"
      fi

      fail() {
        printf 'FAIL: ${name}: %s\n' "$1" | tee "$run_dir/summary.txt"
        exit 1
      }

      rc=0
      (
        cd "$work"
        export TMPDIR="$work" QEMU_OPTS="$qemu_opts" VM_EVIDENCE="$run_dir"
        unset QEMU_KERNEL_PARAMS QEMU_NET_OPTS NIX_DISK_IMAGE
        if [ -n "$kernel_params" ]; then
          export QEMU_KERNEL_PARAMS="$kernel_params"
        fi
        # stdin from /dev/null: timeout puts the guest in its own process
        # group, where a -nographic qemu reading a terminal is stopped
        # (SIGTTIN) before it prints anything.
        exec timeout --kill-after=10s ${timeout} ${launcher}/bin/vm-launch-${name}
      ) < /dev/null > "$work/serial.raw" 2>&1 || rc=$?
      ${stripEscapes}/bin/vm-strip-escapes "$work/serial.raw" > "$run_dir/serial.log"

      if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
        fail "timed out after ${timeout}"
      fi
      if [ "$rc" -ne 0 ]; then
        fail "the launcher exited $rc"
      fi
      if [ ! -s "$run_dir/serial.log" ]; then
        fail 'the guest wrote nothing'
      fi
      if grep -E -m 20 ${lib.escapeShellArg failPattern} "$run_dir/serial.log" > "$work/failures"; then
        cat "$work/failures"
        fail "the log matches ${lib.escapeShellArg failPattern}"
      fi
      missing=""
      sentinels=(${lib.escapeShellArgs sentinels})
      for sentinel in "''${sentinels[@]}"; do
        grep -qF -- "$sentinel" "$run_dir/serial.log" || missing="$missing $sentinel"
      done
      if [ -n "$missing" ]; then
        fail "missing sentinel(s):$missing"
      fi
      ${postCheck}
      printf 'PASS: ${name}: %s sentinels\n' ${toString (builtins.length sentinels)} | tee "$run_dir/summary.txt"
    '';
  };
  check =
    pkgs.runCommand "vm-check-${name}"
      {
        inherit requiredSystemFeatures;
        passthru = { inherit runner; };
      }
      ''
        if ! ${runner}/bin/vm-run-${name} "$out" kvm; then
          echo '--- the last 60 lines of serial.log ---'
          tail -n 60 "$out/serial.log" 2>/dev/null || true
          exit 1
        fi
      '';
  app = pkgs.writeShellApplication {
    name = appName;
    runtimeInputs = with pkgs; [
      coreutils
      findutils
    ];
    text = ''
      accel=auto
      keep=5
      for arg in "$@"; do
        case "$arg" in
          --accel=auto | --accel=kvm | --accel=tcg) accel=''${arg#--accel=} ;;
          --keep=*) keep=''${arg#--keep=} ;;
          *) echo 'usage: ${appName} [--accel=auto|kvm|tcg] [--keep=N]' >&2; exit 2 ;;
        esac
      done
      case "$keep" in
        "" | *[!0-9]* | 0) echo '--keep must be a positive integer' >&2; exit 2 ;;
      esac
      base="''${XDG_CACHE_HOME:-$HOME/.cache}/xtcp2-vm/${name}"
      run_dir="$base/$(date -u +%Y%m%dT%H%M%SZ)"
      mkdir -p "$base"
      rc=0
      ${runner}/bin/vm-run-${name} "$run_dir" "$accel" || rc=$?
      if [ "$rc" -ne 0 ] && [ -s "$run_dir/serial.log" ]; then
        echo '--- the last 40 lines of serial.log ---'
        tail -n 40 "$run_dir/serial.log"
      fi
      echo "evidence: $run_dir"
      find "$base" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort -r | tail -n +"$((keep + 1))" |
        while read -r old; do rm -rf -- "''${base:?}/$old"; done
      exit "$rc"
    '';
  };
in
{
  inherit runner check app;
}
