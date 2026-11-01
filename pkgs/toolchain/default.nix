# SPDX-License-Identifier: MIT
#
# The go toolchain of the package set, with `go` replaced by go-purifier
# under that name. It runs the real go against the locked downloads of a
# build that has a current go-purifier.lock.json, and the usual way
# otherwise. `go-purifier` sits beside it, for `go-purifier lock` and
# `go-purifier release`.
#
# Both take git, for modules from version control, and nix-prefetch-git,
# for locking them, from the end of PATH, so a git on the user's PATH
# comes first. `go-purifier release` builds with the nixpkgs of this
# package set.
{
  lib,
  runCommand,
  makeWrapper,
  go,
  git,
  nix-prefetch-git,
  path,
  cli,
}:
let
  wrapperArgs = lib.escapeShellArgs [
    "--set-default"
    "GO_PURIFIER_GO"
    "${go}/bin/go"
    "--set"
    "GO_PURIFIER_NIX_LIB"
    "${../../nix/lib}"
    "--set-default"
    "GO_PURIFIER_NIXPKGS"
    "${path}"
    "--suffix"
    "PATH"
    ":"
    (lib.makeBinPath [
      git
      nix-prefetch-git
    ])
  ];
in
runCommand "go-purifier-go-${go.version}"
  {
    nativeBuildInputs = [ makeWrapper ];
    passthru = { inherit go cli; };
    meta = {
      description = "go with its module downloads from a go-purifier lockfile";
      mainProgram = "go-purifier";
    };
  }
  ''
    mkdir -p "$out/bin"
    for tool in ${go}/bin/*; do
      name=$(basename "$tool")
      if [ "$name" != go ]; then
        ln -s "$tool" "$out/bin/$name"
      fi
    done
    makeWrapper ${lib.getExe cli} "$out/bin/go" --argv0 go ${wrapperArgs}
    makeWrapper ${lib.getExe cli} "$out/bin/go-purifier" ${wrapperArgs}
  ''
