# SPDX-License-Identifier: MIT
{ pkgs, buildGoProject }:
let
  inherit (pkgs) lib;
  root = ../..;
in
(buildGoProject {
  inherit pkgs;
  src = lib.fileset.toSource {
    inherit root;
    fileset = lib.fileset.unions [
      (root + "/go.mod")
      (root + "/go.sum")
      (root + "/go-purifier.lock.json")
      (lib.fileset.fileFilter (file: file.hasExt "go") (root + "/cmd"))
    ];
  };
}).packages.go-purifier
