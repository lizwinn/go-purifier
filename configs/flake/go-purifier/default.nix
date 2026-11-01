# SPDX-License-Identifier: MIT
{ ... }:
{
  config,
  inputs,
  lib,
  ...
}:
{

  imports = [ inputs.flake-parts.flakeModules.partitions ];

  debug = false;
  caisson = {
    libOverlays.exported = libOverlays: {
      inherit (libOverlays) default;
    };

    # The packages are the `default` entry of this flake's package overlay
    # registry (pkg-overlays/): the package set applies it by default, and
    # the flake exports it as `pkgOverlays` and as the plain
    # `overlays.default`.
    nixpkgs = {
      packages.export.enabled = true;
    };
  };

  # `nix run github:nix-purifiers/go-purifier -- lock` locks the Go module
  # in the current directory.
  #
  # `nix develop github:nix-purifiers/go-purifier` gives a shell with the
  # toolchain: its `go` runs against the locked downloads, and its gopls
  # runs that `go`.
  perSystem =
    { config, pkgs, ... }:
    {
      packages.default = config.packages.toolchain;
      devShells.default = pkgs.mkShell {
        packages = [ config.packages.toolchain ];
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
              inherit (self.lib.go-purifier) buildGoProject;
              overlay = self.overlays.default;
            };
            treefmt.programs.nixfmt.enable = true;
            treefmt.programs.gofmt.enable = true;
          };
      };
  };

}
