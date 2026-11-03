#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Checks `go-purifier publish --dry-run`, which builds through Nix
# and so cannot run in the Nix sandbox:
#
#   tests/release.sh <bin directory of the toolchain package> [work directory]
#
# It commits the hello fixture to a new git repository, as a module in a
# subdirectory, publishes v1.0.0 of it up to the tag, and checks the
# archives in dist/ and the tag.
set -euo pipefail

bin=$(cd "${1:?usage: tests/release.sh <bin directory of the toolchain package> [work directory]}" && pwd)
fixtures=$(cd "$(dirname "$0")/fixtures" && pwd)
work=${2:-$(mktemp -d)}
mkdir -p "$work"
work=$(cd "$work" && pwd)

repo="$work/repo"
rm -rf "$repo"
mkdir -p "$repo"
cp -R "$fixtures/hello" "$repo/hello"
chmod -R u+w "$repo"
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid
git -C "$repo" init -q -b main
git -C "$repo" add -A
git -C "$repo" commit -q -m fixture

export XDG_CACHE_HOME="$work/cache" GOPATH="$work/gopath" GOCACHE="$work/gocache" GOMODCACHE="$work/gomod"
unset GOWORK
(cd "$repo/hello" && "$bin/go-purifier" publish v1.0.0 --dry-run)

dist="$repo/hello/dist/v1.0.0"
(cd "$dist" && sha256sum -c SHA256SUMS)
for command in greet uuidgen; do
  ls "$dist/${command}_v1.0.0_"*.tar.gz > /dev/null
done
if [ "$(git -C "$repo" tag --list)" != "hello/v1.0.0" ]; then
  echo "release: the tags are $(git -C "$repo" tag --list), not hello/v1.0.0" >&2
  exit 1
fi
if [ "$(git -C "$repo" rev-parse 'hello/v1.0.0^{commit}')" != "$(git -C "$repo" rev-parse HEAD)" ]; then
  echo "release: hello/v1.0.0 does not tag HEAD" >&2
  exit 1
fi
echo "release: all checks passed"
