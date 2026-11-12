# nix/checks/no-write-encoders.nix
#
# The daemons, xtcp2 and go-link-monitor, must not be able to build a netlink
# mutation: goip's write verbs and pkg/xtcpnl's write builders compile only
# with the goip_write tag, which cmd/goip and cmd/goip-parity alone carry.
# Three proofs, each with its positive control so the check cannot pass by
# testing nothing:
#
#   files    `go list -deps` of each daemon, with the tags its binary was
#            built with, names no write file of internal/goip, pkg/xtcpnl or
#            pkg/nlparity. The same listing with goip_write must name some.
#   symbols  `go tool nm` on xtcp2-debug (unstripped) and go-link-monitor
#            finds no write builder, no Encode*InfoData and no goip write
#            arm. goip, built with the tag, must have BuildWriteRequest.
#   no_exec  no package in either daemon's deps imports os/exec, bar the
#            third-party ones in execAllowed: the daemons never run a
#            program, and a new importer has to be argued for here.
#
{
  pkgs,
  lib,
  vendoredSource,
  binaries,
  linkmonitor,
}:

let
  versions = import ../versions.nix { inherit pkgs; };

  daemons = [
    {
      name = "xtcp2";
      pkg = "./cmd/xtcp2";
      bin = "${binaries.xtcp2-debug}/bin/xtcp2";
    }
    {
      name = "go-link-monitor";
      pkg = "./cmd/go-link-monitor";
      bin = "${linkmonitor}/bin/go-link-monitor";
    }
  ];

  # The files goip_write gates, in the packages that hold them.
  writePkgs = "^github.com/randomizedcoder/xtcp2/(internal/goip|pkg/xtcpnl|pkg/nlparity)$";
  writeFiles = "(_write[a-z0-9_]*|_flush|_restore|link_property|stats_set)[.]go$";

  # minio-go's process credential provider (the s3parquet destination) runs
  # credential_process from an AWS config; xtcp2 builds static credentials
  # (credentials.NewStaticV4), so that provider is never constructed.
  execAllowed = [ "github.com/minio/minio-go/v7/pkg/credentials" ];

  writeSyms = lib.concatStringsSep "|" [
    "pkg/xtcpnl\\.Build[A-Za-z]*WriteRequest"
    "pkg/xtcpnl\\.BuildSetNsidRequest"
    "pkg/xtcpnl\\.Encode[A-Za-z0-9]*InfoData"
    "internal/goip\\.(linkWrite|routeWrite|addrWrite)$"
  ];
in
pkgs.runCommand "xtcp2-no-write-encoders"
  {
    nativeBuildInputs = [ versions.go ];
    inherit vendoredSource;
  }
  ''
    cp -r $vendoredSource ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    export HOME=$(mktemp -d) GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
    export GOCACHE=$TMPDIR/go-cache
    fail=0

    tags_of() {
      go version -m "$1" | sed -n 's/^[[:space:]]*build[[:space:]]*-tags=//p'
    }
    # write_files PKG TAGS: the write files in PKG's deps under TAGS.
    write_files() {
      go list -deps -tags "$2" -f '{{.ImportPath}} {{join .GoFiles " "}}' "$1" \
        | awk -v p='${writePkgs}' -v f='${writeFiles}' \
            '$1 ~ p { for (i = 2; i <= NF; i++) if ($i ~ f) print $1 "/" $i }'
    }

    ${lib.concatMapStrings (d: ''
      t=$(tags_of ${d.bin})
      w=$(write_files ${d.pkg} "$t")
      if [ -n "$w" ]; then
        echo "FAIL: ${d.name} (tags: $t) compiles write files:"; echo "$w"; fail=1
      fi
      if [ -z "$(write_files ${d.pkg} "''${t:+$t,}goip_write")" ]; then
        echo "FAIL: ${d.name} with goip_write names no write file; the pattern is stale"; fail=1
      fi

      s=$(go tool nm ${d.bin} | grep -E '${writeSyms}' || true)
      if [ -n "$s" ]; then
        echo "FAIL: ${d.name} links write encoders:"; echo "$s"; fail=1
      fi

      e=$(go list -deps -tags "$t" -f '{{.ImportPath}} {{join .Imports " "}}' ${d.pkg} \
        | awk '{ for (i = 2; i <= NF; i++) if ($i == "os/exec") print $1 }' \
        | grep -vxF ${lib.concatMapStringsSep " " (p: "-e ${p}") execAllowed} || true)
      if [ -n "$e" ]; then
        echo "FAIL: ${d.name} (tags: $t) imports os/exec in:"; echo "$e"; fail=1
      fi
    '') daemons}

    if [ -z "$(go tool nm ${binaries.byVariant.debug.goip}/bin/goip | grep -E 'pkg/xtcpnl[.]BuildWriteRequest$' || true)" ]; then
      echo "FAIL: goip lacks xtcpnl.BuildWriteRequest; goip_write is not wired"; fail=1
    fi

    [ "$fail" = 0 ] || exit 1
    echo "ok: ${
      lib.concatMapStringsSep " " (d: d.name) daemons
    } carry no write encoders and no unlisted os/exec; goip has them" > $out
  ''
