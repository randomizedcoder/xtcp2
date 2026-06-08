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

  # Microvm lifecycle, per arch. The microvms input is the result of
  # `import ./nix/microvms { ... }`.
  microvm-lifecycle = microvms.lifecycle;
}
