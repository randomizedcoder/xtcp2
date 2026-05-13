# nix/lib/patchGoMod.nix
#
# Rewrites the local `replace github.com/randomizedcoder/giouring => /home/das/Downloads/giouring`
# in go.mod so it points to a copy of the giouring source vendored at a fixed
# relative path inside the build sandbox.
#
# Why not just rewrite to `${giouring}` (the Nix store path)?
#   buildGoModule's go-modules sub-derivation is fixed-output and cannot
#   reference store paths — Nix rejects it with
#   "fixed-output derivations must not reference store paths".
#   Copying the source into a relative path under the build tree is safe.
#
# Developers iterating against a writable local checkout override with:
#   nix develop --override-input giouring path:/home/das/Downloads/giouring
#
{ giouring }:

''
  if [ -f go.mod ]; then
    mkdir -p ./.nix-vendored-giouring
    cp -r ${giouring}/. ./.nix-vendored-giouring/
    chmod -R u+w ./.nix-vendored-giouring

    sed -i -E \
      's|^(replace github\.com/randomizedcoder/giouring => ).*$|\1./.nix-vendored-giouring|' \
      go.mod

    if ! grep -qF "replace github.com/randomizedcoder/giouring => ./.nix-vendored-giouring" go.mod; then
      echo "patchGoMod: failed to rewrite giouring replace directive" >&2
      cat go.mod >&2
      exit 1
    fi
  fi
''
