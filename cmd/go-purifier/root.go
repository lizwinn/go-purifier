// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// The lock root is the directory the go command treats as the root of the
// build: the directory of go.work in workspace mode, or else the directory
// of the main module's go.mod. The lockfile sits there.

// findLockRoot is the lock root for a go command run in dir, found the way
// the go command finds go.work and go.mod. GOWORK=off disables workspace
// mode, and GOWORK set to a file names the workspace.
func findLockRoot(dir string) (string, bool) {
	switch gowork := os.Getenv("GOWORK"); {
	case gowork == "off":
	case gowork != "" && gowork != "auto":
		if !filepath.IsAbs(gowork) {
			gowork = filepath.Join(dir, gowork)
		}
		return filepath.Dir(gowork), true
	default:
		if root, ok := findUp(dir, "go.work"); ok {
			return root, true
		}
	}
	return findUp(dir, "go.mod")
}

// findUp is the nearest directory at or above dir that holds a file name.
func findUp(dir, name string) (string, bool) {
	for {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// definitionHash is the SHA-256, in SRI form, of the files that define a
// build's dependencies, given relative to the lock root. A missing file and
// an empty one hash differently.
func definitionHash(root string, files []string) (string, error) {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, name := range sorted {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			fmt.Fprintf(h, "%s\x00absent\x00", name)
		case err != nil:
			return "", err
		default:
			fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
			h.Write(data)
		}
	}
	return sri(h.Sum(nil)), nil
}

// definitionFiles are the files that define the dependencies of a build
// whose module directories (workspace modules and local replacements) are
// dirs: go.work and go.work.sum in workspace mode, and the go.mod and
// go.sum of every module directory. Paths are relative to the lock root.
func definitionFiles(root string, workspace bool, dirs []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	add := func(rel string) {
		if !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	if workspace {
		add("go.work")
		add("go.work.sum")
	}
	for _, dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		add(filepath.ToSlash(filepath.Join(rel, "go.mod")))
		add(filepath.ToSlash(filepath.Join(rel, "go.sum")))
	}
	sort.Strings(files)
	return files, nil
}
