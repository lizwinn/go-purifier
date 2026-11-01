# SPDX-License-Identifier: MIT
#
# What `go-purifier release` builds: the release archives of a module's
# commands, from a commit of its repository. builtins.fetchGit takes the
# commit's tracked files only, so the archives hold what the Nix build of
# that commit produces.
{
  # The nixpkgs the go-purifier flake was built with.
  nixpkgs,
  # The repository and the commit to build.
  repo,
  rev,
  # The lock root inside the repository.
  modRoot,
  version,
  # The go-purifier command, as an absolute path into the store, for
  # modules from version control.
  cli,
  # The nix/lib directory of the shared library, which the toolchain links
  # into the go-purifier Nix library as `purifier`.
  purifierLib ? ./purifier,
}:
let
  pkgs = import nixpkgs { };
  cliParts = builtins.match "(${builtins.storeDir}/[^/]+)(/.*)?" cli;
  cliPackage = builtins.storePath (builtins.elemAt cliParts 0);
  buildGoProject = pkgs.callPackage ./build-go-project.nix { inherit purifierLib; };
  build = buildGoProject {
    src = builtins.fetchGit {
      url = repo;
      inherit rev;
    };
    inherit modRoot;
    cli = {
      outPath = cliPackage;
      meta.mainProgram = baseNameOf cli;
    };
  };
in
build.release version
