# nix/lib/ernic-pin.nix
#
# The rocm-ernic input must be upstream ROCm/rocm-ernic, pinned by rev and
# narHash, with the same values in flake.nix (the declaration), flake.lock
# (what nix fetched) and nix/upstream-pins.json (what check-upstream-pins
# compares with develop). verify returns one message per disagreement, so an
# empty list means the three agree. See docs/design-rocm-ernic-nics.md.
{ lib }:

let
  upstream = "ROCm/rocm-ernic";
  get = attrs: name: attrs.${name} or "ABSENT";
  slug = a: "${get a "owner"}/${get a "repo"}";
in
{
  # The three inputs to verify, read from a source tree.
  inputsFrom = root: {
    manifest = lib.importJSON (root + "/nix/upstream-pins.json");
    lock = lib.importJSON (root + "/flake.lock");
    declared = (import (root + "/flake.nix")).inputs.rocm-ernic;
  };

  verify =
    {
      manifest,
      lock,
      declared,
    }:
    let
      pinned = manifest.pins.rocm-ernic or null;
      locked = lock.nodes.rocm-ernic.locked or null;
      same =
        field:
        let
          values = [
            (get pinned field)
            (get declared field)
            (get locked field)
          ];
        in
        lib.optional (lib.unique values != [ (get pinned field) ]
        ) "rocm-ernic ${field}: manifest ${builtins.elemAt values 0}, flake.nix ${builtins.elemAt values 1}, flake.lock ${builtins.elemAt values 2}";
    in
    if pinned == null then
      [ "pins.rocm-ernic is absent from nix/upstream-pins.json" ]
    else if locked == null then
      [ "flake.lock has no rocm-ernic node" ]
    else
      lib.optional
        (
          slug declared != upstream || slug locked != upstream || (pinned.repo or "ABSENT") != upstream
        )
        "rocm-ernic must pin ${upstream}: flake.nix ${slug declared}, flake.lock ${slug locked}, manifest ${
          pinned.repo or "ABSENT"
        }"
      ++ same "rev"
      ++ same "narHash";
}
