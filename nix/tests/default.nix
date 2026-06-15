# nix/tests/default.nix
#
# Aggregates behavioral test runners.
#
{
  pkgs,
  lib,
  src,
  vendoredSource,
  microvms,
}:

{
  go-unit = import ./go-unit.nix { inherit pkgs lib vendoredSource; };
  go-bench = import ./go-bench.nix { inherit pkgs lib vendoredSource; };
  proto-deserialize-golden = import ./proto-deserialize-golden.nix {
    inherit pkgs lib vendoredSource;
  };

  # Whole-repo race-detector test (cgo-enabled).
  go-race = import ./go-test-race.nix { inherit pkgs lib vendoredSource; };

  # Microvm lifecycle, per arch. The microvms input is the result of
  # `import ./nix/microvms { ... }`.
  microvm-lifecycle = microvms.lifecycle;
}
// (import ./go-test-flavors.nix { inherit pkgs lib vendoredSource; })
// (import ./go-test-per-package.nix { inherit pkgs lib vendoredSource; })
