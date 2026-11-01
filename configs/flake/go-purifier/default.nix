# SPDX-License-Identifier: MIT
{ closure-lib, ... }:
{
  inputs,
  lib,
  ...
}:
{

  imports = [
    inputs.flake-parts.flakeModules.partitions
    closure-lib.caisson.modules.flake."purifier/purifier-packages"
  ];

  # The module purifier-packages publishes the derivations of this scope in
  # the package set as the `packages` output.
  purifier.scope = "go-purifier";

  debug = false;
  caisson.libOverlays.exported = libOverlays: {
    inherit (libOverlays) default;
  };

  # `nix run github:nix-purifiers/go-purifier -- lock` locks the Go module
  # in the current directory.
  #
  # `nix develop github:nix-purifiers/go-purifier` gives a shell with the
  # toolchain: its `go` runs against the locked downloads, and its gopls
  # runs that `go`.
  perSystem =
    { pkgs, ... }:
    {
      packages.default = pkgs.go-purifier.toolchain;
      devShells.default = pkgs.mkShell {
        packages = [ pkgs.go-purifier.toolchain ];
      };
    };

  partitionedAttrs.checks = "checks";
  partitionedAttrs.formatter = "formatter";

  partitions.formatter = {
    extraInputs = (lib.caisson.pins.flake-compat ../../../tests/dependencies).sources;
    module =
      { inputs, ... }:
      {
        imports = [ inputs.treefmt-nix.flakeModule ];
        perSystem.treefmt.programs.nixfmt.enable = true;
        perSystem.treefmt.programs.gofmt.enable = true;
      };
  };

  partitions.checks = {
    extraInputs = (lib.caisson.pins.flake-compat ../../../tests/dependencies).sources;
    module =
      { inputs, self, ... }:
      {
        imports = [ inputs.treefmt-nix.flakeModule ];
        perSystem =
          { pkgs, ... }:
          {
            checks = import ../../../tests {
              inherit pkgs;
              inherit (self.lib.go-purifier) readLock;
              overlay = self.overlays.default;
            };
            treefmt.programs.nixfmt.enable = true;
            treefmt.programs.gofmt.enable = true;
          };
      };
  };

}
