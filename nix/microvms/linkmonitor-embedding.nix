# Minimal disposable guest. QEMU's pinned launcher supports KVM and TCG.
{
  pkgs,
  lib,
  nixpkgs,
  artifact,
}:
let
  namespaceTest = pkgs.writeShellApplication {
    name = "linkmonitor-embedding-namespace";
    runtimeInputs = with pkgs; [
      iproute2
      util-linux
    ];
    text = ''
      mount --make-rprivate /
      mount -t sysfs sysfs /sys
      ip link set lo up
      exec "$1" -test.run '^TestEmbedding(VirtualLinux|HostProcess)$' -test.v -test.timeout=180s
    '';
  };
  guestTest = pkgs.writeShellApplication {
    name = "linkmonitor-embedding-guest";
    runtimeInputs = with pkgs; [
      coreutils
      iproute2
      util-linux
      systemd
    ];
    text = ''
      trap 'echo LINKMONITOR_EMBEDDING_FAIL; systemctl poweroff --no-block' ERR
      for variant in core rdma; do
        # New network, mount and PID namespaces; procfs/sysfs match socket scope.
        unshare --net --mount --pid --fork --mount-proc \
          ${namespaceTest}/bin/linkmonitor-embedding-namespace "${artifact}/bin/$variant.test"
        echo "LINKMONITOR_EMBEDDING_$variant"_PASS
      done
      echo LINKMONITOR_EMBEDDING_PASS
      systemctl poweroff --no-block
    '';
  };
  vm =
    (nixpkgs.lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        "${nixpkgs}/nixos/modules/virtualisation/qemu-vm.nix"
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          networking.hostName = "linkmonitor-embedding";
          networking.useDHCP = false;
          boot.kernelModules = [ "veth" ];
          virtualisation = {
            graphics = false;
            memorySize = 2048;
            cores = 2;
            diskImage = null;
            qemu.networkingOptions = lib.mkForce [ ];
            qemu.options = [ "-no-reboot" ];
          };
          systemd.services.linkmonitor-embedding = {
            wantedBy = [ "multi-user.target" ];
            after = [ "systemd-modules-load.service" ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guestTest}/bin/linkmonitor-embedding-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
in
pkgs.writeShellApplication {
  name = "test-linkmonitor-embedding-vm";
  runtimeInputs = with pkgs; [
    coreutils
    gnugrep
  ];
  text = ''
    accel=auto
    case "''${1:---accel=auto}" in
      --accel=auto) ;;
      --accel=kvm) accel=kvm ;;
      --accel=tcg) accel=tcg ;;
      *) echo 'usage: test-linkmonitor-embedding-vm [--accel=auto|kvm|tcg]' >&2; exit 2 ;;
    esac
    if [ "$#" -gt 1 ]; then echo 'too many arguments' >&2; exit 2; fi
    run_dir=$(mktemp -d -t linkmonitor-embedding-XXXXXX)
    echo "Evidence: $run_dir"
    printf '%s\n' '${artifact}' > "$run_dir/artifact.txt"
    printf '%s\n' '${vm}' > "$run_dir/guest.txt"
    printf '%s\n' "$accel" > "$run_dir/accelerator-request.txt"
    export TMPDIR="$run_dir"
    cd "$run_dir"
    case "$accel" in
      auto) export QEMU_OPTS='-machine accel=kvm:tcg' ;;
      kvm) export QEMU_OPTS='-machine accel=kvm' ;;
      tcg) export QEMU_OPTS='-machine accel=tcg' ;;
    esac
    printf '%s\n' "$QEMU_OPTS" > "$run_dir/qemu-options.txt"
    unset QEMU_KERNEL_PARAMS QEMU_NET_OPTS NIX_DISK_IMAGE
    timeout --kill-after=10s 15m ${vm}/bin/run-linkmonitor-embedding-vm > "$run_dir/serial.log" 2>&1 || {
      cat "$run_dir/serial.log"
      exit 1
    }
    cat "$run_dir/serial.log"
    for sentinel in core_PASS rdma_PASS PASS; do
      grep -q "LINKMONITOR_EMBEDDING_$sentinel" "$run_dir/serial.log"
    done
    if grep -E 'LINKMONITOR_EMBEDDING_FAIL|--- SKIP:|--- FAIL:' "$run_dir/serial.log"; then exit 1; fi
    echo "PASS: embedding VM; evidence $run_dir"
  '';
}
