# SPDX-License-Identifier: MIT
#
# The fetcher for locked entries. A file entry
# ({ path, url, mirrors, hash }) is a proxy file; a git entry
# ({ path, git = { url, rev, submodules }, hash }) is a checkout of a
# repository.
#
# By default a file is a fixed-output derivation on Nix's built-in fetcher
# (the same derivation <nix/fetchurl.nix> makes), and a checkout is
# builtins.fetchGit, so neither the go wrapper nor a sandboxed build needs
# nixpkgs to download. A file's store path depends only on its name and
# hash, so the default fetcher and nixpkgs' fetchurl produce the same
# paths.
#
# `fetchurl` and `fetchgit`, when given, replace the defaults. fetchurl is
# called with { name, urls, hash }, and is the place for credentials
# (nixpkgs' fetchurl takes `netrcPhase`) or mirrors. fetchgit is called
# with { name, url, rev, hash, fetchSubmodules }; nixpkgs' fetchgit
# checks the same NAR hash nix-prefetch-git reports.
{
  lock,
  fetchurl ? null,
  fetchgit ? null,
}:
artifact:
let
  name = lock.artifactName artifact;
in
if artifact ? git then
  if fetchgit != null then
    fetchgit {
      inherit name;
      inherit (artifact.git) url rev;
      inherit (artifact) hash;
      fetchSubmodules = artifact.git.submodules;
    }
  else
    builtins.fetchGit {
      inherit (artifact.git) url rev submodules;
      narHash = artifact.hash;
      allRefs = true;
    }
else if fetchurl != null then
  fetchurl {
    inherit name;
    urls = [ artifact.url ] ++ artifact.mirrors or [ ];
    inherit (artifact) hash;
  }
else
  derivation {
    inherit name;
    builder = "builtin:fetchurl";
    system = "builtin";
    inherit (artifact) url;
    urls = [ artifact.url ] ++ artifact.mirrors or [ ];
    outputHash = artifact.hash;
    outputHashAlgo = "sha256";
    outputHashMode = "flat";
    unpack = false;
    executable = false;
    preferLocalBuild = true;
    impureEnvVars = [
      "http_proxy"
      "https_proxy"
      "ftp_proxy"
      "all_proxy"
      "no_proxy"
    ];
  }
