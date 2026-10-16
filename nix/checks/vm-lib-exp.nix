# nix/checks/vm-lib-exp.nix
#
# Hermetic check for nix/microvms/scripts/vm-lib.exp — the expect library the
# netlink capture driver uses to run commands in a microVM guest and get their
# exit status back.
#
# WHY THIS CAN BE A CHECK WHEN THE CAPTURE RUNNERS CANNOT
#
# Every microVM target in this repo is a runner rather than a check, because a
# check derivation has no /dev/kvm, no network and no write access to the
# working tree. None of that applies here. vm-lib.exp's whole job is to turn a
# byte stream carrying an interactive shell session into {exit status, output},
# and the guest side of that stream is a bash on a pty — which is equally
# available from a local `socat ... EXEC:bash,pty`. The sandbox's loopback
# interface is up, so the library can be driven end to end against a real
# shell with no VM at all.
#
# That matters for a practical reason. A regression in the marker protocol
# shows up in the VM as a capture step that times out, twenty minutes into a
# boot, with nothing in the transcript to say why. Here it shows up in about
# fifteen seconds with the failing row named.
#
# WHAT IT DOES NOT COVER, so a green result is not over-read: qemu's serial
# console, the guest's getty autologin, boot timing, and the base64 exfil path.
# Those are only exercised by `nix run .#microvm-x86_64-netlink-dump-capture`.
#
# addr-topology.exp rides on the same shell. Its procs run `xtcp2-nlcap` in
# the guest, so the shell gets a stub of that ahead of the real helper: the
# steps that need a namespace are logged rather than run, and wait-addr-flag,
# the one piece with logic of its own, runs for real against tables fed in
# by `seqread`. Building the real helper here also shellchecks it.
# addr-topology-test.exp's header has the details.
#
{
  pkgs,
  src,
}:

let
  nlcap = import ../microvms/netlink-capture.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-vm-lib-exp"
  {
    nativeBuildInputs = with pkgs; [
      bashInteractive
      coreutils
      expect
      gnugrep
      socat
    ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2

    # The stub and the feed reader, first on the PATH socat's bash inherits.
    mkdir -p stubbin feeds
    # Quoted heredocs: Nix has already put the store paths in, and nothing
    # else in them is for this shell to expand.
    cat >stubbin/xtcp2-nlcap <<'EOF'
    #!${pkgs.bashInteractive}/bin/bash
    case "$1" in
      ns-add|ns-del|topo|sysctl|note)
        printf '%s\n' "$*" >>"$STUB_LOG"
        if [ -n "''${STUB_FAIL:-}" ]; then
          case "$*" in *"$STUB_FAIL"*) echo "STUB_FAIL $*"; exit 1 ;; esac
        fi
        echo "STUB_OK $*"
        ;;
      *) exec ${nlcap}/bin/xtcp2-nlcap "$@" ;;
    esac
    EOF
    cat >stubbin/seqread <<'EOF'
    #!${pkgs.bashInteractive}/bin/bash
    n=$(cat "$1/n" 2>/dev/null || echo 0)
    n=$(( n + 1 ))
    echo "$n" >"$1/n"
    if [ -f "$1/$n" ]; then cat "$1/$n"; else cat "$1/last"; fi
    EOF
    chmod +x stubbin/*
    export PATH="$PWD/stubbin:$PATH"
    export STUB_LOG="$PWD/stub.log"
    export FEED_ROOT="$PWD/feeds"
    export NLCAP_OUT="$PWD/nlcap-out"

    # Fixed port: the sandbox gets a private network namespace, so nothing
    # else can be holding it and there is no reason to hunt for a free one.
    PORT=17767

    # `pty,setsid,ctty` is what makes this a faithful stand-in for the guest
    # console: bash only enables job control, line editing and its terminal
    # echo when stdin is a tty, and the guest's echo is precisely the thing
    # run_out has to skip exactly one line of. Running bash on a plain socket
    # instead would test a stream the library will never see.
    #
    # --norc --noprofile keeps the prompt and shell options out of the
    # nixpkgs bashrc's hands, so the test does not depend on distribution
    # defaults. The library does not match the prompt anyway; this only keeps
    # the transcript small.
    socat "TCP-LISTEN:$PORT,reuseaddr,fork" \
      EXEC:'bash --norc --noprofile -i',pty,setsid,ctty,stderr \
      >socat.log 2>&1 &
    socat_pid=$!

    # Wait for the listener rather than sleeping at it. The whole point of
    # vm-lib.exp is to stop guessing at readiness with sleeps; a check for it
    # should not open with one.
    ready=0
    for _ in $(seq 1 50); do
      if (exec 3<>/dev/tcp/127.0.0.1/$PORT) 2>/dev/null; then
        ready=1
        break
      fi
      sleep 0.1
    done
    if [ "$ready" -ne 1 ]; then
      echo "vm-lib-exp: socat never accepted on 127.0.0.1:$PORT" >&2
      cat socat.log >&2 || true
      exit 1
    fi

    rc=0
    expect nix/microvms/scripts/vm-lib-test.exp "$PORT" > result.log 2>&1 || rc=$?
    arc=0
    expect nix/microvms/scripts/addr-topology-test.exp "$PORT" > addr.log 2>&1 || arc=$?

    kill "$socat_pid" 2>/dev/null || true
    wait "$socat_pid" 2>/dev/null || true

    grep -E '^(PASS|FAIL|===)' result.log || true

    if [ "$rc" -ne 0 ] || ! grep -q 'VMLIB_TEST_PASS' result.log; then
      echo "" >&2
      echo "vm-lib-exp: vm-lib.exp regressed (expect exited $rc)." >&2
      echo "            The FAIL rows above name the broken behavior. Full" >&2
      echo "            transcript:" >&2
      cat result.log >&2
      exit 1
    fi

    grep -E '^(PASS|FAIL|===)' addr.log || true

    if [ "$arc" -ne 0 ] || ! grep -q 'ADDRTOPO_TEST_PASS' addr.log; then
      echo "" >&2
      echo "vm-lib-exp: addr-topology.exp regressed (expect exited $arc)." >&2
      echo "            Full transcript:" >&2
      cat addr.log >&2
      exit 1
    fi

    cat result.log addr.log >$out
  ''
