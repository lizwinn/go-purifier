# SPDX-License-Identifier: MIT
#
# Reads a go-purifier lockfile (written by `go-purifier lock`), and checks
# that it is of the format version this library reads and that its
# content matches its contentHash.
#
# Needs no nixpkgs: the go wrapper evaluates this with nothing but the Nix
# installation it runs. `lockFile` is a path, or an absolute path as a
# string.
{ lockFile }:
let
  read = builtins.addErrorContext "while reading the go-purifier lockfile ${toString lockFile}; when it is damaged, run `go-purifier relock` to write it again" (
    builtins.fromJSON (builtins.readFile lockFile)
  );

  supportedVersion = import ./lock-version.nix;

  # The digest cmd/go-purifier/contentHash.go computes: the SHA-256 of the
  # lockfile without its contentHash field, as builtins.toJSON writes it.
  contentHash = builtins.convertHash {
    hash = builtins.hashString "sha256" (builtins.toJSON (builtins.removeAttrs read [ "contentHash" ]));
    hashAlgo = "sha256";
    toHashFormat = "sri";
  };

  lock =
    if !(builtins.isAttrs read && read ? version) then
      throw "go-purifier: ${toString lockFile} is not a go-purifier lockfile; run `go-purifier relock` to write it again"
    else if read.version != supportedVersion then
      throw "go-purifier: ${toString lockFile} has lock format version ${toString read.version}; this library reads version ${toString supportedVersion}. Run `go-purifier relock` with the go-purifier revision of this library."
    else if (read.contentHash or null) != contentHash then
      throw "go-purifier: ${toString lockFile} was changed after go-purifier wrote it, since its contentHash does not match its content; run `go-purifier relock` to write it again"
    else
      read;

  # A store path name from a module path and version. Store path names
  # allow [A-Za-z0-9+._?=-], must not start with a dot, and are at most 211
  # characters; every other character becomes "_", and a long name keeps
  # its end, where the version is. The go wrapper and the sandboxed build
  # both name the files of modules from version control with this
  # function, so they share store paths.
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

  # Whether the files that define the dependencies, under the lock root
  # `root` (a path), still have the digest the lockfile records.
  # `overrides` gives a file in place of the one at a definition path, such
  # as a go.sum kept outside the source tree. The digest is the one
  # cmd/go-purifier/root.go computes: the SHA-256 of a line per file, in
  # sorted order, of the file's path, a space, and the file's SHA-256 in
  # hex, or "absent".
  isCurrent =
    {
      root,
      overrides ? { },
    }:
    let
      line =
        file:
        let
          path = overrides.${file} or (root + "/${file}");
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

  withHelpers = lock // {
    inherit storeName isCurrent;

    # The lockfile, when it is current for the lock root, as isCurrent
    # decides; otherwise an error that names the command to run.
    checkCurrent =
      args:
      if isCurrent args then
        withHelpers
      else
        throw "go-purifier: go.mod, go.sum or go.work under ${toString args.root} changed since ${toString lockFile} was written; run `go-purifier lock` there and commit the lockfile";

    # The file entries of the shared library for the proxy files: the
    # lockfile records each file's proxy path and SHA-256, and the URL is
    # the path under the proxy.
    files = map (path: {
      inherit path;
      url = "${lock.proxy}/${path}";
      mirrors = [ ];
      hash = lock.files.${path};
    }) (builtins.attrNames lock.files);

    # The checkout entries, by path, as the vcsModules name them.
    checkouts = builtins.listToAttrs (
      map (a: {
        name = a.path;
        value = a;
      }) lock.checkouts
    );

    # Every entry the build fetches.
    entries = withHelpers.files ++ lock.checkouts;

    # The proxy directory of a module version: the go command's case
    # encoding of the module path, where each capital letter becomes "!"
    # and the small letter, then /@v.
    proxyDir = module: escapeModulePath module + "/@v";
  };

  capitals = builtins.genList (i: builtins.substring i 1 "ABCDEFGHIJKLMNOPQRSTUVWXYZ") 26;
  smalls = builtins.genList (i: builtins.substring i 1 "abcdefghijklmnopqrstuvwxyz") 26;
  escapeModulePath = builtins.replaceStrings capitals (map (c: "!" + c) smalls);
in
withHelpers
