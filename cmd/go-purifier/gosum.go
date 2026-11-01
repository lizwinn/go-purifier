// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// completeGoSum adds the go.sum entries that the module at root is missing,
// when go.mod needs no change. The lock run uses -mod=readonly and so
// stops on an incomplete go.sum. `go mod tidy -diff` prints the changes
// `go mod tidy` would make without making them. When they change go.sum
// alone, and add lines to it, completeGoSum runs `go mod tidy`, which
// writes the go.sum entries of every module the build needs. In every
// other case completeGoSum writes nothing, so go.mod keeps the content the
// user gave it, and the lock run stops with the go command's own error
// when go.sum is incomplete. The go command reads the user's GOPROXY and
// module cache here, and not the empty cache of the lock run, so the
// lockfile holds only what the build itself downloads.
func completeGoSum(goBin, root string) error {
	env := append(os.Environ(), "GOFLAGS=", "GOTOOLCHAIN=local")
	cmd := exec.Command(goBin, "mod", "tidy", "-diff")
	cmd.Dir = root
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return fmt.Errorf("go mod tidy -diff: %w", err)
	}
	if err == nil || !onlyAddsToGoSum(stdout.String()) {
		return nil
	}
	fmt.Fprintln(os.Stderr, "go-purifier: adding the missing go.sum entries with go mod tidy, which leaves go.mod as it is")
	_, err = goOutput(goBin, root, env, "mod", "tidy")
	return err
}

// onlyAddsToGoSum reports whether a unified diff of `go mod tidy -diff`
// changes go.sum alone and adds at least one line to it.
func onlyAddsToGoSum(diff string) bool {
	adds := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			if !strings.HasSuffix(strings.TrimSpace(line), "/go.sum") {
				return false
			}
		case strings.HasPrefix(line, "+"):
			adds = true
		}
	}
	return adds
}
