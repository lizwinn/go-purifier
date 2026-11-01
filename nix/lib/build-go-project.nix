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
# other builder:
#
#   { buildGoProject }: (buildGoProject { src = ./.; }).packages.hello
#
# and `buildGoProject.override` replaces what it takes from the set. The
# builder returns
#
#   packages.<name>  the command <name>, for each main package of the main
#                    modules the lockfile names
#   tests            a run of `go test` on every package of the main
#                    modules, in the sandbox
#   release          the release archives of the commands for the host
#                    platform, with a SHA256SUMS file: what
#                    `go-purifier publish` uploads
#   fetches          the entries of the shared library that the build
#                    fetches
#   all              one derivation with every command in bin/, which
#                    packages link into
#   proxy            the module proxy tree of the locked downloads
#   lock             the lockfile, read
{
  lib,
  stdenv,
  runCommand,
  # The go toolchain. go.mod's `go` line sets the oldest it accepts.
  go,
  # The fetchers of the entries of the lockfile, which fetchEntry of the
  # shared library calls.
  fetchurl,
  fetchgit,
  fetchhg,
  fetchsvn,
  fetchfossil,
  fetchbzr,
  # The packages fetchEntry builds with after a fetch, such as for
  # unpacking an archive.
  unzip,
  zstd,
  python3,
  writeText,
  # The functions of the shared library of the nix-purifiers, which the
  # go-purifier overlay passes.
  purifier,
}:
let
  buildGoProject =
    {
      # The source tree. It holds the lock root, and every directory a
      # replace directive of the build points at.
      src,
      # The fetchers of the shared library to replace, such as a fetchurl
      # with credentials or a company mirror, passed to its fetchEntry.
      fetchers ? { },
      # The lock root inside src, such as "app", or "" when src is the lock
      # root.
      modRoot ? "",
      # The lockfile. A lockfile outside src lets a packaging flake build a
      # project whose author does not use go-purifier.
      lockFile ? (if modRoot == "" then src else src + "/${modRoot}") + "/go-purifier.lock.json",
      # The go.sum of the lock root. An external build can pass a copy kept
      # outside src, which the build uses in place of the go.sum in src.
      goSum ? (if modRoot == "" then src else src + "/${modRoot}") + "/go.sum",
      # The version in the names of the release archives, or null.
      version ? null,
      # Passed to `go build` and `go test` as -tags and -ldflags.
      tags ? [ ],
      ldflags ? [ ],
      # The libraries and tools that cgo packages of the build need, such
      # as pkg-config and a C library. The lockfile holds Go modules only.
      buildInputs ? [ ],
      nativeBuildInputs ? [ ],
      # Filters a src that is a path before it is copied into the store, as
      # the `filter` of builtins.path. The default leaves out VCS and editor
      # state, result links and the GC roots of go-purifier.
      srcFilter ? null,
    }:
    let
      lock = import ./lock.nix { inherit lockFile; };

      # fetchEntry of the shared library, with the fetchers of the package
      # set, each of which `fetchers` can replace.
      fetch = purifier.fetchEntry (
        {
          inherit
            fetchurl
            fetchgit
            fetchhg
            fetchsvn
            fetchfossil
            fetchbzr
            runCommand
            unzip
            zstd
            python3
            writeText
            ;
        }
        // fetchers
      );

      # Whether goSum is a copy outside src, which replaces the go.sum in
      # src.
      replacesGoSum = goSum != (if modRoot == "" then src else src + "/${modRoot}") + "/go.sum";

      # The lock in memory, which checkLock of the shared library fails
      # with its problems: a lockfile that is missing, damaged, of another
      # format version, or older than go.mod or go.sum.
      checkedLock = purifier.checkLock (
        import ./read-lock.nix {
          inherit
            src
            modRoot
            lockFile
            goSum
            ;
        }
      );
      currentLock = builtins.seq checkedLock lock;

      # The go-purifier command, which makes the files of modules from
      # version control, built from this repository's own lockfile. Only a
      # lockfile with such modules builds it.
      cliSrc = lib.fileset.toSource {
        root = ../..;
        fileset = lib.fileset.unions [
          ../../go.mod
          ../../go.sum
          ../../go-purifier.lock.json
          (lib.fileset.fileFilter (file: file.hasExt "go") ../../cmd)
        ];
      };
      cli =
        (buildGoProject {
          inherit fetchers;
          src = cliSrc;
        }).packages.go-purifier;

      defaultSrcFilter =
        path: type:
        let
          name = baseNameOf path;
        in
        !(
          name == ".git"
          || name == ".go-purifier"
          || name == ".idea"
          || name == ".vscode"
          # The out links of nix build, such as result and result-man, are
          # symlinks. A Go file such as result.go stays.
          || (type == "symlink" && lib.hasPrefix "result" name)
        );

      # A source tree that is the output of a derivation, such as a fetcher,
      # is used as it is: filtering it would build it at evaluation.
      filteredSrc =
        if builtins.isPath src then
          builtins.path {
            path = src;
            name = "go-source";
            filter = if srcFilter != null then srcFilter else defaultSrcFilter;
          }
        else
          src;

      proxy = import ./proxy.nix {
        inherit
          lib
          stdenv
          runCommand
          lockFile
          cli
          fetch
          ;
        entries = purifier.entriesFor stdenv.buildPlatform.system checkedLock;
      };

      goFlags =
        lib.optionals (tags != [ ]) [ "-tags=${lib.concatStringsSep "," tags}" ]
        ++ lib.optionals (ldflags != [ ]) [ "-ldflags=${lib.concatStringsSep " " ldflags}" ];

      pname =
        if currentLock.binaries != [ ] then (builtins.head currentLock.binaries).name else "go-project";

      # The go command's environment; `go-purifier lock` runs go the same
      # way, so the lockfile holds what the build downloads. GONOPROXY
      # matches no module, so modules the lock run fetched from version
      # control come from the proxy tree as well. A go command that fails
      # after reading the proxy tree needed a download the lockfile does not
      # hold, and goWithHint says what to run.
      goEnvironment = ''
        cd ${lib.escapeShellArg (if modRoot == "" then "." else modRoot)}
        ${lib.optionalString replacesGoSum "cp ${goSum} go.sum"}
        export HOME="$TMPDIR/home"
        export GOCACHE="$TMPDIR/go-cache"
        export GOMODCACHE="$TMPDIR/go-mod"
        export GOPROXY=file://${proxy}
        export GONOPROXY=go-purifier.invalid
        export GOSUMDB=off
        export GOENV=off
        export GOTOOLCHAIN=local
        export GOFLAGS="-mod=readonly -trimpath -buildvcs=false"
        goWithHint() {
          if "$@" 2> "$TMPDIR/go-stderr"; then
            cat "$TMPDIR/go-stderr" >&2
            return 0
          fi
          cat "$TMPDIR/go-stderr" >&2
          if grep -q ${lib.escapeShellArg "${proxy}"} "$TMPDIR/go-stderr"; then
            echo "go-purifier: the go command needed a download that the lockfile does not hold. Run \`go-purifier lock\` at the lock root and commit the lockfile." >&2
          fi
          if grep -q 'replacement directory .* does not exist' "$TMPDIR/go-stderr"; then
            echo "go-purifier: a replace directive points at a directory outside src; set src to a directory that contains it, and modRoot to the lock root inside src." >&2
          fi
          return 1
        }
      '';

      goDerivation =
        attrs:
        stdenv.mkDerivation (
          {
            src = filteredSrc;
            nativeBuildInputs = [ go ] ++ nativeBuildInputs;
            inherit buildInputs;
            dontConfigure = true;
            preBuild = goEnvironment;
          }
          // attrs
        );

      all = goDerivation {
        inherit pname;
        version = "locked";
        buildPhase = ''
          runHook preBuild
          mkdir -p "$out/bin"
          ${lib.concatMapStrings (b: ''
            echo "building ${b.name} (${b.package})"
            goWithHint go build ${lib.escapeShellArgs goFlags} -o "$out/bin/${b.name}" ${lib.escapeShellArg b.package}
          '') currentLock.binaries}
          runHook postBuild
        '';
        installPhase = ''
          runHook preInstall
          runHook postInstall
        '';
        passthru = { inherit lock proxy; };
        meta.description = "The commands of a Go build, built by go-purifier";
      };

      tests = goDerivation {
        pname = "${pname}-tests";
        version = "locked";
        buildPhase = ''
          runHook preBuild
          # The packages of the main modules: the module at the lock root,
          # or the modules of the workspace.
          goWithHint go test ${lib.escapeShellArgs goFlags} $(go list -m -f '{{.Path}}/...')
          runHook postBuild
        '';
        installPhase = ''
          runHook preInstall
          touch $out
          runHook postInstall
        '';
      };

      # One command of the build, as links into `all`, so that every
      # command comes from one build.
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
        }) currentLock.binaries
      );

      # A gzipped tarball per command, named <command>_<os>_<arch>, or
      # <command>_<version>_<os>_<arch> with a version, with fixed times and
      # owners, and their SHA-256 sums.
      inherit (stdenv.hostPlatform.go) GOOS GOARCH;
      archive =
        name:
        if version == null then "${name}_${GOOS}_${GOARCH}" else "${name}_${version}_${GOOS}_${GOARCH}";
      release = runCommand "${pname}-release${lib.optionalString (version != null) "-${version}"}" { } ''
        mkdir -p "$out" work
        ${lib.concatMapStrings (b: ''
          mkdir -p work/${b.name}
          cp ${all}/bin/${b.name} work/${b.name}/
          tar --sort=name --mtime=@1 --owner=0 --group=0 --numeric-owner \
            -C work/${b.name} -cf - ${b.name} \
            | gzip -9n > "$out/${archive b.name}.tar.gz"
        '') currentLock.binaries}
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
        packages
        tests
        release
        all
        proxy
        lock
        ;
      fetches = checkedLock.entries;
    };
in
buildGoProject
