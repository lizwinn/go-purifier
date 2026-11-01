# SPDX-License-Identifier: MIT
#
# The module proxy of a lockfile: every locked file at its proxy path, as a
# link into the store. The go command reads it as GOPROXY=file://<this>.
# It is a package for callPackage, with the lockfile among its arguments;
# buildGoProject's `proxy` is this.
{
  lib,
  runCommand,
  lockFile,
  # Replaces the built-in fetcher; see fetch.nix.
  fetchurl ? null,
}:
let
  lock = import ./lock.nix { inherit lockFile; };
  fetch = import ./fetch.nix { inherit lock fetchurl; };
in
# The manifest goes to the builder as a file, since a module with many
# dependencies locks thousands of files and the list would not fit in an
# environment variable.
runCommand "go-module-proxy"
  {
    manifest = lib.concatMapStrings (a: "${a.path}\t${fetch a}\n") lock.artifacts;
    passAsFile = [ "manifest" ];
    preferLocalBuild = true;
    allowSubstitutes = false;
    passthru = { inherit lock; };
  }
  ''
    mkdir -p $out
    while IFS=$'\t' read -r rel target; do
      mkdir -p "$out/$(dirname "$rel")"
      ln -s "$target" "$out/$rel"
    done < "$manifestPath"
  ''
