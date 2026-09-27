# nix/tests/default.nix
#
# Aggregates behavioral test runners.
#
{
  pkgs,
  lib,
  vendoredSource,
  microvms,
}:

{
  go-unit = import ./go-unit.nix { inherit pkgs vendoredSource; };
  go-bench = import ./go-bench.nix { inherit pkgs vendoredSource; };
  listener-security = import ./go-listener-security.nix { inherit pkgs lib vendoredSource; };
  proto-deserialize-golden = import ./proto-deserialize-golden.nix {
    inherit pkgs vendoredSource;
  };

  # Whole-repo race-detector test (cgo-enabled).
  go-race = import ./go-test-race.nix { inherit pkgs vendoredSource; };

  # Microvm lifecycle, per arch. The microvms input is the result of
  # `import ./nix/microvms { ... }`.
  microvm-lifecycle = microvms.lifecycle;
}
// (import ./go-test-flavors.nix { inherit pkgs lib vendoredSource; })
// (import ./go-test-per-package.nix { inherit pkgs lib vendoredSource; })
