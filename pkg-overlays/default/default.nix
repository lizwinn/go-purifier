# SPDX-License-Identifier: MIT
#
# The default package overlay. It adds the packages of pkgs/ under
# `pkgs.go-purifier` (`.cli`, `.toolchain`), and the builder
# `pkgs.buildGoProject` (nix/lib/build-go-project.nix) beside them, where
# nixpkgs keeps its own builders, so a package written for callPackage
# takes it by name. The builder fetches with the functions of the shared
# library of the nix-purifiers, `lib.purifier`. A consumer that lists this
# flake in `projects` holds the overlay as `go-purifier/default`, which its
# package sets apply by default.
{ closure-lib, ... }:
let
  packages = closure-lib.caisson.nixpkgs.mkPackagesOverlay ../../pkgs "go-purifier";
in
{
  overlay =
    final: prev:
    packages final prev
    // {
      buildGoProject = final.callPackage ../../nix/lib/build-go-project.nix {
        inherit (closure-lib) purifier;
      };
    };
}
