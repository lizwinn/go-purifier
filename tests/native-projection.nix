# SPDX-License-Identifier: MIT
{
  pkgs,
  project,
  projector,
}:
let
  main = pkgs.writeText "main.go" ''
    package main
    import (
      "fmt"
      "example.com/greeter"
    )
    func main() { fmt.Println(greeter.Greeting("Nix")) }
  '';
in
pkgs.runCommand "go-native-projection-consumer"
  {
    passthru.repository = project.projections.go;
    nativeBuildInputs = [
      pkgs.go
      pkgs.jq
      pkgs.stdenv.cc
    ];
  }
  ''
    export HOME="$TMPDIR/home" GOCACHE="$TMPDIR/cache" GOMODCACHE="$TMPDIR/modules"
    export GOPROXY='file://${project.projections.go},file://${project.proxy}'
    export GOSUMDB=off GOENV=off GOWORK=off GOTOOLCHAIN=local
    mkdir consumer
    cd consumer
    version=$(jq -r '.modules[] | select(.path == "example.com/greeter") | .version' \
      ${project.projections.go.catalog})
    printf 'module example.com/consumer\n\ngo 1.24\nrequire example.com/greeter %s\n' \
      "$version" > go.mod
    cp ${main} main.go
    go run -mod=mod . > actual
    grep -Fx 'Hello, Nix (request 00000000-0000-0000-0000-000000000000)' actual
    # Keep the upstream version: native workspace replacement binds every version.
    printf 'module example.com/consumer\n\ngo 1.24\nrequire example.com/greeter v0.99.0\n' > go.mod
    ${pkgs.lib.getExe projector} bind-module-inputs "$PWD" "$TMPDIR/go.work" \
      ${project.projections.go}
    export GOWORK="$TMPDIR/go.work"
    go run . > actual
    grep -Fx 'Hello, Nix (request 00000000-0000-0000-0000-000000000000)' actual
    grep -F 'v0.99.0' go.mod
    touch "$out"
  ''
