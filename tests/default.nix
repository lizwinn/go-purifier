# SPDX-License-Identifier: MIT
#
# The flake's checks: builds of the fixture module from its lockfile, and
# the behaviour of the Nix library on its own.
{ pkgs, overlay }:
let
  pkgs' = pkgs.extend overlay;
  inherit (pkgs') lib runCommand;

  hello = pkgs'.buildGoProject {
    src = ./fixtures/hello;
    doCheck = true;
  };

  lockWithVersion =
    version:
    builtins.toFile "go-purifier.lock.json" (
      builtins.toJSON {
        inherit version;
        module = "example.com/m";
        goVersion = "go1.26.0";
        proxy = "https://proxy.golang.org";
        definitionHash = "";
        binaries = [ ];
        artifacts = [ ];
      }
    );
  readsVersion =
    version:
    (builtins.tryEval (import ../nix/lib/lock.nix { lockFile = lockWithVersion version; }).module)
    .success;
in
{
  # The commands of the fixture, built offline from the fixture's
  # lockfile, with the fixture's tests (whose go-cmp dependency only the
  # tests load) run in the build.
  fixture-greet = runCommand "fixture-greet" { } ''
    printf 'name = "Nix"\nlanguage = "de"\n' | ${lib.getExe hello.packages.greet} > greeting
    if [ "$(cat greeting)" != "Hello, Nix (German)" ]; then
      echo "greet printed: $(cat greeting)" >&2
      exit 1
    fi
    touch $out
  '';
  fixture-uuidgen = runCommand "fixture-uuidgen" { } ''
    ${lib.getExe hello.packages.uuidgen} | grep -Eqx '[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}'
    touch $out
  '';

  # The proxy tree keeps the proxy's case encoding of module paths.
  fixture-proxy-layout = runCommand "fixture-proxy-layout" { } ''
    test -e '${hello.proxy}/github.com/!burnt!sushi/toml/@v/v1.4.0.zip'
    test -e '${hello.proxy}/github.com/!burnt!sushi/toml/@v/v1.4.0.mod'
    touch $out
  '';

  # The go-purifier command, built from this repository's own lockfile,
  # with its unit tests run in the build.
  inherit (pkgs'.go-purifier) cli toolchain;

  # lock.nix reads the version it supports and rejects any other.
  lock-version =
    assert readsVersion (import ../nix/lib/lock-version.nix);
    assert !(readsVersion 0);
    runCommand "lock-version" { } "touch $out";
}
