#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Checks interactive mode, which runs Nix and so cannot run in the Nix
# sandbox:
#
#   tests/interactive.sh <bin directory of the toolchain package>
#
# For each fixture, the go wrapper realizes the lockfile through Nix. The
# go commands then run with every HTTP proxy variable pointing at a closed
# port, so any download the go command tried would fail; they read every
# module from the realized proxy tree. gopls, run the same way, loads the
# locked modules. The script also checks that a stale lockfile and
# GO_PURIFIER=0 leave GOPROXY alone, and runs the loop of upgrading a
# dependency and relocking, which reaches the module proxy.
set -euo pipefail

bin=$(cd "${1:?usage: tests/interactive.sh <bin directory of the toolchain package>}" && pwd)
go="$bin/go"
fixtures=$(cd "$(dirname "$0")/fixtures" && pwd)

tmp=$(mktemp -d)
trap 'chmod -R u+w "$tmp"; rm -rf "$tmp"' EXIT
export XDG_CACHE_HOME="$tmp/cache" GOPATH="$tmp/gopath" GOCACHE="$tmp/gocache" GOMODCACHE="$tmp/gomod"
export CGO_ENABLED=0 GOFLAGS=-mod=readonly
unset GOPROXY GONOPROXY GOPRIVATE GOWORK

# Realizes the lockfile of a directory and checks that go reads the proxy
# tree.
realized() {
  local proxy
  proxy=$("$go" -C "$1" env GOPROXY)
  case "$proxy" in
  file://*) ;;
  *)
    echo "interactive: go in $1 uses GOPROXY=$proxy, not the locked downloads" >&2
    exit 1
    ;;
  esac
}

# Runs go with no route to the network.
offline() {
  HTTPS_PROXY=http://127.0.0.1:9 HTTP_PROXY=http://127.0.0.1:9 \
    https_proxy=http://127.0.0.1:9 http_proxy=http://127.0.0.1:9 \
    "$go" "$@"
}

expect() {
  if [ "$1" != "$2" ]; then
    printf 'interactive: got %q, want %q\n' "$1" "$2" >&2
    exit 1
  fi
}

realized "$fixtures/hello"
offline -C "$fixtures/hello" build -o "$tmp/bin/" ./...
offline -C "$fixtures/hello" test ./...
expect "$(printf 'name = "Nix"\nlanguage = "de"\n' | "$tmp/bin/greet")" "Hello, Nix (German)"

realized "$fixtures/workspace"
offline -C "$fixtures/workspace" test ./lib/...
expect "$(offline -C "$fixtures/workspace" run ./app hello nix)" "Hello Nix"

realized "$fixtures/replace/app"
expect "$(offline -C "$fixtures/replace/app" run .)" "Hello, Nix (request 00000000-0000-0000-0000-000000000000)"

realized "$fixtures/vcs"
expect "$(offline -C "$fixtures/vcs" run .)" "$(printf '00000000-0000-0000-0000-000000000000\n4')"

# gopls runs the go wrapper on PATH, so the editor reads the locked
# downloads too. With an empty module cache and no route to the network,
# it type-checks a package that imports locked modules without a
# diagnostic, and reports a type error planted in a copy of it.
gopls=$(nix build --no-link --print-out-paths --inputs-from "$fixtures/../.." 'nixpkgs#gopls')/bin/gopls
chmod -R u+w "$GOMODCACHE" && rm -rf "$GOMODCACHE"
# gopls takes its workspace from the current directory.
check() {
  (cd "$1" && PATH="$bin:$PATH" HTTPS_PROXY=http://127.0.0.1:9 HTTP_PROXY=http://127.0.0.1:9 \
    "$gopls" check internal/greeting/greeting.go 2>&1)
}
expect "$(check "$fixtures/hello")" ""
cp -r "$fixtures/hello" "$tmp/broken"
chmod -R u+w "$tmp/broken"
printf '\nvar _ int = language.English\n' >> "$tmp/broken/internal/greeting/greeting.go"
diagnostics=$(check "$tmp/broken" || true)
case "$diagnostics" in
*"cannot use language.English"*) ;;
*)
  echo "interactive: gopls did not report the planted type error: $diagnostics" >&2
  exit 1
  ;;
esac

# The everyday loop of changing a dependency, which reaches the module
# proxy: go get fetches a version the lockfile does not hold, the next
# build warns that the lockfile is stale, go-purifier lock renews it, and
# the build runs offline again.
cp -r "$fixtures/hello" "$tmp/upgrade"
chmod -R u+w "$tmp/upgrade"
"$go" -C "$tmp/upgrade" get github.com/google/uuid@v1.5.0
warning=$("$go" -C "$tmp/upgrade" env GOPROXY 2>&1 > /dev/null)
case "$warning" in
*'until `go-purifier lock` renews it'*) ;;
*)
  echo "interactive: no stale warning after go get: $warning" >&2
  exit 1
  ;;
esac
(cd "$tmp/upgrade" && "$bin/go-purifier" lock)
grep -q 'github.com/google/uuid/@v/v1.5.0.zip' "$tmp/upgrade/go-purifier.lock.json"
realized "$tmp/upgrade"
offline -C "$tmp/upgrade" build -o "$tmp/upgrade-bin/" ./...

# A stale lockfile, and GO_PURIFIER=0, leave GOPROXY to the user.
cp -r "$fixtures/hello" "$tmp/stale"
chmod -R u+w "$tmp/stale"
echo >> "$tmp/stale/go.sum"
expect "$(GOPROXY=https://proxy.example "$go" -C "$tmp/stale" env GOPROXY 2>/dev/null)" "https://proxy.example"
expect "$(GO_PURIFIER=0 GOPROXY=https://proxy.example "$go" -C "$fixtures/hello" env GOPROXY)" "https://proxy.example"

echo "interactive: all checks passed"
