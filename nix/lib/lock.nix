# SPDX-License-Identifier: MIT
#
# Reads a go-purifier lockfile (written by `go-purifier lock`).
#
# Needs no nixpkgs: the go wrapper evaluates this with nothing but the Nix
# installation it runs. `lockFile` is a path, or an absolute path as a
# string.
{ lockFile }:
let
  lock = builtins.addErrorContext "while reading the go-purifier lockfile ${toString lockFile}; when it is damaged, run `go-purifier relock` to write it again" (
    builtins.fromJSON (builtins.readFile lockFile)
  );

  supportedVersion = import ./lock-version.nix;

  # A store path name from a tree path. Store path names allow
  # [A-Za-z0-9+._?=-], must not start with a dot, and are at most 211
  # characters; every other character becomes "_", and a long name keeps
  # its end, where the version and the file type are. The go wrapper and
  # the sandboxed build both name their fetches with this function, so they
  # share store paths.
  storeName =
    path:
    let
      # builtins.split alternates the text between matches with a list per
      # match.
      replaced = builtins.concatStringsSep "" (
        map (part: if builtins.isList part then "_" else part) (builtins.split "[^A-Za-z0-9+._?=-]" path)
      );
      length = builtins.stringLength replaced;
      tail = if length > 200 then builtins.substring (length - 200) 200 replaced else replaced;
      undotted = builtins.head (builtins.match "\\.*(.*)" tail);
    in
    if undotted == "" then "file" else undotted;
in
assert
  lock.version == supportedVersion
  || throw "go-purifier: ${toString lockFile} has lock format version ${toString lock.version}; this library reads version ${toString supportedVersion}. Run `go-purifier relock` with the go-purifier revision of this library.";
lock
// {
  inherit storeName;

  # Whether the files that define the dependencies, under the lock root
  # root (a path), still have the digest the lockfile records. The digest
  # is the one cmd/go-purifier/root.go computes: the SHA-256 of a line per
  # file, in sorted order, of the file's path, a space, and the file's
  # SHA-256 in hex, or "absent".
  isCurrent =
    root:
    let
      line =
        file:
        let
          path = root + "/${file}";
        in
        if builtins.pathExists path then
          "${file} ${builtins.hashFile "sha256" path}\n"
        else
          "${file} absent\n";
      digest = builtins.hashString "sha256" (
        builtins.concatStringsSep "" (map line (builtins.sort builtins.lessThan lock.definitionFiles))
      );
    in
    builtins.convertHash {
      hash = digest;
      hashAlgo = "sha256";
      toHashFormat = "sri";
    } == lock.definitionHash;

  # The fetch name of an entry: its tree path, made a valid store name.
  artifactName = artifact: storeName artifact.path;

  # The file entries, which are proxy files, and every other entry, which
  # is a checkout of the repository of a module from version control.
  files = builtins.filter (a: a ? url) lock.artifacts;
  checkouts = builtins.listToAttrs (
    map (a: {
      name = a.path;
      value = a;
    }) (builtins.filter (a: !(a ? url)) lock.artifacts)
  );
}
