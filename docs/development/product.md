# Product

go-purifier makes Nix the dependency backend of Go modules. A module's
downloads are locked by URL and hash, its commands build in the Nix
sandbox, and interactive go resolves from the same locked downloads
through the store. The README describes what it does for a user.

go-purifier is one of the nix-purifiers. Each purifier does the same for
one build tool, and they share the lockfile's download entries: a file
entry is `{ path, url, hash }`, where `path` is the file's place in the
output tree. For Go that tree is a module proxy, and `path` is the
proxy path.

## What it owns

- The go-purifier command (`cmd/go-purifier`). `lock.go` is the lock run
  and the lockfile format. `wrapper.go` is interactive mode, where the
  same binary runs as `go`.
- The Nix library (`nix/lib`). `lock.nix` reads a lockfile and rejects
  other format versions. `fetch.nix` fetches one entry with Nix's built-in
  fetcher. `proxy.nix` builds the proxy tree for the sandbox, and
  `realize.nix` is what interactive mode builds. `build-go-project.nix` is
  `buildGoProject`.
- The packages (`pkgs`). `cli` is the go-purifier command, built by
  `buildGoProject` from this repository's own lockfile. `toolchain` is the
  nixpkgs go toolchain with `go` replaced by the go-purifier wrapper.

## Decisions

- **The lockfile holds only `.mod` and `.zip` files.** These are the files
  go.sum verifies, and the module cache holds them byte for byte as the
  proxy served them. The go command writes each `.info` file in the cache
  itself, so a cached `.info` file differs from the one at its proxy URL,
  and builds at the versions go.mod names do not read `.info` files.
- **The lock run uses one proxy and no `go env` file.** Every locked file
  then has a URL. A module that only a VCS checkout provides fails the
  lock run rather than producing a lockfile Nix cannot fetch.
- **The toolchain comes from nixpkgs.** Go's toolchain downloads are
  per-platform modules, which would make the lockfile depend on the
  system that locked it.
- **Interactive mode needs no nixpkgs.** It evaluates `realize.nix` with
  `nix-build` and `nix-instantiate` against the Nix installation alone,
  as sbt-nix-backend's interactive mode does.
