// SPDX-License-Identifier: MIT

// Command go-purifier makes Nix the dependency backend of a Go build.
//
// `go-purifier lock` runs the build once with an empty module cache and
// records every file the go command downloads in go-purifier.lock.json,
// next to go.mod, or next to go.work for a workspace. The Nix library of
// this repository (nix/lib) builds from that lockfile in the Nix sandbox,
// offline.
//
// Invoked as `go` (the flake's `toolchain` package links it under that
// name), the same binary runs the real go command. When the build has a
// current lockfile, it first realizes the locked files through Nix and
// points GOPROXY at them, so interactive builds resolve offline from
// exactly the files the Nix build uses.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const usageText = `usage: go-purifier <command>

Commands:
  lock               write go-purifier.lock.json for the module or
                     workspace of the current directory, unless the files
                     that define its dependencies are unchanged since the
                     last lock
  relock             write go-purifier.lock.json even if it is current
  release <version>  build the release archives of the module's commands
                     through Nix, and tag the release; with --push, also
                     push the tag, create a GitHub release and ask the
                     module proxy for the version
  go ...             run the go command against the locked downloads; the
                     same as invoking this binary as "go"

Environment:
  GO_PURIFIER_GO       the go command to run (the flake's wrapper sets it)
  GO_PURIFIER_NIX_LIB  the go-purifier Nix library (the flake's wrapper
                       sets it)
  GO_PURIFIER_PROXY    the module proxy a lock downloads from; the default
                       is the first proxy URL in GOPROXY, or
                       https://proxy.golang.org
  GONOPROXY, GOPRIVATE the modules a lock fetches from version control
  GO_PURIFIER=0        run go without the locked downloads
`

func main() {
	if filepath.Base(os.Args[0]) == "go" {
		os.Exit(runGo(os.Args[1:]))
	}
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "lock":
		err = lockCommand(false)
	case "relock":
		err = lockCommand(true)
	case "release":
		err = releaseCommand(os.Args[2:])
	case "go":
		os.Exit(runGo(os.Args[2:]))
	// The builders of the Nix derivations nix/lib makes; see vcs.go and
	// wrapper.go.
	case "module-files":
		err = moduleFilesCommand(os.Args[2:])
	case "link-tree":
		err = linkTreeCommand()
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return
	default:
		fmt.Fprintf(os.Stderr, "go-purifier: unknown command %q\n\n%s", os.Args[1], usageText)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-purifier: %v\n", err)
		os.Exit(1)
	}
}
