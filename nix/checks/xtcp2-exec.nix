# nix/checks/xtcp2-exec.nix
#
# Pure-evaluation check for nix/lib/xtcp2-exec-start.nix: each list element
# must stay one systemd word. The last row pins the old space join, so the
# check shows it would have caught the dropped `-ipmetaBootstrapPath ""` that
# failed self-test check 5g. That systemd then hands xtcp2 the intended argv is
# shown by the interface-naming lifecycle, not here.
{ pkgs, lib }:

let
  inherit (import ../lib/xtcp2-exec-start.nix { inherit pkgs lib; }) mkExecStart;
  pkg = "/p";
  failures = lib.runTests {
    "test positive: plain flags" = {
      expr = mkExecStart pkg [
        "-a"
        "b"
      ];
      expected = ''"/p/bin/xtcp2" "-a" "b"'';
    };
    "test positive: an empty value survives" = {
      expr = mkExecStart pkg [
        "-ipmetaBootstrapPath"
        ""
        "-ipmetaCachePath"
        "/run/x"
      ];
      expected = ''"/p/bin/xtcp2" "-ipmetaBootstrapPath" "" "-ipmetaCachePath" "/run/x"'';
    };
    "test boundary: no args" = {
      expr = mkExecStart pkg [ ];
      expected = ''"/p/bin/xtcp2"'';
    };
    "test corner: spaces stay in one word" = {
      expr = mkExecStart pkg [
        "-tag"
        "a b"
      ];
      expected = ''"/p/bin/xtcp2" "-tag" "a b"'';
    };
    "test corner: percent and dollar are not expanded" = {
      expr = mkExecStart pkg [
        "-u"
        "x%20y"
        "-v"
        "$HOME"
      ];
      expected = ''"/p/bin/xtcp2" "-u" "x%%20y" "-v" "$$HOME"'';
    };
    # systemd splits an unquoted Exec line on runs of whitespace, so the two
    # spaces around the empty element are one separator: four words, not five.
    "test negative: the old join drops the empty word" = {
      expr = lib.filter (w: w != "") (
        lib.splitString " " (
          "/p/bin/xtcp2 "
          + lib.concatStringsSep " " [
            "-ipmetaBootstrapPath"
            ""
            "-ipmetaCachePath"
            "/run/x"
          ]
        )
      );
      expected = [
        "/p/bin/xtcp2"
        "-ipmetaBootstrapPath"
        "-ipmetaCachePath"
        "/run/x"
      ];
    };
  };
in
if failures != [ ] then
  throw "xtcp2-exec: ${builtins.toJSON failures}"
else
  pkgs.runCommand "xtcp2-exec" { } "touch $out"
