# SPDX-License-Identifier: MIT
#
# The module proxy of a lockfile, for the sandboxed build: every locked
# proxy file at its proxy path, and the files of every module from version
# control, each as a link into the store. The go command reads it as
# GOPROXY=file://<this>. It is a package for callPackage, with the
# lockfile among its arguments; buildGoProject's `proxy` is this.
{
  lib,
  stdenv,
  runCommand,
  lockFile,
  # The go-purifier command, which makes the files of modules from version
  # control. Only a lockfile with such modules needs it.
  cli ? null,
  # Replace the default fetchers; see fetch.nix.
  fetchurl ? null,
  fetchgit ? null,
}:
let
  lock = import ./lock.nix { inherit lockFile; };
  fetch = import ./fetch.nix { inherit lock fetchurl fetchgit; };
  vcsModule = import ./vcs-module.nix {
    inherit lock fetch;
    cli =
      if cli != null then
        lib.getExe cli
      else
        throw "go-purifier: the lockfile has modules from version control, whose files the go-purifier command makes; pass `cli` or apply the go-purifier overlay";
    system = stdenv.buildPlatform.system;
  };
  vcsLines = lib.concatMapStrings (
    m:
    let
      files = vcsModule m;
    in
    lib.concatMapStrings (ext: "${m.proxyDir}/${m.version}${ext}\t${files}/${m.version}${ext}\n") [
      ".mod"
      ".zip"
      ".info"
    ]
  ) lock.vcsModules;
in
# The manifest goes to the builder as a file, since a module with many
# dependencies locks thousands of files and the list would not fit in an
# environment variable.
runCommand "go-module-proxy"
  {
    manifest = lib.concatMapStrings (a: "${a.path}\t${fetch a}\n") lock.files + vcsLines;
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
