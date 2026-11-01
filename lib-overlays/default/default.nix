# SPDX-License-Identifier: MIT
#
# The `go-purifier` library namespace: readLock (nix/lib/read-lock.nix),
# which reads the lockfiles into the lock in memory of the purifier
# contract, the builder lib.go-purifier.buildGoProject
# (nix/lib/build-go-project.nix), which
# fetches with the functions of the shared library of the nix-purifiers,
# lib.purifier, and the source of that library, sharedLibrarySource, which
# the toolchain links into the go-purifier Nix library for the files the
# go wrapper evaluates with nix-build.
{ closure-inputs, ... }:
{

  overlay = final: prev: {
    go-purifier = (prev.go-purifier or { }) // {
      buildGoProject = import ../../nix/lib/build-go-project.nix { inherit (final) purifier; };
      readLock = import ../../nix/lib/read-lock.nix;
      sharedLibrarySource = "${closure-inputs.purifier}";
    };
  };

}
