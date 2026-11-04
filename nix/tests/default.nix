# nix/tests/default.nix
#
# Aggregates behavioral test runners.
#
{
  pkgs,
  lib,
  src,
  vendoredSource,
  # nix/lib/goSource.nix's tree and its vendored copy, for the artifacts the
  # vm-* guests boot.
  goSource,
  binaries,
  microvms,
  linkmonitorPackages,
  containers,
  ipmetaBootstrapArtifact,
  # The image the Runpod fleet runs, checked to carry the real snapshot.
  ipmetaSnapshotImage,
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
      ipmetaBootstrapArtifact
      ;
    snapshotImage = ipmetaSnapshotImage;
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
  oci-link-monitor-env = import ./oci-link-monitor-env.nix { inherit pkgs lib containers; };
  oci-ipmeta-bootstrap-contents = ipmetaBootstrap.oci-contents;
  ipmeta-bootstrap-snapshot = ipmetaBootstrap.snapshot;
  oci-ipmeta-bootstrap-snapshot-contents = ipmetaBootstrap.oci-snapshot-contents;

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
    goSource
    linkmonitorPackages
    ;
})
// (lib.mapAttrs' (name: value: {
  name = "test-${name}";
  inherit value;
}) focused)
