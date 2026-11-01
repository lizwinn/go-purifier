// SPDX-License-Identifier: MIT

// Command go-purifier makes Nix the dependency backend of a Go module.
//
// `go-purifier lock` runs the module's build once with an empty module
// cache and records every file the go command downloads from the module
// proxy in go-purifier.lock.json, next to go.mod. The Nix library of this
// repository (nix/lib) builds the module from that lockfile in the Nix
// sandbox, offline.
//
// Invoked as `go` (the flake's `go` package links it under that name),
// the same binary runs the real go command. When the module has a current
// lockfile, it first realizes the locked files through Nix and points
// GOPROXY at them, so interactive builds resolve offline from exactly the
// files the Nix build uses.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const usageText = `usage: go-purifier <command>

Commands:
  lock     write go-purifier.lock.json for the module in the current
           directory, unless go.mod and go.sum are unchanged since the
           last lock
  relock   write go-purifier.lock.json even if it is current
  go ...   run the go command against the locked downloads; the same as
           invoking this binary as "go"

Environment:
  GO_PURIFIER_GO       the go command to run (the flake's wrapper sets it)
  GO_PURIFIER_NIX_LIB  the go-purifier Nix library (the flake's wrapper
                       sets it)
  GO_PURIFIER_PROXY    the module proxy a lock downloads from; the default
                       is the first proxy URL in GOPROXY, or
                       https://proxy.golang.org
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
	case "go":
		os.Exit(runGo(os.Args[2:]))
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
