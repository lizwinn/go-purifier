// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
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
	env, proxy, err := interactiveEnv("go", root, false)
	if err != nil || env != nil || proxy != "" {
		t.Fatalf("interactiveEnv without a lockfile = %v, %q, %v; want nothing", env, proxy, err)
	}

	stale := &Lock{Version: lockFormatVersion, DefinitionFiles: []string{"go.mod", "go.sum"}, DefinitionHash: "sha256-stale"}
	if err := writeLock(filepath.Join(root, lockFileName), stale); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_PURIFIER_NIX_LIB", "")
	for _, changing := range []bool{false, true} {
		env, proxy, err = interactiveEnv("go", root, changing)
		if err != nil || env != nil || proxy != "" {
			t.Fatalf("interactiveEnv with a stale lockfile = %v, %q, %v; want nothing", env, proxy, err)
		}
	}
}

// A lockfile that is not JSON gets an error that names the command that
// writes it again.
func TestInteractiveEnvRejectsMalformedLockfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWORK", "off")
	writeFiles(t, root, map[string]string{"go.mod": "module example.com/m\n", lockFileName: "{ not json"})
	_, _, err := interactiveEnv("go", root, false)
	if err == nil || !strings.Contains(err.Error(), "go-purifier relock") {
		t.Fatalf("interactiveEnv with a malformed lockfile = %v; want an error that names go-purifier relock", err)
	}
}

func TestChangesDependencies(t *testing.T) {
	cases := map[string]bool{
		"build ./...":                       false,
		"test ./...":                        false,
		"run .":                             false,
		"vet ./...":                         false,
		"get example.com/x@v1.2.3":          true,
		"get -u ./...":                      true,
		"-C sub get example.com/x":          true,
		"mod tidy":                          true,
		"mod download":                      true,
		"mod graph":                         false,
		"install example.com/cmd/x@latest":  true,
		"install ./cmd/x":                   false,
		"run example.com/cmd/x@v1.0.0 -arg": true,
		"list -m -u all":                    true,
		"list -m all":                       false,
		"work sync":                         true,
		"":                                  false,
	}
	for line, want := range cases {
		if got := changesDependencies(strings.Fields(line)); got != want {
			t.Errorf("changesDependencies(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestTailBuffer(t *testing.T) {
	var b tailBuffer
	b.Write([]byte(strings.Repeat("a", 70<<10)))
	b.Write([]byte("end"))
	if len(b.data) != 64<<10 || !strings.HasSuffix(b.String(), "end") {
		t.Errorf("tailBuffer kept %d bytes ending %q", len(b.data), b.String()[len(b.data)-3:])
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
