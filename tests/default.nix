# SPDX-License-Identifier: MIT
#
# The flake's checks: builds of the fixtures from their lockfiles, each
# run, the release archives, and the behaviour of the Nix library on its
# own. tests/interactive.sh covers interactive mode, which runs Nix and so
# cannot run in the sandbox.
{ pkgs, overlay }:
let
  pkgs' = pkgs.extend overlay;
  inherit (pkgs') lib runCommand buildGoProject;

  # A module with proxy dependencies, one of them case-encoded, and a
  # dependency only its tests load.
  hello = buildGoProject {
    src = ./fixtures/hello;
    doCheck = true;
  };
  # A workspace of two modules.
  workspace = buildGoProject {
    src = ./fixtures/workspace;
    doCheck = true;
  };
  # A module with a replace directive to the directory next to it.
  replace = buildGoProject {
    src = ./fixtures/replace;
    modRoot = "app";
  };
  # A module whose dependencies the lock fetched from git: one at the root
  # of its repository, one in a subdirectory of a repository of several
  # modules.
  vcs = buildGoProject { src = ./fixtures/vcs; };

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

  lockWithVersion =
    version:
    builtins.toFile "go-purifier.lock.json" (
      builtins.toJSON {
        inherit version;
        modules = [ "example.com/m" ];
        goVersion = "go1.26.0";
        proxy = "https://proxy.golang.org";
        noProxy = "";
        definitionFiles = [ ];
        definitionHash = "";
        binaries = [ ];
        localModules = [ ];
        vcsModules = [ ];
        artifacts = [ ];
      }
    );
  readsVersion =
    version:
    (builtins.tryEval (import ../nix/lib/lock.nix { lockFile = lockWithVersion version; }).modules)
    .success;
in
{
  fixture-hello-greet =
    prints "fixture-hello-greet"
      "printf 'name = \"Nix\"\\nlanguage = \"de\"\\n' | ${lib.getExe hello.packages.greet}"
      "Hello, Nix (German)";
  fixture-hello-uuidgen = runCommand "fixture-hello-uuidgen" { } ''
    ${lib.getExe hello.packages.uuidgen} | grep -Eqx '[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}'
    touch $out
  '';
  fixture-workspace =
    prints "fixture-workspace" "${lib.getExe workspace.packages.app} hello nix"
      "Hello Nix";
  fixture-replace =
    prints "fixture-replace" (lib.getExe replace.packages.replaceapp)
      "Hello, Nix (request 00000000-0000-0000-0000-000000000000)";
  fixture-vcs =
    prints "fixture-vcs" (lib.getExe vcs.packages.vcs)
      "00000000-0000-0000-0000-000000000000\n4";

  # The proxy tree keeps the case encoding of module paths, and holds the
  # files go-purifier made for modules from version control.
  fixture-proxy-layout = runCommand "fixture-proxy-layout" { } ''
    test -e '${hello.proxy}/github.com/!burnt!sushi/toml/@v/v1.4.0.zip'
    test -e '${hello.proxy}/github.com/!burnt!sushi/toml/@v/v1.4.0.mod'
    test -e '${vcs.proxy}/github.com/charmbracelet/x/ansi/@v/v0.11.9.zip'
    test -e '${vcs.proxy}/github.com/google/uuid/@v/v1.6.0.info'
    touch $out
  '';

  # The release archives of the hello fixture: one per command, each
  # holding the command, with sums that verify.
  fixture-release =
    let
      release = hello.release "v1.0.0";
      inherit (pkgs'.stdenv.hostPlatform.go) GOOS GOARCH;
    in
    runCommand "fixture-release" { } ''
      cd ${release}
      sha256sum -c SHA256SUMS
      for command in greet uuidgen; do
        tar -tzf "''${command}_v1.0.0_${GOOS}_${GOARCH}.tar.gz" | grep -qx "$command"
      done
      touch $out
    '';

  # A replace directive to a directory outside src stops the build at
  # evaluation, with an error that names the directory.
  replace-outside-src =
    assert !(builtins.tryEval (buildGoProject { src = ./fixtures/replace/app; }).all.drvPath).success;
    runCommand "replace-outside-src" { } "touch $out";

  # The go-purifier command, built from this repository's own lockfile,
  # with its unit tests run in the build, and the toolchain around it.
  inherit (pkgs'.go-purifier) cli toolchain;

  # lock.nix reads the version it supports and rejects any other.
  lock-version =
    assert readsVersion (import ../nix/lib/lock-version.nix);
    assert !(readsVersion 0);
    runCommand "lock-version" { } "touch $out";
}
