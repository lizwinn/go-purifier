# SPDX-License-Identifier: MIT
#
# The default package overlay. It adds the packages of pkgs/ under
# `pkgs.go-purifier` (`.cli`, `.toolchain`). A consumer that lists this
# flake in `projects` holds it as `go-purifier/default`, which its package
# sets apply by default. The builder is in the library, as
# lib.go-purifier.buildGoProject.
{ closure-lib, ... }:
{
  overlay = closure-lib.caisson.nixpkgs.mkPackagesOverlay ../../pkgs "go-purifier";
}
