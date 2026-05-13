# nix/checks/go-sec.nix
#
# Security scan via gosec.
#
# Exclusions (each one is justified in-place):
#   G103 — unsafe pointers: required by pkg/io_uring (giouring wraps liburing
#          SQE/CQE structs with unsafe.Pointer).
#   G115 — integer overflow conversions: netlink length fields and io_uring
#          batch indices, all bounds-checked.
#   G204 — subprocess with variable: cmd/ns and cmd/nsTest invoke
#          `ip netns exec ...` by design.
#   G304 — file path from variable: register_schema reads .proto paths from CLI.
#
# NOT excluded — these would block the build if found:
#   G104 (unhandled errors), G112 (HTTP timeouts), G401 (weak crypto), etc.
#
{
  pkgs,
  lib,
  vendoredSource,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-gosec"
  {
    nativeBuildInputs = [
      versions.go
      versions.gosec
    ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d)
    export CGO_ENABLED=0
    export GOFLAGS=-mod=vendor
    gosec -exclude=G103,G115,G204,G304 -fmt=text ./... > $out 2>&1 || {
      rc=$?
      cat $out
      exit "$rc"
    }
  ''
