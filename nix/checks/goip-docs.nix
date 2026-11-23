# nix/checks/goip-docs.nix
#
# Documentation gates over docs/netlink: every markdown link resolves
# (relative path and anchor), no trailing whitespace, and the iproute2
# findings record under docs/netlink/upstream is complete — the index lists
# every artifact and every note is linked from a reader. The rules are
# docs/netlink/goip-parity-events.md Tier UB0-T02; the negative cases live
# in nix/tests/test_goip_docs.py.
{
  pkgs,
  src,
}:

pkgs.runCommand "xtcp2-test-goip-docs"
  {
    nativeBuildInputs = [ pkgs.python3 ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    python3 -m unittest discover -s nix/tests -p test_goip_docs.py -v
    python3 nix/tests/goip_docs.py docs . > $out
  ''
