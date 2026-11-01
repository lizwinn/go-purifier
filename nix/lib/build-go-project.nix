# SPDX-License-Identifier: MIT
#
# buildGoProject: the sandboxed build of a Go module or workspace, from the
# lockfile `go-purifier lock` writes at its lock root (next to go.mod, or
# next to go.work for a workspace).
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
#   proxy             the module proxy of every locked download
#   all               one derivation with every command of the main modules
#                     in bin/
#   packages.<name>   the command <name> alone, for each main package the
#                     lockfile names
#   release <version> the release archives of the commands for the host
#                     platform, with a SHA256SUMS file
{
  lib,
  stdenv,
  runCommand,
  callPackage,
  go,
  # The source of the shared library of the nix-purifiers, whose fetchEntry
  # fetches the locked downloads. The go-purifier overlay passes it.
  purifierLib,
  # The go-purifier package scope, whose `cli` makes the files of modules
  # from version control. It is null where the overlay is not applied; a
  # lockfile with such modules then needs `cli`.
  go-purifier ? null,
}:
{
  # The source tree. It holds the lock root, and every directory a replace
  # directive of the build points at.
  src,
  # The lock root inside src, such as "app", or "" when src is the lock
  # root.
  modRoot ? "",
  # The lockfile; the default is the one `go-purifier lock` writes.
  lockFile ? (if modRoot == "" then src else src + "/${modRoot}") + "/go-purifier.lock.json",
  # The go toolchain. go.mod's `go` line sets the oldest it accepts.
  goToolchain ? go,
  # The go-purifier command, for modules from version control.
  cli ? (if go-purifier != null then go-purifier.cli else null),
  # Replace nixpkgs' fetchurl and fetchgit, for credentials or mirrors.
  # Each takes the arguments fetchEntry of the shared library passes to the
  # nixpkgs fetcher of its name.
  fetchurl ? null,
  fetchgit ? null,
  # Runs `go test` on every package of the main modules after the build.
  doCheck ? false,
  # Passed to `go build` and `go test` as -tags and -ldflags.
  tags ? [ ],
  ldflags ? [ ],
  # The libraries and tools that cgo packages of the build need, such as
  # pkg-config and a C library. The lockfile holds Go modules only.
  buildInputs ? [ ],
  nativeBuildInputs ? [ ],
  # Filters src before it is copied into the store, as the `filter` of
  # builtins.path. The default leaves out VCS and editor state and result
  # links.
  srcFilter ? null,
}:
let
  lock = import ./lock.nix { inherit lockFile; };

  # A source tree given as a path is read at evaluation, and a lockfile
  # whose definition files have changed since it was written stops the
  # build there. A source tree that is the output of a derivation, such as
  # a fetcher, is not read at evaluation.
  lockRoot = if modRoot == "" then src else src + "/${modRoot}";
  currentLock =
    if builtins.isPath src && !lock.isCurrent lockRoot then
      throw "go-purifier: go.mod, go.sum or go.work under ${toString lockRoot} changed since its go-purifier.lock.json was written; run `go-purifier lock` there and commit go-purifier.lock.json"
    else
      lock;

  # A relative path with its "." and ".." segments resolved, or null when
  # it leaves the directory it is relative to.
  normalize =
    path:
    let
      step =
        acc: seg:
        if acc == null then
          null
        else if seg == "" || seg == "." then
          acc
        else if seg == ".." then
          (if acc == [ ] then null else lib.init acc)
        else
          acc ++ [ seg ];
      segs = builtins.foldl' step [ ] (lib.splitString "/" path);
    in
    if segs == null then null else lib.concatStringsSep "/" segs;

  # Every directory a replace directive points at is inside src, or the
  # build could not read it.
  checkedLocalModules = map (
    m:
    let
      inSrc = normalize (if modRoot == "" then m.path else "${modRoot}/${m.path}");
    in
    if inSrc == null then
      throw "go-purifier: the lockfile replaces ${m.module} with ${m.path}, which is outside src; set src to a directory that contains both, and modRoot to the lock root inside it"
    else
      inSrc
  ) lock.localModules;

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

  proxy = callPackage ./proxy.nix {
    inherit
      purifierLib
      lockFile
      cli
      fetchurl
      fetchgit
      ;
  };

  goFlags =
    lib.optionals (tags != [ ]) [ "-tags=${lib.concatStringsSep "," tags}" ]
    ++ lib.optionals (ldflags != [ ]) [ "-ldflags=${lib.concatStringsSep " " ldflags}" ];

  pname = baseNameOf (builtins.head currentLock.modules);

  # Runs a go command. When it fails after reading the proxy tree, the go
  # command needed a download the lockfile does not hold, and the hint says
  # how to add it.
  goWithHint = ''
    goWithHint() {
      if "$@" 2> "$TMPDIR/go-stderr"; then
        cat "$TMPDIR/go-stderr" >&2
        return 0
      fi
      cat "$TMPDIR/go-stderr" >&2
      if grep -q ${lib.escapeShellArg "${proxy}"} "$TMPDIR/go-stderr"; then
        echo "go-purifier: the go command needed a download that go-purifier.lock.json does not hold. Run \`go-purifier relock\` at the lock root and commit go-purifier.lock.json." >&2
      fi
      return 1
    }
  '';

  all = stdenv.mkDerivation {
    inherit pname;
    version = "locked";
    src = filteredSrc;
    nativeBuildInputs = [ goToolchain ] ++ nativeBuildInputs;
    inherit buildInputs;
    dontConfigure = true;
    # The go command's environment; `go-purifier lock` runs go the same
    # way, so the lockfile holds what this build downloads. GONOPROXY
    # matches no module, so modules the lock run fetched from version
    # control come from the proxy tree as well.
    preBuild = ''
      ${lib.concatMapStrings (p: "test -d ${lib.escapeShellArg p}\n") checkedLocalModules}
      cd ${lib.escapeShellArg (if modRoot == "" then "." else modRoot)}
      export HOME="$TMPDIR/home"
      export GOCACHE="$TMPDIR/go-cache"
      export GOMODCACHE="$TMPDIR/go-mod"
      export GOPROXY=file://${proxy}
      export GONOPROXY=go-purifier.invalid
      export GOSUMDB=off
      export GOENV=off
      export GOTOOLCHAIN=local
      export GOFLAGS="-mod=readonly -trimpath -buildvcs=false"
      ${goWithHint}
    '';
    buildPhase = ''
      runHook preBuild
      mkdir -p "$out/bin"
      ${lib.concatMapStrings (b: ''
        echo "building ${b.name} (${b.package})"
        goWithHint go build ${lib.escapeShellArgs goFlags} -o "$out/bin/${b.name}" ${lib.escapeShellArg b.package}
      '') lock.binaries}
      runHook postBuild
    '';
    inherit doCheck;
    checkPhase = ''
      runHook preCheck
      goWithHint go test ${lib.escapeShellArgs goFlags} ${
        lib.escapeShellArgs (map (m: "${m}/...") lock.modules)
      }
      runHook postCheck
    '';
    installPhase = ''
      runHook preInstall
      runHook postInstall
    '';
    passthru = { inherit lock proxy purifierLib; };
    meta.description = "${lib.concatStringsSep ", " lock.modules}, built by go-purifier";
  };

  # One command of the build, as links into `all`, so that every command
  # comes from one build.
  mkPackage =
    binary:
    runCommand binary.name
      {
        passthru = {
          inherit
            all
            lock
            proxy
            purifierLib
            ;
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

  # A gzipped tarball per command, named <command>_<version>_<os>_<arch>,
  # with fixed times and owners, and their SHA-256 sums.
  inherit (stdenv.hostPlatform.go) GOOS GOARCH;
  release =
    version:
    runCommand "${pname}-${version}-release" { } ''
      mkdir -p "$out" work
      ${lib.concatMapStrings (b: ''
        mkdir -p work/${b.name}
        cp ${all}/bin/${b.name} work/${b.name}/
        tar --sort=name --mtime=@1 --owner=0 --group=0 --numeric-owner \
          -C work/${b.name} -cf - ${b.name} \
          | gzip -9n > "$out/${b.name}_${version}_${GOOS}_${GOARCH}.tar.gz"
      '') lock.binaries}
      cd "$out"
      if ls *.tar.gz >/dev/null 2>&1; then
        sha256sum *.tar.gz > SHA256SUMS
      else
        touch SHA256SUMS
      fi
    '';
in
{
  inherit
    lock
    proxy
    all
    packages
    release
    ;
}
