// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// withToolLock runs fn on the go.sum of an external build: the go.sum that
// --tool-lockfile or GO_PURIFIER_TOOL_LOCKFILE names, outside the source
// tree. The go command reads go.sum next to go.mod, so withToolLock puts
// the outside go.sum in place of the go.sum in the tree while fn runs.
// When fn succeeds, withToolLock writes the go.sum that fn leaves to the
// outside path. Either way it puts back the go.sum the tree had. An outside
// go.sum that does not exist yet starts as a copy of the go.sum in the
// tree. Without an outside go.sum, fn runs on the go.sum in the tree.
func withToolLock(goBin, dir, toolFlag string, fn func() error) error {
	outside := toolLockPath(toolFlag)
	if outside == "" {
		return fn()
	}
	env, err := readGoEnv(goBin, dir)
	if err != nil {
		return err
	}
	root, workspace, err := env.lockRoot()
	if err != nil {
		return err
	}
	if workspace {
		return errors.New("--tool-lockfile and GO_PURIFIER_TOOL_LOCKFILE name the go.sum of one module, and a workspace has a go.sum in each module")
	}
	inTree := filepath.Join(root, "go.sum")
	original, err := os.ReadFile(inTree)
	hadOriginal := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	start, err := os.ReadFile(outside)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		start, err = original, nil
	case err != nil:
		return err
	}
	if err := os.WriteFile(inTree, start, 0o644); err != nil {
		return err
	}
	runErr := fn()
	var saveErr error
	if runErr == nil {
		result, err := os.ReadFile(inTree)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(outside), 0o755)
		}
		if err == nil {
			err = os.WriteFile(outside, result, 0o644)
		}
		if err == nil {
			fmt.Fprintf(os.Stderr, "go-purifier: wrote the go.sum of %s to %s\n", root, outside)
		}
		saveErr = err
	}
	var restoreErr error
	if hadOriginal {
		restoreErr = os.WriteFile(inTree, original, 0o644)
	} else {
		restoreErr = os.Remove(inTree)
	}
	return errors.Join(runErr, saveErr, restoreErr)
}
