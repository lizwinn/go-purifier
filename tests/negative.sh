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
# hash changed by hand, a missing download, a malformed lockfile, and a lockfile
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

# Edits the lockfile of a copy with a Python statement on `lock`; with
# "rehash", also writes the contentHash go-purifier computes
# (cmd/go-purifier/contentHash.go), as a lockfile from another go-purifier
# would have.
edit_lock() {
  python3 - "$1/go-purifier.lock.json" "$2" "${3:-}" << 'EOF'
import base64, hashlib, json, sys
path, statement, rehash = sys.argv[1], sys.argv[2], sys.argv[3]
lock = json.load(open(path))
exec(statement)
if rehash:
    body = {k: v for k, v in lock.items() if k != "contentHash"}
    canonical = json.dumps(body, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    lock["contentHash"] = "sha256-" + base64.b64encode(hashlib.sha256(canonical.encode()).digest()).decode()
json.dump(lock, open(path, "w"), indent=2)
EOF
}

# Builds a copy with buildGoProject and expects a failure that mentions the
# given text.
expect_build_failure() {
  local dir=$1 text=$2 log="$tmp/build.log"
  if nix build --impure --no-link --expr "
    let
      flake = builtins.getFlake \"git+file:$repo\";
      pkgs = import flake.inputs.nixpkgs { system = builtins.currentSystem; overlays = [ flake.overlays.default ]; };
    in
    (pkgs.go-purifier.buildGoProject { src = /. + \"$dir\"; }).packages.greet
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
# so does a build through the go wrapper.
dir=$(fixture stale)
printf '\n// edited after the lock\n' >> "$dir/go.mod"
expect_build_failure "$dir" 'run `go-purifier lock` there'
expect_go_failure "$dir" 'run `go-purifier lock`'

# A hash changed by hand no longer matches the contentHash.
dir=$(fixture tampered)
edit_lock "$dir" '
k = next(k for k in sorted(lock["files"]) if k.endswith(".zip"))
lock["files"][k] = "sha256-" + "A" * 43 + "="
'
expect_build_failure "$dir" "contentHash does not match its content"
expect_go_failure "$dir" "contentHash does not match its content"

# A download missing from the lockfile: the go command cannot find it in
# the proxy tree, and the hint says to lock.
dir=$(fixture missing)
edit_lock "$dir" '
lock["files"] = {k: v for k, v in lock["files"].items() if "golang.org/x/text" not in k or not k.endswith(".zip")}
' rehash
expect_build_failure "$dir" 'does not hold. Run `go-purifier lock`'
expect_go_failure "$dir" 'Run `go-purifier lock` and commit the lockfile'

# A lockfile that is not JSON.
dir=$(fixture malformed)
echo '{ "version": 1, ' > "$dir/go-purifier.lock.json"
expect_go_failure "$dir" 'is not a go-purifier lockfile'
expect_build_failure "$dir" 'run `go-purifier lock` to write it again'

# A lockfile of another format version.
dir=$(fixture version)
edit_lock "$dir" 'lock["version"] = 99' rehash
expect_build_failure "$dir" 'has lock format version 99'
expect_go_failure "$dir" 'has lock format version 99'

echo "negative: all checks passed"
