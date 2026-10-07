# nix/tests/default.nix
#
# Aggregates behavioral test runners.
#
{
  pkgs,
  lib,
  src,
  vendoredSource,
  binaries,
  microvms,
}:

let
  focused = import ./focused-quality.nix { inherit pkgs lib vendoredSource; };
  ipmetaBootstrap = import ./ipmeta-bootstrap.nix {
    inherit
      pkgs
      lib
      src
      vendoredSource
      binaries
      ;
  };
in
{
  go-unit = import ./go-unit.nix { inherit pkgs vendoredSource; };
  go-bench = import ./go-bench.nix { inherit pkgs vendoredSource; };
  # No lib: that runner has no such argument any more. This file's own lib arg
  # stays — focused-quality.nix, both go-test-* imports and the mapAttrs' below
  # all need it.
  listener-security = import ./go-listener-security.nix { inherit pkgs vendoredSource; };
  proto-deserialize-golden = import ./proto-deserialize-golden.nix {
    inherit pkgs vendoredSource;
  };
  inherit focused;
  ipmeta-bootstrap-artifact = ipmetaBootstrap.artifact;
  oci-ipmeta-bootstrap-contents = ipmetaBootstrap.oci-contents;

  # Whole-repo race-detector test (cgo-enabled).
  go-race = import ./go-test-race.nix { inherit pkgs vendoredSource; };

  # Microvm lifecycle, per arch. The microvms input is the result of
  # `import ./nix/microvms { ... }`.
  microvm-lifecycle = microvms.lifecycle;
  microvm-lifecycle-ipmeta-bootstrap = microvms.lifecycleIpmetaBootstrap;
}
// (import ./go-test-flavors.nix { inherit pkgs lib vendoredSource; })
// (import ./go-test-per-package.nix { inherit pkgs lib vendoredSource; })
// (import ./linkmonitor.nix {
  inherit
    pkgs
    lib
    src
    vendoredSource
    ;
})
// (lib.mapAttrs' (name: value: {
  name = "test-${name}";
  inherit value;
}) focused)
