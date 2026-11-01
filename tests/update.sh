#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# The conformance-update check, which needs the module proxy and so cannot
# run in the Nix sandbox:
#
#   tests/update.sh <bin directory of the toolchain package>
#
# It copies the hello fixture, which pins github.com/BurntSushi/toml at
# v1.4.0, runs `go-purifier update github.com/BurntSushi/toml`, and checks
# that go.mod moved to a newer version and that readLock finds no problem
# with the lockfile, so that both go.mod and the lockfile pass the stale
# check.
set -euo pipefail

bin=$(cd "${1:?usage: tests/update.sh <bin directory of the toolchain package>}" && pwd)
tests=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$tests/.." && pwd)

tmp=$(mktemp -d)
trap 'chmod -R u+w "$tmp"; rm -rf "$tmp"' EXIT
export XDG_CACHE_HOME="$tmp/cache" GOPATH="$tmp/gopath" GOCACHE="$tmp/gocache" GOMODCACHE="$tmp/gomod"
unset GOPROXY GONOPROXY GOPRIVATE GOWORK

dir="$tmp/hello"
cp -r "$tests/fixtures/hello" "$dir"
chmod -R u+w "$dir"
(cd "$dir" && "$bin/go-purifier" update github.com/BurntSushi/toml)

if grep -q 'github.com/BurntSushi/toml v1.4.0' "$dir/go.mod"; then
  echo "update: go.mod still pins github.com/BurntSushi/toml v1.4.0" >&2
  exit 1
fi

problems=$(nix eval --impure --json --expr "
  let
    flake = builtins.getFlake \"git+file:$repo\";
  in
  (flake.lib.go-purifier.readLock { src = /. + \"$dir\"; }).problems
")
if [ "$problems" != "[]" ]; then
  echo "update: the updated lockfile has problems: $problems" >&2
  exit 1
fi
echo "update: all checks passed"
