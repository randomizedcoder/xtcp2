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
    ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2

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

    cp result.log $out
  ''
