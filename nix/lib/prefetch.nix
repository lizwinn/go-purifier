# SPDX-License-Identifier: MIT
#
# What `go-purifier lock` builds to learn the NAR hash of a Mercurial,
# Subversion, Bazaar or Fossil checkout: the entry, fetched with the
# nixpkgs fetcher a build uses and a placeholder hash. The build fails
# with a hash mismatch that reports the hash of the checkout.
{
  # The nixpkgs the go-purifier flake was built with.
  nixpkgs,
  # The entry, as JSON, without its hash.
  entry,
}:
let
  pkgs = import nixpkgs { };
  fetch = import ./fetch.nix {
    lock.artifactName = _: "go-purifier-prefetch";
    inherit (pkgs)
      fetchhg
      fetchsvn
      fetchbzr
      fetchfossil
      ;
  };
in
fetch (builtins.fromJSON entry // { hash = pkgs.lib.fakeHash; })
