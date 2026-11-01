# Commands

All from the repository root.

```zsh
# Unit tests of the go-purifier command
go test ./...

# Every check in the sandbox: the fixture built from its lockfile, the
# command with its unit tests, the lock version check, and formatting
nix flake check

# The go toolchain with the wrapper, and the command on its own
nix build '.#toolchain'
nix build '.#cli'

# Renew the lockfiles of this repository and the fixture (needs the network)
nix run . -- relock
(cd tests/fixtures/hello && nix run ../../.. -- relock)

nix fmt
```

After a change to `go.mod` of this repository or of the fixture, relock
it, or the build of `cli` or of the fixture fails for want of a locked
file.

## Interactive mode by hand

Interactive mode runs Nix, which the sandbox checks cannot do. To try it,
build the toolchain and run its go in the fixture with an empty module
cache and the network off. The build should download every module from
`file://` URLs.

```zsh
nix build '.#toolchain'
GOMODCACHE=$(mktemp -d) ./result/bin/go -C tests/fixtures/hello test ./...
```

## CI

`.github/workflows/check.yml` runs on every pull request and every push to
main. Its `flake-check` job runs `nix flake check`. Its `relock` job
relocks the fixture and this repository from the module proxy and fails
when a lockfile differs from the committed one, apart from the recorded
go version.
