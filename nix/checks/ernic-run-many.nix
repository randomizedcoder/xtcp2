# nix/checks/ernic-run-many.nix
#
# Table test for nix/ernic/run-many.nix with a fake rocm-ernic: the fake
# binds its --socket (or exits first when FAKE_ERNIC_FAIL names it, or
# later when FAKE_ERNIC_DIE_LATE does); on TERM it waits 0.5 s and prints a
# fault report, as rocm-ernic's clean shutdown does. Each row prints
#   ROW <name> <category> PASS|FAIL <description>: <detail>
# and any FAIL fails the check.
{ pkgs, lib }:

let
  fakeServer = pkgs.writeShellApplication {
    name = "rocm-ernic";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.python3
    ];
    text = ''
      socket=""
      while [ "$#" -gt 0 ]; do
        case "$1" in
          --socket) socket=$2; shift 2 ;;
          *) shift ;;
        esac
      done
      name=''${socket##*ernic-}
      name=''${name%.sock}
      if [ "''${FAKE_ERNIC_FAIL:-}" = "$name" ]; then
        echo "fake: $name failing before its socket" >&2
        exit 1
      fi
      late=0
      if [ "''${FAKE_ERNIC_DIE_LATE:-}" = "$name" ]; then late=1; fi
      # Like rocm-ernic, TERM starts a clean shutdown that takes a moment and
      # ends with the fault report.
      exec python3 -c '
      import signal, socket, sys, time
      def term(*_):
          time.sleep(0.5)
          print("  Fault eth_tx: eligible 7, fired 1", flush=True)
          sys.exit(0)
      signal.signal(signal.SIGTERM, term)
      s = socket.socket(socket.AF_UNIX)
      s.bind(sys.argv[1])
      s.listen(1)
      time.sleep(1 if sys.argv[2] == "1" else 3600)
      ' "$socket" "$late"
    '';
  };
  runMany = import ../ernic/run-many.nix {
    inherit pkgs lib;
    server = fakeServer;
  };
  nic = name: slot: {
    inherit name slot;
    mac = "02:00:00:00:20:${slot}";
  };
  two = runMany {
    name = "two";
    nics = [
      (nic "a" "0a")
      (nic "b" "0b")
    ];
  };
  none = runMany {
    name = "none";
    nics = [ ];
  };
in
pkgs.runCommand "xtcp2-ernic-run-many"
  {
    nativeBuildInputs = [
      pkgs.coreutils
      pkgs.procps
      pkgs.util-linux
    ];
  }
  ''
    fail=0
    row() { # name category description ok detail
      if [ "$4" = 1 ]; then r=PASS; else r=FAIL; fail=1; fi
      echo "ROW $1 $2 $r $3: $5"
    }
    fakes() { pgrep -fc 'python3 -c' || true; }
    TWO=${two}/bin/ernic-run-many-two
    NONE=${none}/bin/ernic-run-many-none

    d=$(mktemp -d)
    rc=0
    $TWO --dir "$d" -- sh -c 'test -S ernic-a.sock && test -S ernic-b.sock' || rc=$?
    row all_up positive "every socket exists before the command" "$([ $rc = 0 ] && echo 1)" "rc=$rc"

    d=$(mktemp -d)
    rc=0
    $TWO --dir "$d" -- sh -c 'exit 3' || rc=$?
    n=$(fakes)
    row exit_status positive "the command's status passes through" "$([ $rc = 3 ] && [ "$n" = 0 ] && echo 1)" "rc=$rc fakes=$n"

    d=$(mktemp -d)
    got=$($TWO --dir "$d" -- pwd)
    row cwd positive "the command runs in --dir" "$([ "$got" = "$d" ] && echo 1)" "pwd=$got"

    d=$(mktemp -d)
    rc=0
    err=$(FAKE_ERNIC_FAIL=b $TWO --dir "$d" -- true 2>&1) || rc=$?
    row server_dies negative "an early server exit fails the run" "$([ $rc != 0 ] && [[ $err == *"b exited before creating its socket"* ]] && echo 1)" "rc=$rc err=$err"

    d=$(mktemp -d)
    rc=0
    err=$(FAKE_ERNIC_DIE_LATE=a $TWO --dir "$d" -- sleep 30 2>&1) || rc=$?
    row server_dies_late negative "a server exit while the command runs fails the run" "$([ $rc != 0 ] && [[ $err == *"a exited while the command ran"* ]] && echo 1)" "rc=$rc err=$err"

    d=$(mktemp -d)
    touch "$d/ernic-a.sock"
    rc=0
    err=$($TWO --dir "$d" -- true 2>&1) || rc=$?
    row stale_socket negative "an existing socket is refused" "$([ $rc != 0 ] && [[ $err == *"already exists"* ]] && echo 1)" "rc=$rc err=$err"

    d=$(mktemp -d)
    rc=0
    got=$($NONE --dir "$d" -- echo ran) || rc=$?
    row zero_nics boundary "no NICs just runs the command" "$([ $rc = 0 ] && [ "$got" = ran ] && echo 1)" "rc=$rc out=$got"

    d=$(mktemp -d)
    $TWO --dir "$d" -- sleep 30 &
    pid=$!
    for _ in $(seq 1 100); do [ -S "$d/ernic-b.sock" ] && break; sleep 0.1; done
    sleep 0.5
    kill -TERM "$pid"
    rc=0
    wait "$pid" || rc=$?
    sleep 0.5
    n=$(fakes)
    row sigterm corner "TERM stops the command and every server" "$([ $rc = 143 ] && [ "$n" = 0 ] && echo 1)" "rc=$rc fakes=$n"

    d=$(mktemp -d)
    $TWO --dir "$d" -- true
    got=$(cat "$d/ernic-a.log" "$d/ernic-b.log" 2>/dev/null | grep -c 'Fault eth_tx: eligible 7, fired 1' || true)
    row clean_stop positive "each server finishes its clean shutdown, so its fault report reaches its log" "$([ "$got" = 2 ] && echo 1)" "reports=$got"

    d=$(mktemp -d)
    rc=0
    err=$($TWO --dir "$d" --pcap eth9 -- true 2>&1) || rc=$?
    row pcap_unknown negative "--pcap names an interface the fabric does not have" "$([ $rc = 2 ] && [[ $err == *"--pcap eth9 is not a fabric bridge or TAP"* ]] && echo 1)" "rc=$rc err=$err"

    d=$(mktemp -d)
    rc=0
    err=$($NONE --dir "$d" --pcap br0 -- true 2>&1) || rc=$?
    row pcap_no_fabric negative "--pcap without any fabric is refused" "$([ $rc = 2 ] && [[ $err == *"--pcap br0 is not a fabric bridge or TAP"* ]] && echo 1)" "rc=$rc err=$err"

    d=$(mktemp -d)
    mine=$(readlink /proc/self/ns/net)
    got=$($TWO --dir "$d" -- readlink /proc/self/ns/net)
    row outside_netns corner "the command stays in the caller's netns" "$([ "$got" = "$mine" ] && echo 1)" "caller=$mine command=$got"

    [ "$fail" = 0 ] || exit 1
    touch $out
  ''
