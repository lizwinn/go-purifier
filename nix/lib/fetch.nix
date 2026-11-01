# SPDX-License-Identifier: MIT
#
# The fetcher for locked entries, in the kinds the nix-purifiers share:
#
#   file    { path, url, mirrors, hash }
#   git     { path, git = { url, rev, submodules }, hash }
#   hg      { path, hg = { url, rev, subrepos }, hash }
#   svn     { path, svn = { url, rev, ignoreExternals, ignoreKeywords }, hash }
#   bzr     { path, bzr = { url, rev }, hash }
#   fossil  { path, fossil = { url, rev }, hash }
#
# A file is a proxy file, and the hash of a file is its SHA-256. Every
# other kind is a checkout of a repository, and its hash is the NAR hash
# of the checkout. The ignoreExternals and ignoreKeywords flags of a
# Subversion entry are those of nixpkgs' fetchsvn.
#
# By default a file is a fixed-output derivation on Nix's built-in fetcher
# (the same derivation <nix/fetchurl.nix> makes), and a git checkout is
# builtins.fetchGit, so neither the go wrapper nor a sandboxed build needs
# nixpkgs for them. A file's store path depends only on its name and hash,
# so the default fetcher and nixpkgs' fetchurl produce the same paths. The
# other kinds need the nixpkgs fetcher of their system.
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
  fetchhg ? null,
  fetchsvn ? null,
  fetchbzr ? null,
  fetchfossil ? null,
}:
artifact:
let
  name = lock.artifactName artifact;
  needs =
    fetcher: kind:
    if fetcher != null then
      fetcher
    else
      throw "go-purifier: ${artifact.path} is a ${kind} checkout, which needs nixpkgs' fetch${kind}";
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
else if artifact ? hg then
  needs fetchhg "hg" {
    inherit name;
    inherit (artifact.hg) url rev;
    inherit (artifact) hash;
    fetchSubrepos = artifact.hg.subrepos;
  }
else if artifact ? svn then
  needs fetchsvn "svn" {
    inherit name;
    inherit (artifact.svn)
      url
      rev
      ignoreExternals
      ignoreKeywords
      ;
    inherit (artifact) hash;
  }
else if artifact ? bzr then
  needs fetchbzr "bzr" {
    inherit (artifact.bzr) url rev;
    inherit (artifact) hash;
  }
else if artifact ? fossil then
  needs fetchfossil "fossil" {
    inherit name;
    inherit (artifact.fossil) url rev;
    inherit (artifact) hash;
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
