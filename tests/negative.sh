#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Checks that each common lockfile mistake fails with an error that says
# what went wrong and what to run, in the Nix build and through the go
# wrapper:
#
#   tests/negative.sh <bin directory of the toolchain package>
#
# Each case copies the hello fixture, breaks it, and expects a failure
# whose output contains a given text: a lockfile that go.mod outgrew, a
# tampered hash, a missing download, a malformed lockfile, and a lockfile
# of another format version.
set -euo pipefail

bin=$(cd "${1:?usage: tests/negative.sh <bin directory of the toolchain package>}" && pwd)
tests=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$tests/.." && pwd)

tmp=$(mktemp -d)
trap 'chmod -R u+w "$tmp"; rm -rf "$tmp"' EXIT
export XDG_CACHE_HOME="$tmp/cache" GOPATH="$tmp/gopath" GOCACHE="$tmp/gocache" GOMODCACHE="$tmp/gomod"
export CGO_ENABLED=0 GOFLAGS=-mod=readonly
unset GOPROXY GONOPROXY GOPRIVATE GOWORK

# A copy of the hello fixture, writable.
fixture() {
  rm -rf "$tmp/$1"
  cp -r "$tests/fixtures/hello" "$tmp/$1"
  chmod -R u+w "$tmp/$1"
  echo "$tmp/$1"
}

# Edits the lockfile of a copy with a Python expression on `lock`.
edit_lock() {
  python3 - "$1/go-purifier.lock.json" "$2" << 'EOF'
import json, sys
path, expression = sys.argv[1], sys.argv[2]
lock = json.load(open(path))
exec(expression)
json.dump(lock, open(path, "w"), indent=2)
EOF
}

# Builds a copy with buildGoProject and expects a failure that mentions the
# given text.
expect_build_failure() {
  local dir=$1 text=$2 log="$tmp/build.log"
  if nix build --impure --no-link --expr "
    let
      flake = builtins.getFlake \"path:$repo\";
      pkgs = import flake.inputs.nixpkgs {
        system = builtins.currentSystem;
        overlays = [ flake.overlays.default ];
      };
    in
    (pkgs.buildGoProject { src = /. + \"$dir\"; }).packages.greet
  " > "$log" 2>&1; then
    echo "negative: the Nix build of $dir succeeded; expected a failure mentioning: $text" >&2
    exit 1
  fi
  if ! grep -qF -- "$text" "$log"; then
    echo "negative: the Nix build of $dir failed without mentioning: $text" >&2
    cat "$log" >&2
    exit 1
  fi
}

# Runs go build through the wrapper, with no route to the network, and
# expects a failure that mentions the given text.
expect_go_failure() {
  local dir=$1 text=$2 log="$tmp/go.log"
  if HTTPS_PROXY=http://127.0.0.1:9 HTTP_PROXY=http://127.0.0.1:9 \
    "$bin/go" -C "$dir" build -o "$tmp/out/" ./... > "$log" 2>&1; then
    echo "negative: go build in $dir succeeded; expected a failure mentioning: $text" >&2
    exit 1
  fi
  if ! grep -qF -- "$text" "$log"; then
    echo "negative: go build in $dir failed without mentioning: $text" >&2
    cat "$log" >&2
    exit 1
  fi
}

# go.mod changed after the lock: the Nix build stops at evaluation, and
# the go wrapper warns that it resolves the usual way.
dir=$(fixture stale)
printf '\n// edited after the lock\n' >> "$dir/go.mod"
expect_build_failure "$dir" 'run `go-purifier lock` there'
warning=$("$bin/go" -C "$dir" env GOPROXY 2>&1 > /dev/null)
case "$warning" in
*'until `go-purifier lock` renews it'*) ;;
*)
  echo "negative: the go wrapper did not warn about the stale lockfile: $warning" >&2
  exit 1
  ;;
esac

# A tampered hash fails the fixed-output fetch.
dir=$(fixture tampered)
edit_lock "$dir" '
a = next(a for a in lock["artifacts"] if a["path"].endswith(".zip"))
a["hash"] = "sha256-" + "A" * 43 + "="
'
expect_build_failure "$dir" "hash mismatch in fixed-output derivation"

# A download missing from the lockfile: the go command cannot find it in
# the proxy tree, and the hint says to relock.
dir=$(fixture missing)
edit_lock "$dir" '
lock["artifacts"] = [a for a in lock["artifacts"] if "golang.org/x/text" not in a["path"] or not a["path"].endswith(".zip")]
'
expect_build_failure "$dir" 'does not hold. Run `go-purifier relock`'
expect_go_failure "$dir" '`go-purifier relock` to rebuild the lockfile'

# A lockfile that is not JSON.
dir=$(fixture malformed)
echo '{ "version": 1, ' > "$dir/go-purifier.lock.json"
expect_go_failure "$dir" 'is not a go-purifier lockfile'
expect_build_failure "$dir" 'go-purifier.lock.json'

# A lockfile of another format version.
dir=$(fixture version)
edit_lock "$dir" 'lock["version"] = 99'
expect_build_failure "$dir" 'has lock format version 99'
expect_go_failure "$dir" 'has lock format version 99'

echo "negative: all checks passed"
