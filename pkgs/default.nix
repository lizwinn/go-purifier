# SPDX-License-Identifier: MIT
{ callPackage, ... }:
let
  cli = callPackage ./cli { };
in
{
  # The go-purifier command, built by buildGoProject from this
  # repository's own lockfile.
  inherit cli;
  # The go toolchain with `go` running through go-purifier, and the
  # go-purifier command beside it.
  toolchain = callPackage ./toolchain { inherit cli; };
}
