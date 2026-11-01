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

func TestFindLockRoot(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"app/go.mod":     "module example.com/app\n",
		"app/cmd/x/x.go": "package main\n",
	})
	deep := filepath.Join(root, "app", "cmd", "x")

	t.Setenv("GOWORK", "")
	if got, ok := findLockRoot(deep); !ok || got != filepath.Join(root, "app") {
		t.Errorf("findLockRoot of a module = %q, %v; want the module directory", got, ok)
	}

	writeFiles(t, root, map[string]string{"go.work": "go 1.24\n\nuse ./app\n"})
	if got, ok := findLockRoot(deep); !ok || got != root {
		t.Errorf("findLockRoot in a workspace = %q, %v; want the workspace directory", got, ok)
	}

	t.Setenv("GOWORK", "off")
	if got, ok := findLockRoot(deep); !ok || got != filepath.Join(root, "app") {
		t.Errorf("findLockRoot with GOWORK=off = %q, %v; want the module directory", got, ok)
	}
}

// A build without a lockfile, and a build whose go.mod changed since its
// lockfile was written, run go unchanged, without Nix.
func TestInteractiveEnvLeavesUnlockedBuildsAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWORK", "off")
	writeFiles(t, root, map[string]string{"go.mod": "module example.com/m\n"})
	env, err := interactiveEnv(root)
	if err != nil || env != nil {
		t.Fatalf("interactiveEnv without a lockfile = %v, %v; want nothing", env, err)
	}

	stale := &Lock{Version: lockFormatVersion, DefinitionFiles: []string{"go.mod", "go.sum"}, DefinitionHash: "sha256-stale"}
	if err := writeLock(filepath.Join(root, lockFileName), stale); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_PURIFIER_NIX_LIB", "")
	env, err = interactiveEnv(root)
	if err != nil || env != nil {
		t.Fatalf("interactiveEnv with a stale lockfile = %v, %v; want nothing", env, err)
	}
}

func TestLinkTree(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest")
	writeFiles(t, dir, map[string]string{"manifest": "a/@v/v1.0.0.zip\t/nix/store/x-a.zip\nb/c/@v/v2.0.0.mod\t/nix/store/y-c.mod\n"})
	out := filepath.Join(dir, "out")
	t.Setenv("out", out)
	t.Setenv("manifestPath", manifest)
	if err := linkTreeCommand(); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"a/@v/v1.0.0.zip": "/nix/store/x-a.zip", "b/c/@v/v2.0.0.mod": "/nix/store/y-c.mod"} {
		got, err := os.Readlink(filepath.Join(out, link))
		if err != nil || got != target {
			t.Errorf("link %s = %q, %v; want %q", link, got, err, target)
		}
	}
}
