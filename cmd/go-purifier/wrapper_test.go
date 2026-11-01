// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkingDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"build", "./..."}, cwd},
		{[]string{"-C", "sub", "build"}, filepath.Join(cwd, "sub")},
		{[]string{"-C=sub", "build"}, filepath.Join(cwd, "sub")},
		{[]string{"-C", "/abs/dir", "test"}, "/abs/dir"},
		{[]string{"build", "-C", "sub"}, cwd},
	}
	for _, c := range cases {
		got, err := workingDir(c.args)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("workingDir(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestFindModuleRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := findModuleRoot(deep)
	if !ok || got != root {
		t.Errorf("findModuleRoot(%q) = %q, %v; want %q", deep, got, ok, root)
	}
}

// A module without a lockfile, and a module whose go.mod changed since its
// lockfile was written, run go unchanged, without Nix.
func TestInteractiveEnvLeavesUnlockedModulesAlone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := interactiveEnv(root)
	if err != nil || env != nil {
		t.Fatalf("interactiveEnv without a lockfile = %v, %v; want nothing", env, err)
	}

	stale := &Lock{Version: lockFormatVersion, DefinitionHash: "sha256-stale", Binaries: []Binary{}, Artifacts: []Artifact{}}
	if err := writeLock(filepath.Join(root, lockFileName), stale); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_PURIFIER_NIX_LIB", "")
	env, err = interactiveEnv(root)
	if err != nil || env != nil {
		t.Fatalf("interactiveEnv with a stale lockfile = %v, %v; want nothing", env, err)
	}
}
