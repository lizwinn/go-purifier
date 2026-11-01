# SPDX-License-Identifier: MIT
#
# The `go-purifier` library namespace. It holds nothing: the builder needs
# a package set, so it is the package `buildGoProject` that the default
# package overlay adds.
{ ... }:
{

  overlay = final: prev: {
    go-purifier = prev.go-purifier or { };
  };

}
