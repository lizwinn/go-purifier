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

    # The scope `go-purifier` is the `default` entry of this flake's package
    # overlay registry (pkg-overlays/): the package set applies it by
    # default, and the flake exports it as `pkgOverlays` and as the plain
    # `overlays.default`. The scope holds the builder, a function, so the
    # flake lists the packages of the scope under `packages` itself.
    nixpkgs = {
      packages.export.enabled = false;
    };
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
      packages = {
        inherit (pkgs.go-purifier) cli toolchain;
        default = pkgs.go-purifier.toolchain;
      };
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
