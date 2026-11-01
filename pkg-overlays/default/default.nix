# SPDX-License-Identifier: MIT
#
# The default package overlay. It adds the packages of pkgs/ under
# `pkgs.go-purifier` (`.cli`, `.toolchain`), and the builder
# `pkgs.buildGoProject` (nix/lib/build-go-project.nix) beside them, where
# nixpkgs keeps its own builders, so a package written for callPackage
# takes it by name. A consumer that lists this flake in `projects` holds
# it as `go-purifier/default`, which its package sets apply by default.
#
# The builder fetches the locked downloads with the shared library of the
# nix-purifiers, which it reads from the `purifier` input.
{ closure-lib, closure-inputs, ... }:
let
  packages = closure-lib.caisson.nixpkgs.mkPackagesOverlay ../../pkgs "go-purifier";
in
{
  overlay =
    final: prev:
    packages final prev
    // {
      buildGoProject = final.callPackage ../../nix/lib/build-go-project.nix {
        purifierLib = "${closure-inputs.purifier}/nix/lib";
      };
    };
}
