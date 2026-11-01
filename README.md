# go-purifier

go-purifier makes Nix the dependency backend of a Go module. It records
the files the go command wants to download in a lockfile, so that Nix can
build the module in its sandbox. You do not commit any generated Nix files
to your repository.

- **A lockfile.** `go-purifier lock` writes `go-purifier.lock.json` next to
  `go.mod`. It lists every module file the build downloads, by URL and
  SHA-256, and the main packages of the module.
- **Sandboxed builds.** `buildGoProject`, which the flake's overlay adds
  to a package set, builds the module from its lockfile with no network
  access. `packages.<name>` is the command of each main package.
- **Interactive go on the locked downloads.** The flake's `go` runs the
  real go command. In a module with a current lockfile, it first fetches
  the locked files through Nix and points `GOPROXY` at them, so `go build`,
  `go test` and `go run` read the same files the Nix build reads, offline.

The purifier idea was inspired by nixcloud's
[cargo (libnix)](https://github.com/nixcloud/cargo).

## How the lock is made

`go-purifier lock` runs the go command in the module with an empty
`GOPATH` and module cache, and with every module coming from one proxy.
It runs `go mod download` and `go list -deps -test ./...`, which together
download what building and testing every package of the module needs.
Each `.mod` and `.zip` file in the module download cache then goes into
the lockfile. The download cache has the layout of a module proxy, so a
file's path in the cache is also its path under the proxy URL.

The proxy is `GO_PURIFIER_PROXY` when it is set, or else the first proxy
URL in `GOPROXY`, or else `https://proxy.golang.org`. The lock run ignores
the user's `go env` file and `GOPRIVATE`, so every module has to be
available from that proxy.

`go-purifier lock` relocks only when `go.mod` or `go.sum` changed since the
lockfile was written. `go-purifier relock` relocks regardless. The lock run
uses `-mod=readonly`, so run `go mod tidy` first if `go.sum` is
incomplete.

## Use

Lock the module, from its directory:

```zsh
nix run github:nix-purifiers/go-purifier -- lock
```

Commit `go-purifier.lock.json`. The flake's `overlays.default` adds
`buildGoProject` to a package set, and a package written for `callPackage`
takes it by name, like any other builder:

```nix
# package.nix
{ buildGoProject }:
(buildGoProject { src = ./.; }).packages.hello
```

```nix
{
  inputs.go-purifier.url = "github:nix-purifiers/go-purifier";
  inputs.nixpkgs.follows = "go-purifier/nixpkgs";

  outputs = { nixpkgs, go-purifier, ... }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        overlays = [
          go-purifier.overlays.default
          (final: prev: { hello = final.callPackage ./package.nix { }; })
        ];
      };
    in
    {
      packages.${system}.hello = pkgs.hello;
    };
}
```

`buildGoProject` returns:

- `packages.<name>`, the command of each main package, named the way
  `go install` names it;
- `all`, one derivation with every command of the module in `bin/`, which
  `packages` link into;
- `proxy`, the module proxy tree of the locked files.

`nix/lib/build-go-project.nix` documents the arguments: `goToolchain`
(the default is the package set's `go`), `fetchurl` (for credentials or
mirrors), `doCheck` (runs `go test ./...`), `tags`, `ldflags` and
`srcFilter`.

## Interactive go

`nix develop github:nix-purifiers/go-purifier` gives a shell with the
flake's `go`, `gofmt` and `go-purifier`. Run go as usual. In a module with
a current lockfile, the first go command fetches the locked files through
Nix, under one GC root in the user cache directory, and links them into a
proxy tree there. Every go command in the module then runs with
`GOPROXY=file://<that tree>`, `GOSUMDB=off` and `GOTOOLCHAIN=local`.
`go.sum` still verifies every module.

A module without a lockfile resolves the usual way. When `go.mod` or
`go.sum` changed since the lockfile was written, go warns and resolves the
usual way until `go-purifier lock` renews the lockfile. `GO_PURIFIER=0`
turns interactive mode off.

## Limits

- The go toolchain comes from nixpkgs, with `GOTOOLCHAIN=local`. A
  `go.mod` whose `go` line is newer than that toolchain needs a newer
  `goToolchain`.
- Private modules that are not on the lock proxy, and `go.work`
  workspaces, are not supported yet.
- A build that downloads outside the go command, such as a `go generate`
  step that fetches files, fails in the sandbox.
