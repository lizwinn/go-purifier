# go-purifier

go-purifier makes Nix the dependency backend of a Go build. It records the
files the go command wants to download in a lockfile, so that Nix can
build the module or workspace in its sandbox. You do not commit any
generated Nix files to your repository.

- **A lockfile.** `go-purifier lock` writes `go-purifier.lock.json` next
  to `go.mod`, or next to `go.work` for a workspace. It lists every
  download of the build: module files from the module proxy, by URL and
  SHA-256, and checkouts of git repositories for modules that `GOPRIVATE`
  or `GONOPROXY` names. It also lists the commands of the build.
- **Sandboxed builds.** `buildGoProject`, which the flake's overlay adds
  to a package set, builds from the lockfile with no network access.
  `packages.<name>` is each command.
- **Interactive go on the locked downloads.** The flake's `go` runs the
  real go command. In a build with a current lockfile, it first fetches
  the locked downloads through Nix and points `GOPROXY` at them, so
  `go build`, `go test` and `go run` read the same files the Nix build
  reads, offline.
- **Releases built by Nix.** `go-purifier release v1.2.3` builds the
  release archives of the commands from the tagged commit through Nix,
  and tags it. With `--push` it also pushes the tag, creates a GitHub
  release with the archives, and asks the module proxy for the new
  version.

go-purifier is one of the [nix-purifiers](https://nix-purifiers.github.io).
Its design follows sbt-nix-backend, the first of them: the tool keeps
planning the build, a lock run with empty caches records what the tool
downloads, Nix builds from the lockfile, and the interactive tool reads
the same locked downloads. The purifier idea was inspired by nixcloud's
[cargo (libnix)](https://github.com/nixcloud/cargo).

## How the lock is made

`go-purifier lock` runs the go command at the lock root with an empty
`GOPATH`, module cache and build cache, and with every module outside
`GONOPROXY` coming from one proxy. It runs `go list -m all`, which
downloads the `go.mod` file of every module in the module graph, and
`go list all`, which downloads every module whose packages the main
modules or their tests import.

The lockfile then gets two kinds of entry:

- **Proxy files.** Each `.mod` and `.zip` file in the module download
  cache. The download cache has the layout of a module proxy, so a file's
  path in the cache is also its path under the proxy URL.
- **Git checkouts.** For each module that `GONOPROXY` (or `GOPRIVATE`)
  matches, the go command fetched it from its repository and recorded the
  repository URL, commit and subdirectory in the cache. The lockfile
  records the checkout with the NAR hash `nix-prefetch-git` reports, and
  the module's version and subdirectory. The Nix build makes the module's
  `.mod` and `.zip` files from the checkout with `go-purifier
  module-files`, following the go command's own rules, and go.sum
  verifies them. An SSH URL is recorded as the HTTPS URL of the same host,
  since the Nix sandbox has no SSH keys.

The lock proxy is `GO_PURIFIER_PROXY` when it is set, or else the first
proxy URL in `GOPROXY`, or else `https://proxy.golang.org`. The lockfile
records the `GONOPROXY` patterns, and a relock without `GONOPROXY` or
`GOPRIVATE` in the environment uses them again.

The lockfile also records the files that define the dependencies:
`go.work` and `go.work.sum` for a workspace, and the `go.mod` and `go.sum`
of every workspace module and of every directory a `replace` directive
points at. `go-purifier lock` relocks only when one of them changed since
the lockfile was written, and `go-purifier relock` relocks regardless. The
lock run uses `-mod=readonly`, so run `go mod tidy` first if `go.sum` is
incomplete.

## Use

Lock the build, from its directory:

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

- `packages.<name>`, the command of each main package of the main
  modules, named the way `go install` names it;
- `all`, one derivation with every command in `bin/`, which `packages`
  link into;
- `release "<version>"`, the release archives of the commands for the host
  platform, with a `SHA256SUMS` file;
- `proxy`, the module proxy tree of the locked downloads.

`nix/lib/build-go-project.nix` documents the arguments. `modRoot` is the
lock root inside `src`, for a build whose `replace` directives point
outside the lock root: `src` then has to contain those directories, and an
evaluation error says so when it does not. `goToolchain` replaces the
package set's `go`. `fetchurl` and `fetchgit` replace the fetchers, for
mirrors or for credentials to private repositories. `doCheck` runs
`go test` on every package of the main modules. `tags`, `ldflags`,
`buildInputs`, `nativeBuildInputs` and `srcFilter` do what their names
say.

## Interactive go

`nix develop github:nix-purifiers/go-purifier` gives a shell with the
flake's `go`, `gofmt` and `go-purifier`. Run go as usual. In a build with
a current lockfile, the first go command builds the proxy tree of the
lockfile through Nix, under one GC root in the user cache directory. Every
go command in the build then runs with `GOPROXY=file://<that tree>`,
`GOSUMDB=off`, `GOTOOLCHAIN=local`, and a `GONOPROXY` that matches no
module, so `GOPRIVATE` does not send the go command to version control.
`go.sum` still verifies every module.

A build without a lockfile resolves the usual way. When a file that
defines the dependencies changed since the lockfile was written, go warns
and resolves the usual way until `go-purifier lock` renews the lockfile.
`GO_PURIFIER=0` turns interactive mode off.

## Releases

A Go module is published by tagging a commit of its repository; the module
proxy serves any tagged version on request. Run `go-purifier release
<version>` in the module's directory. It checks that the version is a
canonical semantic version that fits the module path's major version, and
that the lockfile is current. It then builds the release archives of the
commands through Nix from `HEAD`, writes them to `dist/<version>/`, and
tags `HEAD` with the tag the go command looks up for that version: the
version itself for a module at the repository root, or the module's
directory, a slash and the version for a module in a subdirectory.

`go-purifier release <version> --push` also pushes the tag to `origin`,
creates a GitHub release with the archives when `gh` is on the `PATH`, and
asks the lock proxy for the new version so the proxy caches it.

## Decisions

- **The lockfile holds only `.mod` and `.zip` files from the proxy.** These
  are the files go.sum verifies, and the module cache holds them byte for
  byte as the proxy served them. The go command writes each `.info` file
  in the cache itself, so a cached `.info` file differs from the one at its
  proxy URL. Builds at the versions go.mod names do not read `.info` files.
- **File entries have no mirrors.** Module proxies serve zips with the same
  content, which go.sum verifies, but not necessarily the same bytes, so
  the other proxies in `GOPROXY` are not mirrors of the lock proxy for a
  fixed-output hash. proxy.golang.org keeps every version it has served.
- **The toolchain comes from nixpkgs.** Go's toolchain downloads are
  per-platform modules, which would make a lockfile depend on the system
  that made it. Every go command runs with `GOTOOLCHAIN=local`.
- **Interactive mode needs no nixpkgs.** The go wrapper evaluates
  `nix/lib/realize.nix` with the Nix installation alone, and the
  go-purifier command is the builder of the proxy tree and of the files of
  git modules.

## Limits

- **Modules from Mercurial, Subversion, Bazaar or Fossil repositories**
  that `GONOPROXY` names fail the lock run. The lockfile shares its entry
  kinds with the other nix-purifiers, and those have entries for files
  and git checkouts only. The same modules lock from a module proxy that
  serves them.
- **`.gitattributes` export rules.** The go command makes the zip of a
  module from version control with `git archive`, which leaves out files
  marked `export-ignore` and expands `export-subst`. go-purifier makes the
  zip from a checkout, which keeps them, so for such a repository go.sum
  rejects the zip and the build fails with a checksum mismatch.
- **Downloads outside the go command,** such as a `go generate` step that
  fetches files, fail in the sandbox. C libraries for cgo packages come
  from `buildInputs`, not from the lockfile.
- **`go.mod`'s `go` line** has to be at most the version of the toolchain
  in use; pass a newer `goToolchain` otherwise.
- **`--push`** has no automated check, since it needs a GitHub remote. Its
  steps are `git push`, `gh release create` and `go list -m` of the new
  version. The build, the archives and the tag that come before it are
  checked by `tests/release.sh`.
