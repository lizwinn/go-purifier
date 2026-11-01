// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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
	files := map[string]string{
		"github.com/!burnt!sushi/toml/@v/v1.3.2.mod": "module github.com/BurntSushi/toml\n",
		"github.com/!burnt!sushi/toml/@v/v1.3.2.zip": "zip",
		"github.com/!burnt!sushi/toml/@v/list":       "v1.3.2\n",
		"sumdb/sum.golang.org/lookup/x":              "tile",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := collectArtifacts(dir, "https://proxy.example")
	if err != nil {
		t.Fatal(err)
	}
	want := []Artifact{
		{
			Path: "github.com/!burnt!sushi/toml/@v/v1.3.2.mod",
			URL:  "https://proxy.example/github.com/!burnt!sushi/toml/@v/v1.3.2.mod",
			Hash: "sha256-KAIbQYClnDmTYHqVsY4jDdC8a+pSQv/o6ou/tPT3tNc=",
		},
		{
			Path: "github.com/!burnt!sushi/toml/@v/v1.3.2.zip",
			URL:  "https://proxy.example/github.com/!burnt!sushi/toml/@v/v1.3.2.zip",
			Hash: "sha256-SnD+mqZDbgLC3qNA+9HjUuTvLYzmylKtJdS5VHH8i/I=",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectArtifacts = %#v, want %#v", got, want)
	}
}

func TestCollectArtifactsOfMissingCache(t *testing.T) {
	got, err := collectArtifacts(filepath.Join(t.TempDir(), "absent"), "https://proxy.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("collectArtifacts of a missing cache = %v, want none", got)
	}
}

func TestDefinitionHash(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n")
	withoutSum, err := definitionHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	write("go.sum", "")
	withEmptySum, err := definitionHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if withoutSum == withEmptySum {
		t.Error("a missing go.sum and an empty one have the same definition hash")
	}
	write("go.mod", "module example.com/m\n\ngo 1.24\n")
	changed, err := definitionHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed == withEmptySum {
		t.Error("changing go.mod left the definition hash unchanged")
	}
}
