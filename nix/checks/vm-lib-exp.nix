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
# A SECOND DRIVER, ON THE SAME SHELL
#
# scripts/addr-topology-test.exp runs after vm-lib-test.exp and covers
# addr-topology.exp (docs/netlink/addr-write-path.md): v6hex, the
# wait_addr_flag wrapper, each address state's exact `ip` step list, the
# per-state namespace reset, and the required/measured failure handling. The
# REAL xtcp2-nlcap is on the shell's PATH, so wait-addr-flag, sysctl and note
# run as the guest runs them; NLCAP_OUT and NLCAP_IF_INET6 point its output
# and its /proc/net/if_inet6 at files in the build directory. It does not
# cover whether a kernel accepts the steps or whether DAD sets each flag -
# the sandbox has no namespaces - which the capture run records instead.
#
# AND A THIRD, WITH NO SHELL AT ALL
#
# scripts/addr-corpus-test.exp covers addr-corpus.exp: the address write,
# flush and save rows and the readback captures, checked against the
# design's slug lists written out again as literals, plus the two procs the
# capture driver takes from it (pre_step, fatal_refusals). It replaces
# vmlib::run_out with a recorder outright, so it needs no console. It does
# not cover what the guest runs or what the kernel answers;
# TestAddrCaptureCorpusComplete pins that in the committed fixtures.
#
# scripts/rtnl-write-corpus-test.exp does the same for rtnl-write-corpus.exp,
# the C1 rows (docs/netlink/goip-parity-gaps.md): slugs, readbacks, floors,
# expected statuses and the event rows' monitor lines. TestRtnlWriteCorpus
# pins the captured bytes.
#
# scripts/rtnl-read-corpus-test.exp does the same for rtnl-read-corpus.exp,
# the C1-T03 reads: names, floors, the namespace's build steps and the
# multicast route. TestRtnlReadCorpus pins the captured bytes.
#
{
  pkgs,
  src,
}:

pkgs.runCommand "xtcp2-vm-lib-exp"
  {
    nativeBuildInputs = with pkgs; [
      bashInteractive
      coreutils
      expect
      gnugrep
      socat
      (import ../microvms/netlink-capture.nix { inherit pkgs; })
    ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2

    # Fixed port: the sandbox gets a private network namespace, so nothing
    # else can be holding it and there is no reason to hunt for a free one.
    PORT=17767

    # The two xtcp2-nlcap seams, exported before socat so every shell it
    # forks inherits them. vm-lib-test.exp never runs the helper.
    export NLCAP_OUT=$PWD/nlcap
    export NLCAP_IF_INET6=$PWD/if_inet6

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
    crc=0
    expect nix/microvms/scripts/addr-corpus-test.exp > corpus.log 2>&1 || crc=$?
    rrc=0
    expect nix/microvms/scripts/rtnl-write-corpus-test.exp > rtnl.log 2>&1 || rrc=$?
    rdrc=0
    expect nix/microvms/scripts/rtnl-read-corpus-test.exp > rtnlread.log 2>&1 || rdrc=$?

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
      echo "            The FAIL rows above name the broken behavior. Full" >&2
      echo "            transcript:" >&2
      cat addr.log >&2
      exit 1
    fi

    grep -E '^(PASS|FAIL|===)' corpus.log || true

    if [ "$crc" -ne 0 ] || ! grep -q 'ADDRCORPUS_TEST_PASS' corpus.log; then
      echo "" >&2
      echo "vm-lib-exp: addr-corpus.exp regressed (expect exited $crc)." >&2
      echo "            The FAIL rows above name the broken behavior. Full" >&2
      echo "            transcript:" >&2
      cat corpus.log >&2
      exit 1
    fi

    grep -E '^(PASS|FAIL|===)' rtnl.log || true

    if [ "$rrc" -ne 0 ] || ! grep -q 'RTNLCORPUS_TEST_PASS' rtnl.log; then
      echo "" >&2
      echo "vm-lib-exp: rtnl-write-corpus.exp regressed (expect exited $rrc)." >&2
      echo "            The FAIL rows above name the broken behavior. Full" >&2
      echo "            transcript:" >&2
      cat rtnl.log >&2
      exit 1
    fi

    grep -E '^(PASS|FAIL|===)' rtnlread.log || true

    if [ "$rdrc" -ne 0 ] || ! grep -q 'RTNLREAD_TEST_PASS' rtnlread.log; then
      echo "" >&2
      echo "vm-lib-exp: rtnl-read-corpus.exp regressed (expect exited $rdrc)." >&2
      echo "            The FAIL rows above name the broken behavior. Full" >&2
      echo "            transcript:" >&2
      cat rtnlread.log >&2
      exit 1
    fi

    cat result.log addr.log corpus.log rtnl.log rtnlread.log > $out
  ''
