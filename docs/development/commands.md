# Commands

All from the repository root.

```zsh
# Unit tests of the go-purifier command
go test ./...

# Every check in the sandbox: each fixture built from its lockfile and
# run, the release archives, the command with its unit tests, the lock
# version check, and formatting
nix flake check

# Evaluate the checks for aarch64-linux as well
nix flake check --all-systems --no-build

# Interactive mode and the release command, which need Nix and so run
# outside the sandbox
nix build '.#toolchain' --out-link toolchain
tests/interactive.sh toolchain/bin
tests/release.sh toolchain/bin

# Modules from Mercurial, Subversion, Bazaar and Fossil repositories, which
# the script serves on this host. Where the Nix daemon cannot reach this
# host's loopback network, VCS_SYSTEMS_SKIP_NIX=1 runs only the check of
# the module zips against go.sum.
tests/vcs-systems.sh toolchain/bin

# Renew the lockfiles of this repository and the fixtures (needs the
# network; the vcs fixture also reaches github.com with git)
purifier=$PWD/toolchain/bin/go-purifier
for dir in . tests/fixtures/hello tests/fixtures/workspace tests/fixtures/replace/app tests/fixtures/vcs; do
  (cd $dir && $purifier relock)
done

nix fmt
```

After a change to `go.mod` of this repository or of a fixture, relock it,
or the build of `cli` or of the fixture fails for want of a locked file.

## The fixtures

| Fixture | What it covers |
|---|---|
| `hello` | proxy modules, a case-encoded module path, a dependency only tests load |
| `workspace` | a `go.work` workspace of two modules |
| `replace` | a `replace` directive to a directory next to the lock root (`modRoot = "app"`) |
| `vcs` | modules fetched from git through `GONOPROXY`, one in a repository subdirectory |

`tests/vcs-systems.sh` makes its Mercurial, Subversion, Bazaar and Fossil
modules when it runs, since they need servers.

## CI

`.github/workflows/check.yml` runs on every pull request and every push to
main. Its `flake-check` job runs `nix flake check` and evaluates the
checks for every system. Its `outside-the-sandbox` job runs
`tests/interactive.sh` and `tests/release.sh`, then relocks this
repository and every fixture and fails when a lockfile differs from the
committed one, apart from the recorded go version.
