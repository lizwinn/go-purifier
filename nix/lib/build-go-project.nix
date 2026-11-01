# SPDX-License-Identifier: MIT
#
# buildGoProject: the sandboxed build of a Go module, from the lockfile
# `go-purifier lock` writes next to go.mod.
#
# The file is a package for callPackage: the first argument set is what it
# takes from the package set, and the result is the builder. The
# go-purifier overlay (`overlays.default`) adds it to the package set as
# `buildGoProject`, so a package written for callPackage takes it like any
# other dependency:
#
#   { buildGoProject }: (buildGoProject { src = ./.; }).packages.hello
#
# and `buildGoProject.override` replaces what it takes from the set. The
# builder returns
#
#   proxy             the module proxy of every locked file
#   all               one derivation with every main package of the
#                     module in bin/
#   packages.<name>   the command <name> alone, for each main package the
#                     lockfile names
{
  lib,
  stdenv,
  runCommand,
  callPackage,
  go,
}:
{
  # The root of the Go module, where go.mod is.
  src,
  # The lockfile; the default is the one `go-purifier lock` writes.
  lockFile ? src + "/go-purifier.lock.json",
  # The go toolchain. go.mod's `go` line sets the oldest it accepts.
  goToolchain ? go,
  # Replaces the built-in fetcher; see fetch.nix.
  fetchurl ? null,
  # Runs `go test ./...` after the build.
  doCheck ? false,
  # Passed to `go build` and `go test` as -tags and -ldflags.
  tags ? [ ],
  ldflags ? [ ],
  # Filters the module root before it is copied into the store, as the
  # `filter` of builtins.path. The default leaves out VCS and editor state
  # and result links.
  srcFilter ? null,
}:
let
  lock = import ./lock.nix { inherit lockFile; };

  defaultSrcFilter =
    path: type:
    let
      name = baseNameOf path;
    in
    !(name == ".git" || name == ".idea" || name == ".vscode" || lib.hasPrefix "result" name);

  filteredSrc = builtins.path {
    path = src;
    name = "go-source";
    filter = if srcFilter != null then srcFilter else defaultSrcFilter;
  };

  proxy = callPackage ./proxy.nix { inherit lockFile fetchurl; };

  goFlags =
    lib.optionals (tags != [ ]) [ "-tags=${lib.concatStringsSep "," tags}" ]
    ++ lib.optionals (ldflags != [ ]) [ "-ldflags=${lib.concatStringsSep " " ldflags}" ];

  pname = baseNameOf lock.module;

  all = stdenv.mkDerivation {
    inherit pname;
    version = "locked";
    src = filteredSrc;
    nativeBuildInputs = [ goToolchain ];
    dontConfigure = true;
    # The go command's environment; `go-purifier lock` runs go the same
    # way, so the lockfile holds what this build downloads.
    preBuild = ''
      export HOME="$TMPDIR/home"
      export GOCACHE="$TMPDIR/go-cache"
      export GOMODCACHE="$TMPDIR/go-mod"
      export GOPROXY=file://${proxy}
      export GOSUMDB=off
      export GOENV=off
      export GOTOOLCHAIN=local
      export GOWORK=off
      export GOFLAGS="-mod=readonly -trimpath -buildvcs=false"
    '';
    buildPhase = ''
      runHook preBuild
      mkdir -p "$out/bin"
      ${lib.concatMapStrings (b: ''
        echo "building ${b.name} (${b.package})"
        go build ${lib.escapeShellArgs goFlags} -o "$out/bin/${b.name}" ${lib.escapeShellArg b.package}
      '') lock.binaries}
      runHook postBuild
    '';
    inherit doCheck;
    checkPhase = ''
      runHook preCheck
      go test ${lib.escapeShellArgs goFlags} ./...
      runHook postCheck
    '';
    installPhase = ''
      runHook preInstall
      runHook postInstall
    '';
    passthru = { inherit lock proxy; };
    meta.description = "${lock.module}, built by go-purifier";
  };

  # One command of the module, as links into `all`, so that every command
  # of the module comes from one build.
  mkPackage =
    binary:
    runCommand binary.name
      {
        passthru = {
          inherit all lock proxy;
          goPackage = binary.package;
        };
        meta = {
          description = "${binary.package}, built by go-purifier";
          mainProgram = binary.name;
        };
      }
      ''
        mkdir -p "$out/bin"
        ln -s ${all}/bin/${binary.name} "$out/bin/${binary.name}"
      '';

  packages = builtins.listToAttrs (
    map (b: {
      inherit (b) name;
      value = mkPackage b;
    }) lock.binaries
  );
in
{
  inherit
    lock
    proxy
    all
    packages
    ;
}
