#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Checks modules from Mercurial, Subversion, Bazaar and Fossil
# repositories, which need servers and so cannot run in the Nix sandbox:
#
#   tests/vcs-systems.sh <bin directory of the toolchain package>
#
# It takes hg, svn, svnadmin, svnserve, bzr, fossil and python3 from PATH,
# and runs itself again in a `nix shell` of them when one is missing.
#
# The script makes a Go module in a repository of each system, serves each
# repository on this host, and serves the go command's lookups of the
# example.invalid module paths with tests/go-get-proxy.py. A consumer
# module requires all four through GOPRIVATE.
#
# The script first checks the module zips go-purifier makes from
# checkouts against go.sum, without Nix. It then locks the consumer,
# builds it with buildGoProject and runs it, and runs it again through the
# go wrapper with no route to the network. The fixed-output builds of the
# Nix daemon fetch from the servers on this host, so the daemon has to
# share this host's loopback network. Where it does not, as in a sandboxed
# agent session, VCS_SYSTEMS_SKIP_NIX=1 stops after the first check.
set -euo pipefail

for tool in hg svn svnadmin svnserve bzr fossil python3; do
  if ! command -v "$tool" > /dev/null; then
    if [ -n "${VCS_SYSTEMS_IN_SHELL:-}" ]; then
      echo "vcs-systems: $tool is not on PATH" >&2
      exit 1
    fi
    VCS_SYSTEMS_IN_SHELL=1 exec nix shell 'nixpkgs#mercurial' 'nixpkgs#subversion' 'nixpkgs#breezy' \
      'nixpkgs#fossil' 'nixpkgs#python3' -c "$0" "$@"
  fi
done

bin=$(cd "${1:?usage: tests/vcs-systems.sh <bin directory of the toolchain package>}" && pwd)
tests=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$tests/.." && pwd)

tmp=$(mktemp -d)
pids=()
cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" 2> /dev/null || true
  done
  chmod -R u+w "$tmp" 2> /dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT

export HOME="$tmp/home" XDG_CACHE_HOME="$tmp/cache" XDG_CONFIG_HOME="$tmp/config"
export XDG_DATA_HOME="$tmp/data" XDG_STATE_HOME="$tmp/state"
export GOPATH="$tmp/gopath" GOCACHE="$tmp/gocache" GOMODCACHE="$tmp/gomod"
export CGO_ENABLED=0 BRZ_EMAIL="test <test@example.invalid>" EMAIL=test@example.invalid
unset GOWORK GOPROXY GONOPROXY GOPRIVATE GOFLAGS
mkdir -p "$HOME"

proxy_port=18300 hg_port=18301 svn_port=18302 bzr_port=18303 fossil_port=18304

# A module that names its version control system.
module() {
  printf 'module example.invalid/%s\n\ngo 1.24\n' "$2" > "$1/go.mod"
  printf 'package %s\n\n// Name names the version control system of this module.\nfunc Name() string { return "%s" }\n' "$2" "$2" > "$1/$2.go"
}

mkdir -p "$tmp/hg"
module "$tmp/hg" hgmod
(cd "$tmp/hg" && hg init && hg add -q && hg commit -q -u test -m init)
hg serve -R "$tmp/hg" -a 127.0.0.1 -p "$hg_port" > "$tmp/hg-serve.log" 2>&1 &
pids+=($!)

svnadmin create "$tmp/svnrepo"
mkdir -p "$tmp/svnsrc"
module "$tmp/svnsrc" svnmod
svn import -q -m init "$tmp/svnsrc" "file://$tmp/svnrepo/svnmod"
svnserve --foreground -d -r "$tmp" --listen-host 127.0.0.1 --listen-port "$svn_port" > "$tmp/svnserve.log" 2>&1 &
pids+=($!)

mkdir -p "$tmp/bzr"
module "$tmp/bzr" bzrmod
(cd "$tmp/bzr" && bzr init -q && bzr add -q && bzr commit -q -m init)
bzr serve --directory="$tmp/bzr" --listen=127.0.0.1 --port="$bzr_port" > "$tmp/bzr-serve.log" 2>&1 &
pids+=($!)

fossil init -A test "$tmp/fossil.repo" > /dev/null
mkdir -p "$tmp/fossilwork"
(cd "$tmp/fossilwork" && fossil open -q "$tmp/fossil.repo" > /dev/null && module . fossilmod && fossil add -q . > /dev/null && fossil commit -q -m init --user test > /dev/null)
fossil server "$tmp/fossil.repo" --port "$fossil_port" --localhost > "$tmp/fossil-server.log" 2>&1 &
pids+=($!)

python3 "$tests/go-get-proxy.py" "$proxy_port" \
  example.invalid/hgmod hg "http://127.0.0.1:$hg_port/" \
  example.invalid/svnmod svn "svn://127.0.0.1:$svn_port/svnrepo/svnmod" \
  example.invalid/bzrmod bzr "bzr://127.0.0.1:$bzr_port/" \
  example.invalid/fossilmod fossil "http://127.0.0.1:$fossil_port/" &
pids+=($!)
sleep 3

consumer="$tmp/consumer"
mkdir -p "$consumer"
printf 'module example.com/vcssystems\n\ngo 1.24\n' > "$consumer/go.mod"
cat > "$consumer/main.go" << 'EOF'
// Command vcssystems prints the names of its four dependencies.
package main

import (
	"fmt"

	"example.invalid/bzrmod"
	"example.invalid/fossilmod"
	"example.invalid/hgmod"
	"example.invalid/svnmod"
)

func main() {
	fmt.Println(hgmod.Name(), svnmod.Name(), bzrmod.Name(), fossilmod.Name())
}
EOF

# The go command reaches example.invalid through the proxy, and the
# servers on this host directly. Fossil reads the lowercase proxy
# variables and ignores no_proxy, so those stay unset.
unset http_proxy https_proxy all_proxy ALL_PROXY
export GOPRIVATE=example.invalid GOINSECURE=example.invalid
export HTTP_PROXY="http://127.0.0.1:$proxy_port" HTTPS_PROXY="http://127.0.0.1:$proxy_port"
export NO_PROXY=127.0.0.1,localhost no_proxy=127.0.0.1,localhost

GO_PURIFIER=0 GOFLAGS=-mod=mod "$bin/go" -C "$consumer" get \
  example.invalid/hgmod@latest example.invalid/svnmod@latest \
  example.invalid/bzrmod@latest example.invalid/fossilmod@latest
GO_PURIFIER=0 "$bin/go" -C "$consumer" mod tidy

# The module files go-purifier makes from checkouts. Each checkout is made
# with the commands of the builder of the nixpkgs fetcher for its system,
# at the revision the go command recorded or encoded in the pseudo-version.
# The go command then downloads every module from those files, and checks
# each against go.sum.
field() {
  python3 -c 'import json, sys; info = json.load(open(sys.argv[1])); print((info.get("Origin") or {}).get(sys.argv[2], "") if sys.argv[2] != "Time" else info["Time"])' "$1" "$2"
}
checkouts="$tmp/checkouts" fileproxy="$tmp/fileproxy"
mkdir -p "$checkouts"
for name in hgmod svnmod bzrmod fossilmod; do
  info=$(ls "$GOMODCACHE/cache/download/example.invalid/$name/@v/"*.info)
  version=$(basename "$info" .info)
  rev=$(field "$info" Hash)
  # The revision number a Subversion or Bazaar pseudo-version encodes.
  number=${version##*-}
  case $name in
  svnmod | bzrmod) number=$((10#$number)) ;;
  esac
  case $name in
  hgmod)
    hg archive -q -y -r "$rev" -R "$tmp/hg" "$checkouts/$name"
    rm -f "$checkouts/$name/.hg_archival.txt"
    ;;
  svnmod)
    svn export -q --non-interactive --ignore-externals --ignore-keywords -r "$number" "file://$tmp/svnrepo/svnmod" "$checkouts/$name"
    ;;
  bzrmod)
    bzr export -q -r "$number" --format=dir "$checkouts/$name" "$tmp/bzr"
    ;;
  fossilmod)
    mkdir -p "$checkouts/$name"
    (cd "$checkouts/$name" && fossil open -q "$tmp/fossil.repo" "$rev" > /dev/null && rm .fslckout)
    ;;
  esac
  mkdir -p "$fileproxy/example.invalid/$name"
  out="$fileproxy/example.invalid/$name/@v" "$bin/go-purifier" module-files \
    "$checkouts/$name" "" "example.invalid/$name" "$version" "$(field "$info" Time)"
done
GO_PURIFIER=0 GOMODCACHE="$tmp/gomod-check" GOPROXY="file://$fileproxy" GONOPROXY=go-purifier.invalid GOSUMDB=off \
  "$bin/go" -C "$consumer" mod download
echo "vcs-systems: go.sum verifies the module zips go-purifier makes from hg, svn, bzr and fossil checkouts"

if [ -n "${VCS_SYSTEMS_SKIP_NIX:-}" ]; then
  echo "vcs-systems: skipped the lock and the Nix build (VCS_SYSTEMS_SKIP_NIX)"
  exit 0
fi

(cd "$consumer" && "$bin/go-purifier" lock)

for kind in hg svn bzr fossil; do
  if ! grep -q "\"$kind\": {" "$consumer/go-purifier.lock.json"; then
    echo "vcs-systems: the lockfile has no $kind entry" >&2
    exit 1
  fi
done

expected="hgmod svnmod bzrmod fossilmod"
built=$(nix build --impure --no-link --print-out-paths --expr "
  let
    flake = builtins.getFlake \"path:$repo\";
    pkgs = flake.legacyPackages.\${builtins.currentSystem};
  in
  (pkgs.buildGoProject { src = /. + \"$consumer\"; }).packages.vcssystems
")
if [ "$("$built/bin/vcssystems")" != "$expected" ]; then
  echo "vcs-systems: the Nix build printed $("$built/bin/vcssystems")" >&2
  exit 1
fi

# Interactive mode: realize the lockfile, then run with no route to the
# network.
case "$("$bin/go" -C "$consumer" env GOPROXY)" in
file://*) ;;
*)
  echo "vcs-systems: the go wrapper does not use the locked downloads" >&2
  exit 1
  ;;
esac
rm -rf "$GOMODCACHE"
interactive=$(HTTP_PROXY=http://127.0.0.1:9 HTTPS_PROXY=http://127.0.0.1:9 "$bin/go" -C "$consumer" run .)
if [ "$interactive" != "$expected" ]; then
  echo "vcs-systems: go run printed $interactive" >&2
  exit 1
fi

echo "vcs-systems: all checks passed"
