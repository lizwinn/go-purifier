// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// withToolLock runs on the outside go.sum, writes the result there, and
// leaves the go.sum of the tree as it was.
func TestWithToolLock(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command on PATH")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GO_PURIFIER_TOOL_LOCKFILE", "")
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"go.mod": "module example.com/m\n", "go.sum": "tree\n"})
	outside := filepath.Join(t.TempDir(), "sums", "go.sum")
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	// An outside go.sum that does not exist starts from the go.sum of the
	// tree.
	err = withToolLock(goBin, root, outside, func() error {
		if got := read(filepath.Join(root, "go.sum")); got != "tree\n" {
			t.Errorf("the run saw go.sum %q, want the go.sum of the tree", got)
		}
		return os.WriteFile(filepath.Join(root, "go.sum"), []byte("first\n"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(outside); got != "first\n" {
		t.Errorf("outside go.sum = %q, want what the run wrote", got)
	}
	if got := read(filepath.Join(root, "go.sum")); got != "tree\n" {
		t.Errorf("go.sum of the tree = %q, want it unchanged", got)
	}

	// An outside go.sum that exists is what the run sees; a failed run
	// leaves it as it was.
	failure := errors.New("failed")
	err = withToolLock(goBin, root, outside, func() error {
		if got := read(filepath.Join(root, "go.sum")); got != "first\n" {
			t.Errorf("the run saw go.sum %q, want the outside go.sum", got)
		}
		if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte("second\n"), 0o644); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("withToolLock = %v, want the error of the run", err)
	}
	if got := read(outside); got != "first\n" {
		t.Errorf("outside go.sum after a failed run = %q, want it unchanged", got)
	}
	if got := read(filepath.Join(root, "go.sum")); got != "tree\n" {
		t.Errorf("go.sum of the tree after a failed run = %q, want it unchanged", got)
	}
}
