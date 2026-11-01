# SPDX-License-Identifier: MIT
#
# The proxy tree the go wrapper builds for interactive use: every locked
# proxy file at its proxy path, and the files of every module from version
# control, as links into the store, in one derivation. `nix-build` of this
# file fetches every entry in parallel, and its out link is the GC root
# that keeps them all alive.
#
# The wrapper evaluates it with nothing but the Nix installation it runs.
# The go-purifier command that runs the wrapper is the builder of the tree
# and of the module files, so no nixpkgs is needed.
{
  # The lockfile, as an absolute path.
  lockFile,
  # The go-purifier command, as an absolute path into the store.
  cli,
  libDir ? ./.,
}:
let
  lock = import (libDir + "/lock.nix") { inherit lockFile; };
  fetch = import (libDir + "/fetch.nix") { inherit lock; };
  # The store object of the command, and the path of the command inside
  # it. storePath adds the store object to the string context of the
  # path, so the builds that run the command may read it.
  cliMatch = builtins.match "(${builtins.storeDir}/[^/]+)(/.*)?" cli;
  cliParts =
    if cliMatch == null then
      throw "go-purifier: the go-purifier command ${cli} is not in the Nix store"
    else
      cliMatch;
  cli' =
    builtins.storePath (builtins.elemAt cliParts 0)
    + (
      let
        rest = builtins.elemAt cliParts 1;
      in
      if rest == null then "" else rest
    );
  system = builtins.currentSystem;
  vcsModule = import (libDir + "/vcs-module.nix") {
    inherit lock fetch system;
    cli = cli';
  };
  fileLines = map (a: "${a.path}\t${fetch a}\n") lock.files;
  vcsLines = map (
    m:
    let
      files = vcsModule m;
    in
    builtins.concatStringsSep "" (
      map (ext: "${m.proxyDir}/${m.version}${ext}\t${files}/${m.version}${ext}\n") [
        ".mod"
        ".zip"
        ".info"
      ]
    )
  ) lock.vcsModules;
in
derivation {
  name = "go-purifier-proxy";
  inherit system;
  builder = cli';
  args = [ "link-tree" ];
  manifest = builtins.concatStringsSep "" (fileLines ++ vcsLines);
  passAsFile = [ "manifest" ];
  preferLocalBuild = true;
  allowSubstitutes = false;
}
