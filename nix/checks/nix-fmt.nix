# nix/checks/nix-fmt.nix
#
# `nixfmt --check` over every *.nix in the repo.
#
{
  pkgs,
  lib,
  src,
}:

let
  versions = import ../versions.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-nix-fmt"
  {
    nativeBuildInputs = [
      versions.nixfmt
      pkgs.findutils
    ];
    inherit src;
  }
  ''
    cp -r $src/. ./xtcp2 && chmod -R +w ./xtcp2
    cd ./xtcp2
    bad=0
    while IFS= read -r f; do
      if ! nixfmt --check "$f" 2>/dev/null; then
        echo "nixfmt: not formatted: $f"
        bad=$((bad+1))
      fi
    done < <(find . -type f -name '*.nix' \
      -not -path './vendor/*' -not -path './.git/*' \
      -not -path './build/*')
    if [ "$bad" -gt 0 ]; then
      echo "nixfmt: $bad file(s) need formatting — run 'nixfmt **/*.nix'" >&2
      exit 1
    fi
    echo "nixfmt: all *.nix files formatted" > $out
  ''
