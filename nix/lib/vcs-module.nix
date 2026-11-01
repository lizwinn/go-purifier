# SPDX-License-Identifier: MIT
#
# The .mod, .zip and .info file of a module version from version control,
# made from a checkout of its repository by `go-purifier module-files`,
# the way the go command makes them. go.sum verifies the .mod and .zip
# file when a build reads them.
#
# The go-purifier command is the builder, so this needs no nixpkgs.
{
  lock,
  # The go-purifier command, as a path into the store.
  cli,
  system,
  # Fetches a git entry; see fetch.nix.
  fetch,
}:
vcsModule:
derivation {
  name = lock.storeName "${vcsModule.module}@${vcsModule.version}";
  inherit system;
  builder = cli;
  args = [
    "module-files"
    (fetch lock.checkouts.${vcsModule.entry})
    vcsModule.subdir
    vcsModule.module
    vcsModule.version
    vcsModule.time
  ];
}
