# SPDX-License-Identifier: MIT
#
# The go toolchain of the package set, with `go` replaced by go-purifier
# under that name. It runs the real go against a module's locked downloads
# when the module has a current go-purifier.lock.json, and the usual way
# otherwise. `go-purifier` sits beside it, for `go-purifier lock`.
{
  runCommand,
  makeWrapper,
  go,
  cli,
}:
runCommand "go-purifier-go-${go.version}"
  {
    nativeBuildInputs = [ makeWrapper ];
    passthru = { inherit go cli; };
    meta = {
      description = "go with its module downloads from a go-purifier lockfile";
      mainProgram = "go-purifier";
    };
  }
  ''
    mkdir -p "$out/bin"
    for tool in ${go}/bin/*; do
      name=$(basename "$tool")
      if [ "$name" != go ]; then
        ln -s "$tool" "$out/bin/$name"
      fi
    done
    makeWrapper ${cli}/bin/go-purifier "$out/bin/go" \
      --argv0 go \
      --set-default GO_PURIFIER_GO ${go}/bin/go \
      --set GO_PURIFIER_NIX_LIB ${../../nix/lib}
    makeWrapper ${cli}/bin/go-purifier "$out/bin/go-purifier" \
      --set-default GO_PURIFIER_GO ${go}/bin/go \
      --set GO_PURIFIER_NIX_LIB ${../../nix/lib}
  ''
