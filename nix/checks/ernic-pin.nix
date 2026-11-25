# nix/checks/ernic-pin.nix
#
# Pure-evaluation table test for nix/lib/ernic-pin.nix: the rocm-ernic input
# must be upstream ROCm/rocm-ernic, and its rev and narHash must agree across
# flake.nix, flake.lock and nix/upstream-pins.json. The first row runs the
# rule over the real files; the rest over edited copies.
{ pkgs, lib }:

let
  pin = import ../lib/ernic-pin.nix { inherit lib; };
  real = pin.inputsFrom ../..;
  rev = "bd902345b0f8f3863f535a0a09da9f21ce744737";
  narHash = "sha256-vwIFQjGK7rorBfCNwyq4kepVz68A4BJHag2X3y/T+xU=";
  good = {
    manifest = {
      pins.rocm-ernic = {
        repo = "ROCm/rocm-ernic";
        inherit rev narHash;
      };
    };
    lock.nodes.rocm-ernic.locked = {
      type = "github";
      owner = "ROCm";
      repo = "rocm-ernic";
      inherit rev narHash;
    };
    declared = {
      type = "github";
      owner = "ROCm";
      repo = "rocm-ernic";
      inherit rev narHash;
    };
  };
  other = "0000000000000000000000000000000000000000";
  failures = lib.runTests {
    # name: ernic_matches. The committed flake.nix, flake.lock and manifest agree.
    "test positive: the real files agree" = {
      expr = pin.verify real;
      expected = [ ];
    };
    # name: synthetic_ok. Three identical declarations pass.
    "test positive: matching rev and narHash everywhere" = {
      expr = pin.verify good;
      expected = [ ];
    };
    # name: rev_mismatch.
    "test negative: the manifest rev differs from the lock" = {
      expr = pin.verify (lib.recursiveUpdate good { manifest.pins.rocm-ernic.rev = other; });
      expected = [
        "rocm-ernic rev: manifest ${other}, flake.nix ${rev}, flake.lock ${rev}"
      ];
    };
    # name: narhash_mismatch.
    "test negative: the declared narHash differs from the lock" = {
      expr = pin.verify (lib.recursiveUpdate good { declared.narHash = "sha256-x"; });
      expected = [
        "rocm-ernic narHash: manifest ${narHash}, flake.nix sha256-x, flake.lock ${narHash}"
      ];
    };
    # name: no_declared_narhash. A url-only input has no narHash to compare.
    "test negative: flake.nix declares no narHash" = {
      expr = pin.verify (good // { declared = removeAttrs good.declared [ "narHash" ]; });
      expected = [
        "rocm-ernic narHash: manifest ${narHash}, flake.nix ABSENT, flake.lock ${narHash}"
      ];
    };
    # name: missing_pin.
    "test boundary: the manifest has no rocm-ernic entry" = {
      expr = pin.verify (good // { manifest.pins = { }; });
      expected = [ "pins.rocm-ernic is absent from nix/upstream-pins.json" ];
    };
    # name: missing_lock. The input was dropped from flake.lock.
    "test boundary: flake.lock has no rocm-ernic node" = {
      expr = pin.verify (good // { lock.nodes = { }; });
      expected = [ "flake.lock has no rocm-ernic node" ];
    };
    # name: fork_url. Back on a fork is a regression this check exists for.
    "test corner: a fork owner is rejected" = {
      expr = pin.verify (
        lib.recursiveUpdate good {
          declared.owner = "randomizedcoder";
          lock.nodes.rocm-ernic.locked.owner = "randomizedcoder";
        }
      );
      expected = [
        "rocm-ernic must pin ROCm/rocm-ernic: flake.nix randomizedcoder/rocm-ernic, flake.lock randomizedcoder/rocm-ernic, manifest ROCm/rocm-ernic"
      ];
    };
    # name: two_faults. Every disagreement is reported, not just the first.
    "test corner: rev and narHash both differ" = {
      expr = builtins.length (
        pin.verify (
          lib.recursiveUpdate good {
            manifest.pins.rocm-ernic = {
              rev = other;
              narHash = "sha256-y";
            };
          }
        )
      );
      expected = 2;
    };
  };
in
if failures != [ ] then
  throw "ernic-pin: ${builtins.toJSON failures}"
else
  pkgs.runCommand "xtcp2-ernic-pin" { } "touch $out"
