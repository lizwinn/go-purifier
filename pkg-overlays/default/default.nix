# SPDX-License-Identifier: MIT
#
# The default package overlay. It adds one attribute to the package set,
# the scope `pkgs.go-purifier` that the packages of pkgs/ make
# (`.buildGoProject`, `.cli`, `.toolchain`). A package written for
# callPackage takes the scope by name, as `{ go-purifier }:`, and the
# builder is `go-purifier.buildGoProject`. `go-purifier.overrideScope`
# replaces what the scope shares, such as `go`. A consumer that lists this
# flake in `projects` holds the overlay as `go-purifier/default`, which its
# package sets apply by default.
{ closure-lib, ... }:
{
  overlay = closure-lib.caisson.nixpkgs.mkPackagesOverlay ../../pkgs "go-purifier";
}
