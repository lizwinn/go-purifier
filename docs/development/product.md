# Product

go-purifier makes Nix the dependency backend of Go builds. A build's
downloads are locked by URL or repository and hash, its commands build in
the Nix sandbox, and interactive go resolves from the same locked
downloads through the store. The README describes what it does for a
user, including its decisions and limits.

go-purifier is one of the nix-purifiers. Each purifier does the same for
one build tool, and they share the kinds of download entry of the shared
library, `github:nix-purifiers/purifier`, whose `fetchEntry` fetches every
entry go-purifier writes: files, and checkouts of git, Mercurial,
Subversion, Bazaar and Fossil repositories. For Go, the path of a file
entry is its module proxy path, and the path of a checkout entry is a name
under `vcs/` that the Go-specific `vcsModules` list refers to. A
Subversion entry carries fetchsvn's `ignoreExternals` and
`ignoreKeywords`, both true, because the go command exports Subversion
repositories with `--ignore-externals --ignore-keywords`.

## What it owns

- The go-purifier command (`cmd/go-purifier`). `root.go` finds the lock
  root and hashes the files that define the dependencies. `lock.go` is
  the lock run and the lockfile format, and `contentHash.go` is the digest
  of the lockfile's own content. `update.go` is `go-purifier update`.
  `vcs.go` turns the modules the go command fetched from version control
  into checkout entries, and is the `module-files` builder that makes
  their `.mod` and `.zip` files. `wrapper.go` is interactive mode, where
  the same binary runs as `go`, and the `link-tree` builder of its proxy
  tree. `prefetch.go` is `go-purifier prefetch`, and `publish.go` is
  `go-purifier publish`.
- The Nix library (`nix/lib`). `lock.nix` reads a lockfile, makes the file
  entries from its proxy paths, and finds its problems: a missing file,
  another format version, a content that does not match its contentHash,
  and a changed definition digest. `read-lock.nix` is
  `lib.go-purifier.readLock`, the lock in memory of the purifier contract,
  which the builder checks with `checkLock` of the shared library.
  `prefetch.nix`
  is what the lock run builds to learn the hash of a checkout that is not
  a git checkout. The files that `nix-build` evaluates directly
  (`realize.nix`, `prefetch.nix`, `release.nix`) import the shared library
  from `./purifier`, which the toolchain package links into its copy of
  this directory. `vcs-module.nix` is the derivation of the files of one
  module from version control. `proxy.nix` builds the
  proxy tree for the sandbox, and `realize.nix` builds it for interactive
  mode, importing nixpkgs only for checkouts from Mercurial, Subversion,
  Bazaar or Fossil repositories. `build-go-project.nix` is the package
  `buildGoProject`, which the default package overlay
  (`pkg-overlays/default`) adds to the package set with `callPackage`, and
  `release.nix` is what `go-purifier publish` builds.
- The packages (`pkgs`). `cli` is the go-purifier command, built by
  `buildGoProject` from this repository's own lockfile. `toolchain` is the
  nixpkgs go toolchain with `go` replaced by the go-purifier wrapper, with
  gopls, and with git, Mercurial, Subversion, Breezy, Fossil,
  nix-prefetch-git and nix at the end of its `PATH`.

## How the outputs are known

The lock run records the main packages of the main modules in the
lockfile's `binaries`. `readLock` reads them with `builtins.fromJSON`
into its `outputs`, so every command is an attribute (`packages.<name>`)
at evaluation time, with no import-from-derivation.
A Go library has no built artifact: it is published as source by tagging
a commit.
