# nix/redteam.nix
#
# Red-team harness wiring. The program lives in docs/redteam-plan.md (threat
# model + scenario catalog) and docs/redteam-implementation.md (phases and
# definition of done); the build decisions here are documented in
# docs/redteam-harness.md. What this file adds:
#
#   packages.redteam-oci                the oci-xtcp2 image as a single .tar.gz
#                                        (streamLayeredImage output is a script,
#                                        not a file — see D2 in the harness doc)
#   packages.redteam-oci-tcp-stress     the tcp_server/tcp_client image the RT-11
#                                        victim work population runs, same form
#   packages.redteam-attacker-tools     tar of static xtcp2 client binaries;
#                                        refuses to build if any binary stops
#                                        being statically linked (a dynamic ELF
#                                        would not run on the Ubuntu guests)
#   apps.redteam-rt-<family>-<num>      one per vagrant/scenarios/rt-*.sh
#   apps.redteam-{destroy,status,lint}  harness lifecycle helpers
#   checks.redteam-lint                 hermetic shellcheck of vagrant/**.sh —
#                                        the only red-team member of
#                                        `nix flake check`; VM scenarios need
#                                        KVM, libvirtd and the network, so they
#                                        are apps, not checks
#
# Runners execute from the repo root on the harness host (l) and require
# /dev/kvm, a reachable qemu:///system and the network (vagrant box download).
# They run the scenario script from the *working tree* ($PWD), not the store
# copy — same `path:.`-vs-`.` reasoning as focused-quality in nix/default.nix:
# WIP scenarios are only discoverable when the flake source includes them, so
# use `nix run path:.#redteam-…` during development.
#
{
  pkgs,
  lib,
  src, # full tree — goSource's Go-only filter must not strip vagrant/
  # vagrant with the vagrant-libvirt plugin bundled (the nixpkgs withLibvirt
  # default on Linux). Unfree (bsl11): nix/default.nix passes an allowUnfree
  # instance scoped to these apps, so nothing else in the flake needs it.
  vagrant,
  xtcp2client, # binaries.xtcp2client (static, CGO_ENABLED=0)
  xtcp2ctl, # binaries.xtcp2ctl     (static, CGO_ENABLED=0)
  oci-xtcp2, # containers.oci-xtcp2: streamLayeredImage script
  oci-xtcp2-tcp-stress, # containers.oci-xtcp2-tcp-stress, same form
}:

let
  # Materialize a streamLayeredImage (a script that streams the tar to stdout)
  # as one deterministic .tar.gz file. gzip -n keeps it byte-stable so the
  # runner's scp-upload decision (same store path = skip) stays cheap.
  streamToTarGz =
    name: image:
    pkgs.runCommand name { nativeBuildInputs = [ pkgs.gzip ]; } ''
      ${image} | gzip -n > $out
    '';

  redteamOci = streamToTarGz "xtcp2-oci.tar.gz" oci-xtcp2;
  redteamTcpStressOci = streamToTarGz "xtcp2-tcp-stress.tar.gz" oci-xtcp2-tcp-stress;

  # Static client binaries the attacker VM and the co-tenant attacker container
  # run. The build-time linkage assertion is the whole point: these unpack on
  # stock Ubuntu, where a dynamic ELF would die on a missing /nix/store loader.
  attackerToolsBinaries = {
    inherit xtcp2client xtcp2ctl;
  };
  attackerTools =
    pkgs.runCommand "xtcp2-redteam-attacker-tools.tar"
      {
        nativeBuildInputs = [
          pkgs.file
          pkgs.gnutar
        ];
      }
      ''
        set -eu
        # nixpkgs' grpcurl is dynamically linked and deliberately NOT here; the
        # gRPC scenarios get a static build of their own when Phase 1 needs them
        # (see docs/redteam-harness.md "Open questions").
        for b in ${
          lib.concatStringsSep " " (lib.mapAttrsToList (_: drv: "${drv}/bin/*") attackerToolsBinaries)
        }; do
          file "$b" | grep -q 'statically linked' || {
            echo "redteam-attacker-tools: $b is not a static ELF — it would not run on the Ubuntu guests" >&2
            exit 1
          }
        done
        mkdir -p xtcp2-redteam/bin
        for b in ${
          lib.concatStringsSep " " (
            lib.mapAttrsToList (name: drv: "${drv}/bin/${name}") attackerToolsBinaries
          )
        }; do
          cp "$b" "xtcp2-redteam/bin/$(basename "$b")"
        done
        tar -C xtcp2-redteam -cf $out .
      '';

  # vagrant/scenarios/rt-<family>-<num>-<slug>.sh -> rt-<family>-<num>
  scenarioId =
    file:
    let
      parts = lib.splitString "-" (lib.removeSuffix ".sh" (baseNameOf file));
    in
    "rt-${builtins.elemAt parts 1}-${builtins.elemAt parts 2}";

  scenariosDir = src + "/vagrant/scenarios";
  scenarioFiles =
    if lib.pathExists scenariosDir then lib.filesystem.listFilesRecursive scenariosDir else [ ];

  # One app per scenario. The tarball store paths are baked in as build-time
  # inputs (no run-time `nix build` inside the runner), the VM state lives in
  # vagrant/.gen (VAGRANT_CWD/VAGRANT_HOME), and the scenario script itself
  # runs from the working tree so WIP edits are picked up without a rebuild.
  mkScenarioRunner =
    file:
    let
      id = scenarioId file;
    in
    pkgs.writeShellApplication {
      name = "redteam-${id}";
      runtimeInputs = [
        pkgs.bash
        pkgs.coreutils
        vagrant
        pkgs.openssh
      ];
      text = ''
        if [ ! -f flake.nix ]; then
          echo "redteam-${id}: must be run from the xtcp2 repo root" >&2
          exit 2
        fi
        export XTCP2_REPO_ROOT="$PWD"
        export XTCP2_OCI_TAR="${redteamOci}"
        export XTCP2_TCP_STRESS_TAR="${redteamTcpStressOci}"
        export XTCP2_ATTACKER_TOOLS_TAR="${attackerTools}"
        export VAGRANT_CWD="$XTCP2_REPO_ROOT/vagrant"
        export VAGRANT_HOME="$VAGRANT_CWD/.gen/.vagrant.d"
        mkdir -p "$VAGRANT_HOME"
        exec bash "$XTCP2_REPO_ROOT/vagrant/scenarios/${baseNameOf file}"
      '';
    };

  scenarioApps = lib.listToAttrs (
    map (file: {
      name = "redteam-${scenarioId file}";
      value = {
        type = "app";
        program = "${mkScenarioRunner file}/bin/redteam-${scenarioId file}";
      };
    }) scenarioFiles
  );

  # The lint both `checks.redteam-lint` and `nix run .#redteam-lint` run —
  # one derivation so the check and the app cannot drift. Plain repo scripts
  # are NOT covered by writeShellApplication's build-time shellcheck, which
  # is why they need this pass; the nix-built runners are checked at build.
  redteamLint = pkgs.writeShellApplication {
    name = "redteam-lint";
    runtimeInputs = [
      pkgs.findutils
      pkgs.gnugrep
      pkgs.shellcheck
    ];
    text = ''
      echo "shellcheck vagrant/"
      find "${src}/vagrant" -name '*.sh' -print0 |
        xargs -0 shellcheck -x
      echo "bash -n vagrant/"
      find "${src}/vagrant" -name '*.sh' -print0 |
        xargs -0 -n1 bash -n
      echo "redteam-lint: OK"
    '';
  };
in
{
  packages = {
    redteam-oci = redteamOci;
    redteam-oci-tcp-stress = redteamTcpStressOci;
    redteam-attacker-tools = attackerTools;
  };

  apps = scenarioApps // {
    redteam-destroy = {
      type = "app";
      program = "${
        pkgs.writeShellApplication {
          name = "redteam-destroy";
          runtimeInputs = [
            pkgs.coreutils
            vagrant
          ];
          text = ''
            if [ ! -f flake.nix ]; then
              echo "redteam-destroy: must be run from the xtcp2 repo root" >&2
              exit 2
            fi
            export VAGRANT_CWD="$PWD/vagrant"
            export VAGRANT_HOME="$VAGRANT_CWD/.gen/.vagrant.d"
            ( cd "$VAGRANT_CWD" && vagrant destroy -f )
          '';
        }
      }/bin/redteam-destroy";
    };
    redteam-status = {
      type = "app";
      program = "${
        pkgs.writeShellApplication {
          name = "redteam-status";
          runtimeInputs = [
            vagrant
          ];
          text = ''
            if [ ! -f flake.nix ]; then
              echo "redteam-status: must be run from the xtcp2 repo root" >&2
              exit 2
            fi
            export VAGRANT_CWD="$PWD/vagrant"
            export VAGRANT_HOME="$VAGRANT_CWD/.gen/.vagrant.d"
            ( cd "$VAGRANT_CWD" && vagrant status )
          '';
        }
      }/bin/redteam-status";
    };
    redteam-lint = {
      type = "app";
      program = "${redteamLint}/bin/redteam-lint";
    };
  };

  checks = {
    redteam-lint = pkgs.runCommand "redteam-lint-check" { } ''
      ${redteamLint}/bin/redteam-lint
      touch $out
    '';
  };
}
