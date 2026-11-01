// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBinaryName(t *testing.T) {
	cases := map[string]string{
		"example.com/hello":            "hello",
		"example.com/hello/cmd/greet":  "greet",
		"example.com/hello/v2":         "hello",
		"example.com/hello/v2/cmd/sub": "sub",
		"hello":                        "hello",
	}
	for path, want := range cases {
		if got := binaryName(path); got != want {
			t.Errorf("binaryName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestBinariesOfRejectsClashingNames(t *testing.T) {
	if _, err := binariesOf([]string{"example.com/a/tool", "example.com/b/tool"}); err == nil {
		t.Fatal("two main packages named tool were accepted")
	}
}

func TestLockable(t *testing.T) {
	cases := map[string]bool{
		"golang.org/x/text/@v/v0.14.0.zip":     true,
		"golang.org/x/text/@v/v0.14.0.mod":     true,
		"golang.org/x/text/@v/v0.14.0.info":    false,
		"golang.org/x/text/@v/list":            false,
		"golang.org/x/text/@v/v0.14.0.lock":    false,
		"golang.org/x/text/@v/v0.14.0.ziphash": false,
		"golang.org/x/text/@v/v0.14.0.partial": false,
		"cache.mod":                            false,
	}
	for rel, want := range cases {
		if got := lockable(rel); got != want {
			t.Errorf("lockable(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestCollectArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"github.com/!burnt!sushi/toml/@v/v1.3.2.mod": "module github.com/BurntSushi/toml\n",
		"github.com/!burnt!sushi/toml/@v/v1.3.2.zip": "zip",
		"github.com/!burnt!sushi/toml/@v/list":       "v1.3.2\n",
		"example.com/private/@v/v1.0.0.mod":          "module example.com/private\n",
		"sumdb/sum.golang.org/lookup/x":              "tile",
	})
	got, err := collectArtifacts(dir, "https://proxy.example", "example.com/private")
	if err != nil {
		t.Fatal(err)
	}
	want := []Artifact{
		{
			Path:    "github.com/!burnt!sushi/toml/@v/v1.3.2.mod",
			URL:     "https://proxy.example/github.com/!burnt!sushi/toml/@v/v1.3.2.mod",
			Mirrors: &[]string{},
			Hash:    "sha256-KAIbQYClnDmTYHqVsY4jDdC8a+pSQv/o6ou/tPT3tNc=",
		},
		{
			Path:    "github.com/!burnt!sushi/toml/@v/v1.3.2.zip",
			URL:     "https://proxy.example/github.com/!burnt!sushi/toml/@v/v1.3.2.zip",
			Mirrors: &[]string{},
			Hash:    "sha256-SnD+mqZDbgLC3qNA+9HjUuTvLYzmylKtJdS5VHH8i/I=",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectArtifacts = %#v, want %#v", got, want)
	}
}

func TestCollectArtifactsOfMissingCache(t *testing.T) {
	got, err := collectArtifacts(filepath.Join(t.TempDir(), "absent"), "https://proxy.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("collectArtifacts of a missing cache = %v, want none", got)
	}
}

func TestVCSModules(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"example.com/!private/@v/v1.0.0.info":     `{"Version":"v1.0.0","Time":"2024-01-02T03:04:05Z","Origin":{"VCS":"git","URL":"https://git.example/private","Hash":"0123456789abcdef0123456789abcdef01234567","Ref":"refs/tags/v1.0.0"}}`,
		"example.com/!private/sub/@v/v0.1.0.info": `{"Version":"v0.1.0","Time":"2024-01-02T03:04:05Z","Origin":{"VCS":"git","URL":"https://git.example/private","Subdir":"sub","Hash":"0123456789abcdef0123456789abcdef01234567"}}`,
		"example.com/public/@v/v1.0.0.info":       `{"Version":"v1.0.0","Time":"2024-01-02T03:04:05Z"}`,
	})
	prefetches := 0
	tools := vcsTools{
		resolve: func(vcs, url, ref string) (string, error) { return "", errors.New("git origins carry their commit") },
		prefetch: func(Artifact) (string, error) {
			prefetches++
			return "sha256-checkout", nil
		},
	}
	mods, checkouts, err := vcsModules(dir, "example.com/Private", tools)
	if err != nil {
		t.Fatal(err)
	}
	if prefetches != 1 || len(checkouts) != 1 {
		t.Fatalf("two modules of one revision were prefetched %d times into %d entries", prefetches, len(checkouts))
	}
	entry := checkouts[0]
	if entry.Git == nil || entry.Git.URL != "https://git.example/private" || entry.Git.Rev != "0123456789abcdef0123456789abcdef01234567" || entry.Git.Submodules || entry.Hash != "sha256-checkout" {
		t.Errorf("git entry = %#v", entry)
	}
	want := []VCSModule{
		{Module: "example.com/Private", Version: "v1.0.0", ProxyDir: "example.com/!private/@v", Time: "2024-01-02T03:04:05Z", Entry: entry.Path},
		{Module: "example.com/Private/sub", Version: "v0.1.0", ProxyDir: "example.com/!private/sub/@v", Time: "2024-01-02T03:04:05Z", Entry: entry.Path, Subdir: "sub"},
	}
	if !reflect.DeepEqual(mods, want) {
		t.Errorf("vcsModules = %#v, want %#v", mods, want)
	}
}

func TestPseudoVersionRevision(t *testing.T) {
	cases := map[string]string{
		"v0.0.0-20261101084936-000000000001": "1",
		"v0.0.0-20261101084936-000000004200": "4200",
		"v0.0.0-20261101084936-bc8d4a154f48": "",
		"v1.2.3":                             "",
	}
	for version, want := range cases {
		if got := pseudoVersionRevision(version); got != want {
			t.Errorf("pseudoVersionRevision(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestHTTPSURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:org/repo.git":        "https://github.com/org/repo.git",
		"ssh://git@github.com/org/repo":      "https://github.com/org/repo",
		"ssh://git@git.example:2222/org/rep": "https://git.example/org/rep",
		"https://github.com/org/repo":        "https://github.com/org/repo",
		"file:///srv/git/repo":               "file:///srv/git/repo",
	}
	for in, want := range cases {
		if got := httpsURL(in); got != want {
			t.Errorf("httpsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// Each version control system gets the entry kind the nix-purifiers share.
// Mercurial and Fossil origins carry their revision; Subversion and Bazaar
// origins carry only the reference, which the lock resolves.
func TestCheckoutEntry(t *testing.T) {
	resolved := map[string]string{
		"svn https://svn.example/repo v1.0.0": "42",
		"bzr https://bzr.example/repo v1.0.0": "revid:jdoe@example-20240102-abc",
	}
	tools := vcsTools{
		resolve: func(vcs, url, ref string) (string, error) {
			if rev, ok := resolved[vcs+" "+url+" "+ref]; ok {
				return rev, nil
			}
			return "", errors.New("no revision")
		},
	}
	cases := []struct {
		vcs, url, hash, ref string
		want                Artifact
	}{
		{"hg", "ssh://hg@hg.example/repo", "0123456789ab", "", Artifact{Hg: &HgSource{URL: "https://hg.example/repo", Rev: "0123456789ab"}}},
		{"svn", "https://svn.example/repo", "", "v1.0.0", Artifact{Svn: &SvnSource{URL: "https://svn.example/repo", Rev: "42", IgnoreExternals: true, IgnoreKeywords: true}}},
		{"bzr", "https://bzr.example/repo", "", "v1.0.0", Artifact{Bzr: &RevSource{URL: "https://bzr.example/repo", Rev: "revid:jdoe@example-20240102-abc"}}},
		{"fossil", "https://fossil.example/repo", "abcdef0123", "", Artifact{Fossil: &RevSource{URL: "https://fossil.example/repo", Rev: "abcdef0123"}}},
	}
	for _, c := range cases {
		got, err := checkoutEntry(c.vcs, c.url, c.hash, c.ref, tools)
		if err != nil {
			t.Errorf("checkoutEntry(%s): %v", c.vcs, err)
			continue
		}
		if !strings.HasPrefix(got.Path, "vcs/") || strings.ContainsAny(strings.TrimPrefix(got.Path, "vcs/"), "/:@") {
			t.Errorf("checkoutEntry(%s).Path = %q, want a plain name under vcs/", c.vcs, got.Path)
		}
		got.Path = ""
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("checkoutEntry(%s) = %#v, want %#v", c.vcs, got, c.want)
		}
	}
	if _, err := checkoutEntry("darcs", "https://example/repo", "abc", "", tools); err == nil {
		t.Error("an unknown version control system was accepted")
	}
}

func TestDefinitionHash(t *testing.T) {
	dir := t.TempDir()
	files := []string{"go.mod", "go.sum"}
	writeFiles(t, dir, map[string]string{"go.mod": "module example.com/m\n"})
	withoutSum, err := definitionHash(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{"go.sum": ""})
	withEmptySum, err := definitionHash(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if withoutSum == withEmptySum {
		t.Error("a missing go.sum and an empty one have the same definition hash")
	}
	writeFiles(t, dir, map[string]string{"go.mod": "module example.com/m\n\ngo 1.24\n"})
	changed, err := definitionHash(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if changed == withEmptySum {
		t.Error("changing go.mod left the definition hash unchanged")
	}
	reordered, err := definitionHash(dir, []string{"go.sum", "go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if reordered != changed {
		t.Error("the definition hash depends on the order of the files")
	}
}

func TestDefinitionFiles(t *testing.T) {
	root := "/w"
	got, err := definitionFiles(root, true, []string{"/w/app", "/w/lib", "/lib2", "/w/app"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"../lib2/go.mod", "../lib2/go.sum", "app/go.mod", "app/go.sum", "go.work", "go.work.sum", "lib/go.mod", "lib/go.sum"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("definitionFiles = %q, want %q", got, want)
	}
}
