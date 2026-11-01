# go-purifier

go-purifier makes Nix the dependency backend of a Go build. It records the
files the go command wants to download in a lockfile, so that Nix can
build the module or workspace in its sandbox. You do not commit any
generated Nix files to your repository.

- **A lockfile.** `go-purifier lock` writes `go-purifier.lock.json` next
  to `go.mod`, or next to `go.work` for a workspace. It lists every
  download of the build. It lists each module file from the module proxy
  by its URL and SHA-256. For each module that `GOPRIVATE` or `GONOPROXY`
  names, it lists a checkout of the git, Mercurial, Subversion, Bazaar or
  Fossil repository the module comes from. It also lists the commands of
  the build.
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
- **Repository checkouts.** The go command fetches each module that
  `GONOPROXY` (or `GOPRIVATE`) matches from its git, Mercurial,
  Subversion, Bazaar or Fossil repository. It records the repository URL
  and the module's subdirectory in the cache. For git, Mercurial and
  Fossil it also records the revision. For Subversion and Bazaar, the lock
  run resolves the revision with `svn log` or `bzr log`, from the
  reference or the pseudo-version the go command used. The lockfile
  records each checkout with its NAR hash. For git that is the hash
  `nix-prefetch-git` reports; for the other systems it is the hash of the
  checkout the nixpkgs fetcher of that system makes. The Nix build makes
  the `.mod` and `.zip` files of each module from its checkout with
  `go-purifier module-files`, following the go command's own rules, and
  go.sum verifies them. The lockfile records an SSH URL of a git or
  Mercurial repository as the HTTPS URL of the same host, since the Nix
  sandbox has no SSH keys.

The lock proxy is `GO_PURIFIER_PROXY` when it is set, or else the first
proxy URL in `GOPROXY`, or else `https://proxy.golang.org`. The lockfile
records the `GONOPROXY` patterns, and a relock without `GONOPROXY` or
`GOPRIVATE` in the environment uses them again, as `GOPRIVATE` as well, so
`GOVCS` lets the go command use any version control system for them. The
lock run also keeps the user's `GOINSECURE` and `GOVCS`.

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

Commands that change the dependencies, such as `go get`, `go mod tidy`,
`go install pkg@version` and `go list -m -u`, read the locked downloads
first and then your own `GOPROXY`, so they can add modules the lockfile
does not hold yet. The next command then warns that the lockfile is stale;
run `go-purifier lock`, and the build is offline again.

The development shell also has gopls. gopls runs the `go` on its `PATH`,
so an editor that starts it from the shell resolves from the same locked
downloads, offline.

A build without a lockfile resolves the usual way. When a file that
defines the dependencies changed since the lockfile was written, go warns
and resolves the usual way until `go-purifier lock` renews the lockfile.
`GO_PURIFIER=0` turns interactive mode off.

## When something is wrong

| Mistake | What happens |
|---|---|
| `go.mod`, `go.sum` or `go.work` changed after the lock | `buildGoProject` stops at evaluation and says to run `go-purifier lock`. The `go` wrapper warns, and resolves the usual way. |
| A download is missing from the lockfile | The go command fails to read it from the proxy tree, and both the Nix build and the `go` wrapper add a hint to run `go-purifier relock`. |
| A hash in the lockfile was edited | Nix reports a hash mismatch for that download. |
| The lockfile is not valid JSON | The `go` wrapper names the file and says to run `go-purifier relock`; the Nix build names the file in its error. |
| The lockfile has another format version | Both name the version and say to run `go-purifier relock`. |

`tests/negative.sh` checks each of these.

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
- **Downloads go through the shared library.** Every entry is fetched with
  `fetchEntry` of [purifier](https://github.com/nix-purifiers/purifier),
  the library of the nix-purifiers. The sandboxed build and interactive
  mode use the same library and the nixpkgs the go-purifier flake pins, so
  they fetch the same store paths. Interactive mode fetches proxy files
  with Nix's built-in fetcher, and imports the nixpkgs package set only
  for a lockfile with repository checkouts. The go-purifier command builds
  the proxy tree of interactive mode and the files of modules from version
  control.

## Limits

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

## Tests

CI runs every test on x86_64-linux and aarch64-linux, on each pull
request and each push to main.

- `go test ./...` holds the unit tests of the lock run: the lockable
  files, the proxy paths and URLs, the git origins and the entry of each
  version control system, the revision of a pseudo-version, the
  definition digest, the module zips, the release tags, and the `go`
  wrapper.
- `nix flake check` builds each fixture under `tests/fixtures` offline
  from its lockfile and runs it. The fixtures cover proxy modules
  (`hello`), a workspace (`workspace`), a local `replace` (`replace`) and
  modules fetched from git (`vcs`). The checks also cover the release
  archives, the error for a `replace` outside `src`, and the lock format
  version.
- `tests/interactive.sh` runs the `go` wrapper and gopls offline, and the
  loop of upgrading a dependency and relocking.
- `tests/negative.sh` checks the error of each mistake in the table above.
- `tests/release.sh` releases a fixture in a scratch repository.
- `tests/vcs-systems.sh` serves modules from Mercurial, Subversion, Bazaar
  and Fossil repositories on the host, and locks, builds and runs a module
  that requires all four.
- CI relocks this repository and every fixture, and fails unless each
  lockfile comes out byte for byte as committed.
