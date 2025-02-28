#
#
#
# https://nixos.wiki/wiki/Python

# type nix-shell

let
  pkgs = import <nixpkgs> {};
in pkgs.mkShell {
  packages = [
    (pkgs.python3.withPackages (python-pkgs: [
      #python-pkgs.subprocess
      python-pkgs.argparse
      python-pkgs.tempfile
    ]))
  ];
}