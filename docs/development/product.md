# Product

go-purifier makes Nix the dependency backend of Go builds. A build's
downloads are locked by URL or repository and hash, its commands build in
the Nix sandbox, and interactive go resolves from the same locked
downloads through the store. The README describes what it does for a
user, including its decisions and limits.

go-purifier is one of the nix-purifiers. Each purifier does the same for
one build tool, and they share the lockfile's download entries: a file
entry is `{ path, url, mirrors, hash }`, and a git entry is
`{ path, git = { url, rev, submodules }, hash }`. For Go, the path of a
file entry is its module proxy path, and the path of a git entry is a
name under `vcs/` that the Go-specific `vcsModules` list refers to.

## What it owns

- The go-purifier command (`cmd/go-purifier`). `root.go` finds the lock
  root and hashes the files that define the dependencies. `lock.go` is
  the lock run and the lockfile format. `vcs.go` turns the modules the go
  command fetched from git into git entries, and is the `module-files`
  builder that makes their `.mod` and `.zip` files. `wrapper.go` is
  interactive mode, where the same binary runs as `go`, and the
  `link-tree` builder of its proxy tree. `release.go` is
  `go-purifier release`.
- The Nix library (`nix/lib`). `lock.nix` reads a lockfile and rejects
  other format versions. `fetch.nix` fetches one entry. `vcs-module.nix`
  is the derivation of the files of one git module. `proxy.nix` builds the
  proxy tree for the sandbox, and `realize.nix` builds it for interactive
  mode without nixpkgs. `build-go-project.nix` is `buildGoProject`, and
  `release.nix` is what `go-purifier release` builds.
- The packages (`pkgs`). `cli` is the go-purifier command, built by
  `buildGoProject` from this repository's own lockfile. `toolchain` is the
  nixpkgs go toolchain with `go` replaced by the go-purifier wrapper, and
  git and nix-prefetch-git at the end of its `PATH`.

## How the outputs are known

The lock run records the main packages of the main modules in the
lockfile's `binaries`. `buildGoProject` reads them with
`builtins.fromJSON`, so every command is an attribute
(`packages.<name>`) at evaluation time, with no import-from-derivation.
A Go library has no built artifact: it is published as source by tagging
a commit.
