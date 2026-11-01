# SPDX-License-Identifier: MIT
#
# The packages of the `go-purifier` scope. caisson passes the composed
# library as `lib`, whose `go-purifier` namespace has the source of the
# shared library.
{
  callPackage,
  lib,
  ...
}:
let
  cli = callPackage ./cli { };
in
{
  # The go-purifier command, built by buildGoProject from this
  # repository's own lockfile.
  inherit cli;
  # The go toolchain with `go` running through go-purifier, gopls, and the
  # go-purifier command beside them.
  toolchain = callPackage ./toolchain {
    inherit cli;
    purifierSrc = lib.go-purifier.sharedLibrarySource;
  };
}
