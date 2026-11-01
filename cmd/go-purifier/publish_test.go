// SPDX-License-Identifier: MIT

package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

func TestReleaseTag(t *testing.T) {
	cases := []struct {
		modRel, modPath, version, want string
	}{
		{".", "example.com/m", "v1.2.3", "v1.2.3"},
		{"sub", "example.com/m/sub", "v0.1.0", "sub/v0.1.0"},
		{"v2", "example.com/m/v2", "v2.0.0", "v2.0.0"},
		{"sub/v3", "example.com/m/sub/v3", "v3.1.0", "sub/v3.1.0"},
		{".", "example.com/m/v2", "v2.0.0", "v2.0.0"},
	}
	for _, c := range cases {
		if got := releaseTag(c.modRel, c.modPath, c.version); got != c.want {
			t.Errorf("releaseTag(%q, %q, %q) = %q, want %q", c.modRel, c.modPath, c.version, got, c.want)
		}
	}
}

func TestCheckReleaseVersion(t *testing.T) {
	ok := []struct{ path, version string }{
		{"example.com/m", "v1.2.3"},
		{"example.com/m", "v0.1.0-rc.1"},
		{"example.com/m/v2", "v2.0.0"},
	}
	for _, c := range ok {
		if err := checkReleaseVersion(c.path, c.version); err != nil {
			t.Errorf("checkReleaseVersion(%q, %q) = %v", c.path, c.version, err)
		}
	}
	bad := []struct{ path, version string }{
		{"example.com/m", "1.2.3"},
		{"example.com/m", "v1.2"},
		{"example.com/m", "v1.2.3+build"},
		{"example.com/m", "v2.0.0"},
		{"example.com/m/v2", "v1.0.0"},
	}
	for _, c := range bad {
		if err := checkReleaseVersion(c.path, c.version); err == nil {
			t.Errorf("checkReleaseVersion(%q, %q) accepted the version", c.path, c.version)
		}
	}
}

func TestWriteModuleFiles(t *testing.T) {
	checkout := t.TempDir()
	writeFiles(t, checkout, map[string]string{
		"sub/go.mod":        "module example.com/repo/sub\n",
		"sub/a.go":          "package sub\n",
		"sub/nested/go.mod": "module example.com/repo/sub/nested\n",
		"sub/nested/b.go":   "package nested\n",
		"nogomod/c.go":      "package nogomod\n",
		"nogomod/LICENSE":   "own license\n",
		"LICENSE":           "root license\n",
	})
	out := filepath.Join(t.TempDir(), "out")
	if err := writeModuleFiles(checkout, "sub", "example.com/repo/sub", "v1.0.0", out); err != nil {
		t.Fatal(err)
	}
	if gomod, err := os.ReadFile(filepath.Join(out, "v1.0.0.mod")); err != nil || string(gomod) != "module example.com/repo/sub\n" {
		t.Errorf("v1.0.0.mod = %q, %v", gomod, err)
	}
	if _, err := os.Stat(filepath.Join(out, "v1.0.0.info")); err == nil {
		t.Error("module-files wrote a .info file, which builds do not read")
	}
	cf, err := modzip.CheckZip(module.Version{Path: "example.com/repo/sub", Version: "v1.0.0"}, filepath.Join(out, "v1.0.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/repo/sub@v1.0.0/LICENSE", "example.com/repo/sub@v1.0.0/a.go", "example.com/repo/sub@v1.0.0/go.mod"}
	if got := sortedNames(cf.Valid); !reflect.DeepEqual(got, want) {
		t.Errorf("the zip holds %q; want %q: the root LICENSE, without the nested module", got, want)
	}

	out2 := filepath.Join(t.TempDir(), "out")
	if err := writeModuleFiles(checkout, "nogomod", "example.com/nogomod", "v1.0.0", out2); err != nil {
		t.Fatal(err)
	}
	if gomod, err := os.ReadFile(filepath.Join(out2, "v1.0.0.mod")); err != nil || string(gomod) != "module example.com/nogomod\n" {
		t.Errorf("v1.0.0.mod of a module without go.mod = %q, %v", gomod, err)
	}
	r, err := zip.OpenReader(filepath.Join(out2, "v1.0.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "/LICENSE") {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			if string(data) != "own license\n" {
				t.Errorf("a module with its own LICENSE has %q in its zip", data)
			}
		}
	}
}

func sortedNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}
