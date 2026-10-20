# nix/checks/vm-check-runner.nix
#
# Hermetic check for nix/microvms/mkVmCheck.nix's runner and app, with shell
# stand-ins for the guest: no VM boots. Each row builds a runner around its
# own launch text, runs it into a fresh evidence directory and asserts the
# exit code, the summary and, where it matters, the evidence left behind.
# The guests themselves are covered by the vm-* checks that use the runner.
{ pkgs, lib }:

let
  mkVmCheck = import ../microvms/mkVmCheck.nix { inherit pkgs lib; };
  rows = [
    {
      id = "pass";
      category = "positive";
      description = "every sentinel present, no failure line";
      launch = "echo GUEST_A_PASS; echo GUEST_B_PASS";
      want = "PASS: t-pass: 2 sentinels";
      wantRc = 0;
    }
    {
      id = "escapes";
      category = "positive";
      description = "CSI, OSC and DCS queries, ESC c/7/8, colours and CRs are stripped from serial.log";
      launch = ''printf '\033c\033[6n\033[18t\033]0;title\007\033P+q6E616D65\033\\\0337\033[1;32mGUEST_A_PASS\033[0m\0338\r\nGUEST_B_PASS\r\n' '';
      want = "PASS: t-escapes";
      wantRc = 0;
      expectFiles = ''! grep -q "$(printf '[\033\r]')" "$dir/serial.log" && grep -qx GUEST_A_PASS "$dir/serial.log"'';
    }
    {
      id = "missing";
      category = "negative";
      description = "a sentinel the guest never printed fails the run, by name";
      launch = "echo GUEST_A_PASS";
      want = "missing sentinel(s): GUEST_B_PASS";
      wantRc = 1;
    }
    {
      id = "failline";
      category = "negative";
      description = "a line matching the fail pattern fails even with every sentinel";
      launch = "echo GUEST_A_PASS; echo GUEST_ROW_FAIL x; echo GUEST_B_PASS";
      want = "the log matches";
      wantRc = 1;
    }
    {
      id = "exit";
      category = "negative";
      description = "the launcher's non-zero exit fails the run";
      launch = "echo GUEST_A_PASS; echo GUEST_B_PASS; exit 3";
      want = "the launcher exited 3";
      wantRc = 1;
    }
    {
      id = "accel";
      category = "negative";
      description = "an unknown accelerator is a usage error";
      launch = "echo GUEST_A_PASS; echo GUEST_B_PASS";
      accel = "hvf";
      want = "-";
      wantRc = 2;
    }
    {
      id = "empty";
      category = "boundary";
      description = "a guest that writes nothing fails before the sentinel check";
      launch = "true";
      want = "the guest wrote nothing";
      wantRc = 1;
    }
    {
      id = "timeout";
      category = "boundary";
      description = "a guest still running at the timeout is killed and fails";
      launch = "echo GUEST_A_PASS; echo GUEST_B_PASS; sleep 30";
      timeout = "2s";
      want = "timed out after 2s";
      wantRc = 1;
    }
    {
      id = "stdin";
      category = "corner";
      description = "the guest reads /dev/null, not the caller's stdin (SIGTTIN)";
      launch = ''if [ "$(readlink /proc/self/fd/0)" = /dev/null ]; then echo GUEST_A_PASS; fi; echo GUEST_B_PASS'';
      want = "PASS: t-stdin";
      wantRc = 0;
    }
    {
      id = "env";
      category = "corner";
      description = "scratch goes to the work dir, which is removed; QEMU_OPTS and VM_EVIDENCE are set";
      launch = ''
        [ "$TMPDIR" = "$PWD" ] && [ "''${PWD##*/}" = work ] && echo GUEST_A_PASS
        [ "$QEMU_OPTS" = '-machine accel=tcg' ] && [ -d "$VM_EVIDENCE" ] && echo GUEST_B_PASS
        touch scratch
      '';
      accel = "tcg";
      want = "PASS: t-env";
      wantRc = 0;
      expectFiles = ''[ ! -e "$dir/work" ] && [ -s "$dir/launcher.txt" ] && [ -s "$dir/summary.txt" ]'';
    }
    {
      id = "postcheck";
      category = "corner";
      description = "postCheck's fail fails a run whose sentinels all passed";
      launch = "echo GUEST_A_PASS; echo GUEST_B_PASS";
      postCheck = ''[ -s "$run_dir/serial.log" ] && fail 'post check said no' '';
      want = "post check said no";
      wantRc = 1;
    }
  ];
  runnerOf =
    row:
    (mkVmCheck {
      name = "t-${row.id}";
      inherit (row) launch;
      sentinels = [
        "GUEST_A_PASS"
        "GUEST_B_PASS"
      ];
      failPattern = "GUEST_(ROW_)?FAIL";
      timeout = row.timeout or "20s";
      postCheck = row.postCheck or "";
    }).runner;
  appOf =
    (mkVmCheck {
      name = "t-app";
      launch = "echo GUEST_A_PASS";
      sentinels = [ "GUEST_A_PASS" ];
      failPattern = "GUEST_FAIL";
    }).app;
in
pkgs.runCommand "xtcp2-vm-check-runner"
  {
    nativeBuildInputs = with pkgs; [
      coreutils
      gnugrep
      findutils
    ];
  }
  ''
    fails=0
    # row <category> <description> <runner> <accel> <want_rc> <want_summary|-> <assert>
    row() {
      local cat=$1 desc=$2 runner=$3 accel=$4 want_rc=$5 want=$6 assert=$7 dir rc=0
      dir=$(mktemp -d)/evidence
      "$runner" "$dir" "$accel" > "$dir.out" 2>&1 || rc=$?
      if [ "$rc" -ne "$want_rc" ]; then
        echo "FAIL $cat: $desc: rc=$rc (want $want_rc)"; cat "$dir.out"; fails=$((fails + 1))
      elif [ "$want" != - ] && ! grep -qF -- "$want" "$dir.out"; then
        echo "FAIL $cat: $desc: output lacks '$want':"; cat "$dir.out"; fails=$((fails + 1))
      elif ! (eval "$assert"); then
        echo "FAIL $cat: $desc: assertion failed: $assert"; fails=$((fails + 1))
      else
        echo "ok   $cat: $desc"
      fi
    }
    ${lib.concatMapStrings (row: ''
      row ${
        lib.escapeShellArgs [
          row.category
          row.description
          "${runnerOf row}/bin/vm-run-t-${row.id}"
          (row.accel or "kvm")
          (toString row.wantRc)
          row.want
          (row.expectFiles or "true")
        ]
      }
    '') rows}

    # The app: evidence under $XDG_CACHE_HOME, and only the newest --keep runs.
    export XDG_CACHE_HOME=$PWD/cache
    base=$XDG_CACHE_HOME/xtcp2-vm/t-app
    mkdir -p "$base"/20000101T00000{1,2,3,4,5,6}Z
    if ${appOf}/bin/test-t-app-vm-check --accel=tcg --keep=2 > app.out 2>&1 &&
      grep -q 'PASS: t-app' app.out &&
      [ "$(find "$base" -mindepth 1 -maxdepth 1 -type d | wc -l)" -eq 2 ] &&
      [ ! -e "$base/20000101T000005Z" ] && [ -e "$base/20000101T000006Z" ]; then
      echo "ok   corner: the app keeps the newest --keep runs under XDG_CACHE_HOME"
    else
      echo "FAIL corner: the app keeps the newest --keep runs under XDG_CACHE_HOME"; cat app.out; ls "$base"
      fails=$((fails + 1))
    fi
    if ${appOf}/bin/test-t-app-vm-check --keep=0 > /dev/null 2>&1; then
      echo "FAIL negative: --keep=0 is refused"; fails=$((fails + 1))
    else
      echo "ok   negative: --keep=0 is refused"
    fi

    if [ "$fails" -ne 0 ]; then
      echo "$fails row(s) failed"
      exit 1
    fi
    touch "$out"
  ''
