# SPDX-License-Identifier: MIT
#
# The flake's checks: the conformance checks of the nix-purifiers that the
# Nix sandbox can run, builds of the fixtures from their lockfiles, each
# run, the release archives, and the go-purifier packages.
#
# conformance-relock, conformance-rebuild and conformance-offline need the
# network or the Nix daemon, so they are CI jobs of those names
# (.github/workflows/check.yml).
{
  pkgs,
  buildGoProject,
  overlay,
}:
let
  inherit (pkgs) lib runCommand;
  packages = (pkgs.extend overlay).go-purifier;

  build = args: buildGoProject ({ inherit pkgs; } // args);

  # A module with proxy dependencies, one of them case-encoded, and a
  # dependency only its tests load.
  hello = build { src = ./fixtures/hello; };
  # A workspace of two modules.
  workspace = build { src = ./fixtures/workspace; };
  # A module with a replace directive to the directory next to it.
  replace = build {
    src = ./fixtures/replace;
    modRoot = "app";
  };
  # A module whose dependencies the lock fetched from git: one at the root
  # of its repository, one in a subdirectory of a repository of several
  # modules.
  vcs = build { src = ./fixtures/vcs; };
  # A source tree with no lockfile, built from a lockfile kept beside it.
  external = build {
    src = ./fixtures/conformance/external/src;
    lockFile = ./fixtures/conformance/external/go-purifier.lock.json;
  };

  # Runs a command and compares what it prints.
  prints =
    name: command: expected:
    runCommand name { inherit expected; } ''
      ${command} > actual
      printf '%s\n' "$expected" > expected-file
      if ! diff -u expected-file actual; then
        exit 1
      fi
      touch $out
    '';

  # Evaluates a lockfile check of nix/lib/lock.nix on a copy of a fixture,
  # and expects an error that contains `text`. lock.nix needs no nixpkgs,
  # so the evaluation runs in the sandbox, on copies outside the store.
  rejects =
    name: fixture: call: text:
    runCommand name
      {
        nativeBuildInputs = [ pkgs.nix ];
        inherit text;
      }
      ''
        export HOME="$TMPDIR/home" NIX_STATE_DIR="$TMPDIR/state" NIX_LOG_DIR="$TMPDIR/log" NIX_CONF_DIR="$TMPDIR/conf"
        mkdir -p "$HOME" "$NIX_CONF_DIR"
        cp -r ${../nix/lib} "$TMPDIR/lib"
        cp -r ${fixture} "$TMPDIR/fixture"
        chmod -R u+w "$TMPDIR/lib" "$TMPDIR/fixture"
        if nix-instantiate --eval --strict --store dummy:// --expr "
          builtins.deepSeq ((import $TMPDIR/lib/lock.nix { lockFile = $TMPDIR/fixture/go-purifier.lock.json; })${call}) true
        " > out 2> err; then
          echo "the check accepted the lockfile; it should have failed with: $text" >&2
          exit 1
        fi
        if ! grep -qF -- "$text" err; then
          echo "the check failed without: $text" >&2
          cat err >&2
          exit 1
        fi
        touch $out
      '';
in
{
  # A lockfile older than go.mod fails at evaluation, naming
  # `go-purifier lock`.
  conformance-stale =
    rejects "conformance-stale" ./fixtures/conformance/stale ".checkCurrent { root = $TMPDIR/fixture; }"
      "run `go-purifier lock`";

  # A lockfile whose download hash was changed fails at evaluation, since
  # its contentHash no longer matches, naming `go-purifier relock`.
  conformance-tampered =
    rejects "conformance-tampered" ./fixtures/conformance/tampered ".entries"
      "contentHash does not match its content; run `go-purifier relock`";

  # A lockfile without a download the build needs: the go command cannot
  # find it in the proxy tree, and the build names `go-purifier lock`.
  conformance-missing =
    runCommand "conformance-missing"
      { failure = pkgs.testers.testBuildFailure (build { src = ./fixtures/conformance/missing; }).all; }
      ''
        grep -qF 'the lockfile does not hold. Run `go-purifier lock`' "$failure/testBuildFailure.log"
        touch $out
      '';

  # A lockfile that is not JSON fails at evaluation, naming
  # `go-purifier relock`.
  conformance-malformed =
    rejects "conformance-malformed" ./fixtures/conformance/malformed ".entries"
      "run `go-purifier relock` to write it again";

  # A source tree that holds no go-purifier lockfile builds from one kept
  # outside it.
  conformance-external =
    prints "conformance-external"
      "printf 'name = \"Nix\"\\nlanguage = \"de\"\\n' | ${lib.getExe external.packages.greet}"
      "Hello, Nix (German)";

  fixture-hello-greet =
    prints "fixture-hello-greet"
      "printf 'name = \"Nix\"\\nlanguage = \"de\"\\n' | ${lib.getExe hello.packages.greet}"
      "Hello, Nix (German)";
  fixture-hello-uuidgen = runCommand "fixture-hello-uuidgen" { } ''
    ${lib.getExe hello.packages.uuidgen} | grep -Eqx '[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}'
    touch $out
  '';
  fixture-hello-tests = hello.tests;
  fixture-workspace =
    prints "fixture-workspace" "${lib.getExe workspace.packages.app} hello nix"
      "Hello Nix";
  fixture-workspace-tests = workspace.tests;
  fixture-replace =
    prints "fixture-replace" (lib.getExe replace.packages.replaceapp)
      "Hello, Nix (request 00000000-0000-0000-0000-000000000000)";
  fixture-vcs =
    prints "fixture-vcs" (lib.getExe vcs.packages.vcs)
      "00000000-0000-0000-0000-000000000000\n4";

  # The output conformance-rebuild builds again with `nix build --rebuild`.
  fixture-hello-build = hello.all;

  # The proxy tree keeps the case encoding of module paths, and holds the
  # files go-purifier made for modules from version control.
  fixture-proxy-layout = runCommand "fixture-proxy-layout" { } ''
    test -e '${hello.proxy}/github.com/!burnt!sushi/toml/@v/v1.4.0.zip'
    test -e '${hello.proxy}/github.com/!burnt!sushi/toml/@v/v1.4.0.mod'
    test -e '${vcs.proxy}/github.com/charmbracelet/x/ansi/@v/v0.11.9.zip'
    test -e '${vcs.proxy}/github.com/google/uuid/@v/v1.6.0.mod'
    touch $out
  '';

  # The release archives of the hello fixture: one per command, each
  # holding the command, with sums that verify. The fetches name every
  # locked download.
  fixture-release =
    let
      inherit (pkgs.stdenv.hostPlatform.go) GOOS GOARCH;
      release =
        (build {
          src = ./fixtures/hello;
          version = "v1.0.0";
        }).release;
    in
    runCommand "fixture-release" { } ''
      cd ${release}
      sha256sum -c SHA256SUMS
      for command in greet uuidgen; do
        tar -tzf "''${command}_v1.0.0_${GOOS}_${GOARCH}.tar.gz" | grep -qx "$command"
      done
      touch $out
    '';
  fixture-fetches =
    assert builtins.length hello.fetches == 8;
    assert builtins.all (e: e ? url && e ? mirrors && e ? hash) hello.fetches;
    runCommand "fixture-fetches" { } "touch $out";

  # A replace directive to a directory outside src fails the build, which
  # says to widen src.
  replace-outside-src =
    runCommand "replace-outside-src"
      { failure = pkgs.testers.testBuildFailure (build { src = ./fixtures/replace/app; }).all; }
      ''
        grep -qF 'a replace directive points at a directory outside src' "$failure/testBuildFailure.log"
        touch $out
      '';

  # The go-purifier command, built from this repository's own lockfile, its
  # unit tests, and the toolchain around it.
  inherit (packages) cli toolchain;
  cli-tests =
    (build {
      src = lib.fileset.toSource {
        root = ../.;
        fileset = lib.fileset.unions [
          ../go.mod
          ../go.sum
          ../go-purifier.lock.json
          (lib.fileset.fileFilter (file: file.hasExt "go") ../cmd)
        ];
      };
    }).tests;

  # lock.nix rejects a lockfile of another format version, naming
  # `go-purifier relock`.
  lock-version =
    rejects "lock-version" ./fixtures/conformance/version ".entries"
      "has lock format version 99";
}
