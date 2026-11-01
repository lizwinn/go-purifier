#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Writes the fixtures of the conformance checks under
# tests/fixtures/conformance from the hello fixture:
#
#   tests/conformance-fixtures.sh
#
#   stale      go.mod changed after the lock
#   tampered   a hash in the lockfile changed, its contentHash not
#   missing    a download removed from the lockfile, with a contentHash
#              that matches, as a lockfile that misses an entry would be
#   malformed  a lockfile that is not JSON
#   external   a source tree with no lockfile, and the lockfile beside it
#   version    a lockfile of another format version
#
# Run it after locking the hello fixture again; CI checks that the committed
# fixtures are what it writes.
set -euo pipefail

tests=$(cd "$(dirname "$0")" && pwd)
hello="$tests/fixtures/hello"
out="$tests/fixtures/conformance"

copy() {
  rm -rf "${out:?}/$1"
  mkdir -p "$out/$1"
  cp -r "$hello/." "$out/$1/${2:-}"
  # Leaves out the GC roots that a prefetch of the hello fixture writes.
  rm -rf "$out/$1/${2:-}/.go-purifier"
}

# Edits a lockfile with a Python statement on `lock`; with "rehash", also
# writes the contentHash go-purifier computes (cmd/go-purifier/contentHash.go).
edit_lock() {
  "${PYTHON:-python3}" - "$1" "$2" "${3:-}" << 'EOF'
import base64, hashlib, json, sys
path, statement, rehash = sys.argv[1], sys.argv[2], sys.argv[3]
lock = json.load(open(path))
exec(statement)
if rehash:
    body = {k: v for k, v in lock.items() if k != "contentHash"}
    canonical = json.dumps(body, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    lock["contentHash"] = "sha256-" + base64.b64encode(hashlib.sha256(canonical.encode()).digest()).decode()
open(path, "w").write(json.dumps(lock, indent=2) + "\n")
EOF
}

copy stale
printf '\n// changed after the lock\n' >> "$out/stale/go.mod"

copy tampered
edit_lock "$out/tampered/go-purifier.lock.json" '
k = next(k for k in sorted(lock["files"]) if k.endswith(".zip"))
lock["files"][k] = "sha256-" + "A" * 43 + "="
'

copy missing
edit_lock "$out/missing/go-purifier.lock.json" '
del lock["files"]["golang.org/x/text/@v/v0.21.0.zip"]
' rehash

copy malformed
printf '{ "version": 1,\n' > "$out/malformed/go-purifier.lock.json"

copy external src
mv "$out/external/src/go-purifier.lock.json" "$out/external/go-purifier.lock.json"

copy version
edit_lock "$out/version/go-purifier.lock.json" 'lock["version"] = 99'
