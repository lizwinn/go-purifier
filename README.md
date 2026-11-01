# go-purifier

go-purifier makes Nix the dependency backend of a Go build. It records the
files the go command downloads in a lockfile, so that Nix can build the
module or workspace in its sandbox, and so that the go command and gopls
read the same files offline. Your repository holds the lockfile next to
`go.mod`, and your flake builds from it.

go-purifier is one of the [nix-purifiers](https://nix-purifiers.github.io),
which share one contract: the same builder interface, commands, checks
and README sections for every build tool.

## Quickstart

Lock the module or workspace, from its directory:

```zsh
nix run github:nix-purifiers/go-purifier -- lock
```

This writes `go-purifier.lock.json` next to `go.mod`, or next to `go.work`
for a workspace. Commit it.

The flake's `overlays.default` adds the builder `buildGoProject` to a
package set, where nixpkgs keeps its own builders. A package written for
`callPackage` takes it by name:

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
        overlays = [ go-purifier.overlays.default ];
      };
    in
    {
      packages.${system}.hello = pkgs.callPackage ./package.nix { };
    };
}
```

`buildGoProject` returns these attributes:

- `packages.<name>` is the command of each main package of the main
  modules, named the way `go install` names it.
- `tests` runs `go test` on every package of the main modules in the
  sandbox.
- `release` is the release archives of the commands for the host
  platform, with a `SHA256SUMS` file. `go-purifier publish` uploads it.
- `fetches` is the list of downloads of the build, as entries of the
  shared library of the nix-purifiers, for SBOM, license and vulnerability
  tools.
- `all` is one derivation with every command in `bin/`, which `packages`
  link into, and `proxy` is the module proxy tree of the locked downloads.

`nix/lib/build-go-project.nix` documents the arguments. `modRoot` is the
lock root inside `src`, for a build whose `replace` directives point
outside the lock root; `src` then has to contain those directories.
`lockFile` and `goSum` take a lockfile and a `go.sum` kept outside `src`,
so that a packaging flake can build a project whose author does not use
go-purifier. `fetchers` replaces the fetchers of the shared library, such
as a `fetchurl` with credentials or a company mirror. `version`, `tags`,
`ldflags`, `buildInputs`, `nativeBuildInputs` and `srcFilter` do what
their names say.

`buildGoProject.override` replaces what the builder takes from the
package set. `buildGoProject.override { go = pkgs.go_1_26; }` builds with
another go toolchain.

`lib.go-purifier.readLock { src; }` reads the lockfile into the shape all
nix-purifiers share: the download entries, the commands as outputs, and
the problems that keep the lockfile from being used.

## Development loop

`nix develop github:nix-purifiers/go-purifier` gives a shell with the
flake's `go`, `gofmt`, gopls and `go-purifier`. Run go as usual. In a
build with a current lockfile, the first go command builds the proxy tree
of the lockfile through Nix, under a GC root in `.go-purifier/` at the
lock root. Every go command then runs with `GOPROXY=file://<that tree>`,
`GOSUMDB=off`, `GOTOOLCHAIN=local`, and a `GONOPROXY` that matches no
module, so `GOPRIVATE` does not send the go command to version control.
`go.sum` still verifies every module.

To change a dependency, run `go get`, `go mod tidy` or another command
that changes `go.mod`. These commands read the locked downloads first and
then your own `GOPROXY`, so they can add modules the lockfile does not hold
yet. The next build stops and says to run `go-purifier lock`; after that
the build reads only the locked downloads again.

`go-purifier update` updates every module the build needs to its latest
minor or patch version with `go get -u -t ./...`, then runs `go mod tidy`
and the lock, so that `go.mod`, `go.sum` and the lockfile change in one
run. `go-purifier update github.com/BurntSushi/toml` updates only the
named modules, with `go get <module>@latest`. In a workspace it updates
each module and then runs `go work sync`.

A build without a lockfile resolves the usual way, and `GO_PURIFIER=0`
turns the locked downloads off.

## Working offline

`go-purifier prefetch` realizes in the Nix store everything the lockfile
lists: the proxy files, the checkouts of modules from version control, and
the toolchain of the development shell. It keeps them alive with GC roots
in `.go-purifier/` at the lock root. After a prefetch, `go build`,
`go test`, `go run` and gopls work with the network cut off. Add
`.go-purifier/` to `.gitignore`.

## Editor

The development shell has gopls. gopls runs the `go` on its `PATH`, so an
editor that starts gopls from the shell resolves from the same locked
downloads, offline after a prefetch. An editor that starts gopls itself
needs the shell's `PATH`, such as through direnv with `use flake`.

## Troubleshooting

Every error names the command that fixes it.

| Mistake | What happens |
|---|---|
| `go.mod`, `go.sum` or `go.work` changed after the lock | `buildGoProject` stops at evaluation, and a build through the `go` wrapper stops, both saying to run `go-purifier lock`. |
| A download is missing from the lockfile | The go command fails to read it from the proxy tree, and both the Nix build and the `go` wrapper say to run `go-purifier lock`. |
| The lockfile was edited by hand | Its `contentHash` no longer matches its content, and both say to run `go-purifier lock`. |
| The lockfile is not valid JSON | Both name the file and say to run `go-purifier lock`. |
| The lockfile has another format version | Both name the version and say to run `go-purifier lock`. |
| There is no lockfile | `buildGoProject` names the path where it looked and says to run `go-purifier lock`. |
| A `replace` directive points outside `src` | The Nix build says to widen `src` and to set `modRoot`. |
| `go.mod` lacks a module that the code imports | `go-purifier lock` stops with the go command's error and says to run `go mod tidy`, then `go-purifier lock` again. |

The last row is an error of the lock run. A missing download and a
`replace` directive outside `src` show when the go command runs, so the
build reports them. `readLock` reports each of the other mistakes as an
entry of `problems`. A lockfile that is not JSON fails evaluation with
the file named and the command to run, because Nix cannot catch an error
of `builtins.fromJSON`.

## Supported language repositories

`go-purifier lock` runs the go command at the lock root with an empty
`GOPATH`, module cache and build cache, with `-mod=readonly`, and with
every module outside `GONOPROXY` coming from one proxy. It runs
`go list -m all`, which downloads the `go.mod` file of every module in the
module graph, and `go list all`, which downloads every module whose
packages the main modules or their tests import. The lock run keeps the
resolution of `go.mod` as it is, and a lock of an unchanged project
writes the same lockfile byte for byte. When `go.sum` of a module lacks
entries and `go.mod` needs no change, the lock first runs `go mod tidy`,
which adds them and leaves `go.mod` as it is. In a workspace the lock does
not change `go.sum`, and an incomplete `go.sum` stops it with the go
command's error.

The lockfile holds what Nix needs and `go.mod` and `go.sum` cannot give
it, since `go.sum` records hashes of module contents and not of the files
Nix downloads:

- **Files from a module proxy.** The SHA-256 of each `.mod` and `.zip`
  file, by its path under the proxy. The lock proxy is `GO_PURIFIER_PROXY`
  when it is set, or else the first proxy URL in `GOPROXY`, or else
  `https://proxy.golang.org`. Each file comes from the lock proxy alone,
  because two proxies can serve zips with the same content in different
  bytes.
- **Modules from git, Mercurial, Subversion, Bazaar and Fossil.** The go
  command fetches each module that `GONOPROXY` or `GOPRIVATE` matches from
  its repository. The lockfile records the repository, the revision and
  the NAR hash of a checkout, and the Nix build makes the module's `.mod`
  and `.zip` files from the checkout with `go-purifier module-files`,
  following the go command's own rules, which `go.sum` then verifies. An
  SSH URL of a git or Mercurial repository is recorded as the HTTPS URL of
  the same host, since the Nix sandbox has no SSH keys. The lockfile
  records the `GONOPROXY` patterns, and a later lock without `GONOPROXY` or
  `GOPRIVATE` in the environment uses them again.
- **Local directories** that a `replace` directive points at, which come
  from `src`.

The lockfile also records the commands of the build, and the digest of
the files that define the dependencies (`go.work` and `go.work.sum` for a
workspace, and the `go.mod` and `go.sum` of every main module and of every
directory a `replace` directive points at), which tells a stale lockfile.

These limits affect a build:

- **The go toolchain comes from nixpkgs.** Every go command runs with
  `GOTOOLCHAIN=local`, because Go's toolchain downloads are per-platform
  modules and would tie a lockfile to the system that made it. `go.mod`'s
  `go` line has to be at most the version of the toolchain in use;
  `buildGoProject.override { go = ...; }` selects a newer one.
- **`.gitattributes` export rules break a module from git.** The go
  command makes the zip of such a module with `git archive`, which drops
  files marked `export-ignore` and expands `export-subst`. go-purifier
  makes the zip from a checkout, which keeps them, so `go.sum` rejects the
  zip and the build fails with a checksum mismatch.
- **Only the go command downloads.** A step that fetches files itself,
  such as a `go generate` that downloads, fails in the sandbox. C
  libraries for cgo packages come from `buildInputs`.

## Examples

### fzf

To build [fzf](https://github.com/junegunn/fzf) from a clone of its
repository, lock it in the clone:

```zsh
git clone https://github.com/junegunn/fzf
cd fzf
nix run github:nix-purifiers/go-purifier -- lock
```

Then add a `flake.nix` next to `go.mod`. fzf's Makefile passes the version
to the linker, so the flake does the same with `ldflags`:

```nix
{
  inputs.go-purifier.url = "github:nix-purifiers/go-purifier";
  inputs.nixpkgs.follows = "go-purifier/nixpkgs";

  outputs = { nixpkgs, go-purifier, ... }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        overlays = [ go-purifier.overlays.default ];
      };
      build = pkgs.buildGoProject {
        src = ./.;
        ldflags = [ "-X main.version=0.74.5" ];
      };
    in
    {
      packages.${system}.default = build.packages.fzf;
      checks.${system}.tests = build.tests;
    };
}
```

A flake reads only the files git tracks, so add both files with
`git add go-purifier.lock.json flake.nix`. `nix build` then builds fzf,
and `nix flake check` runs its tests in the sandbox.

To package fzf without changing its repository, keep the lockfile in your
own flake instead. Run the lock in a clone at the tag you package, and
write the lockfile next to your `flake.nix`:

```zsh
git clone --branch v0.74.5 https://github.com/junegunn/fzf
cd fzf
nix run github:nix-purifiers/go-purifier -- lock --lockfile ~/my-flake/fzf.lock.json
```

A package in your flake then fetches the source and passes the lockfile.
Your flake calls it with `callPackage` from a package set with
`go-purifier.overlays.default`, as in the Quickstart:

```nix
# fzf.nix
{ buildGoProject, fetchFromGitHub }:
(buildGoProject {
  src = fetchFromGitHub {
    owner = "junegunn";
    repo = "fzf";
    rev = "v0.74.5";
    hash = "sha256-+v3oSlf3HBpPkNqS3QxRV/B1yHYb07/a70WYuS0aopY=";
  };
  lockFile = ./fzf.lock.json;
  ldflags = [ "-X main.version=0.74.5" ];
}).packages.fzf
```

## Credits

The design follows [sbt-nix-backend](https://github.com/nix-purifiers/sbt-purifier),
the first of the nix-purifiers: the build tool keeps planning the build, a
lock run with empty caches records what the tool downloads, Nix builds
from the lockfile, and the interactive tool reads the same locked
downloads. The purifier idea was inspired by nixcloud's
[cargo (libnix)](https://github.com/nixcloud/cargo). Every download goes
through [purifier](https://github.com/nix-purifiers/purifier), the shared
library of the nix-purifiers, so the sandboxed build and the go wrapper
fetch the same store paths.
