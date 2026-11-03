// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeModuleProjection(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("dep/go.mod", "module example.com/dep/v2\n\ngo 1.24\n")
	write("dep/dep.go", "package dep\nfunc Message() string { return \"native projection\" }\n")
	write("lib/go.mod", "module example.com/lib\n\ngo 1.24\nrequire example.com/dep/v2 v2.99.0\nreplace example.com/dep/v2 => ../dep\n")
	write("lib/lib.go", "package lib\nimport \"example.com/dep/v2\"\nfunc Message() string {return dep.Message()}\n")
	request := moduleProjectionRequest{Identity: "nix-source-identity", Modules: []string{filepath.Join(root, "dep"), filepath.Join(root, "lib")}}
	data, _ := json.Marshal(request)
	write("request.json", string(data))
	proxy := filepath.Join(root, "proxy")
	if err := projectModules(filepath.Join(root, "request.json"), proxy); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(proxy, ".catalog"))
	if err != nil {
		t.Fatal(err)
	}

	reused := filepath.Join(root, "reused")
	reuseRequest, _ := json.Marshal(moduleProjectionRequest{Identity: "another-producer", Repositories: []string{proxy}})
	write("reuse.json", string(reuseRequest))
	if err := projectModules(filepath.Join(root, "reuse.json"), reused); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(proxy, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(proxy, path)
		if err != nil {
			return err
		}
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		copied, err := os.ReadFile(filepath.Join(reused, rel))
		if err != nil {
			return err
		}
		if !bytes.Equal(original, copied) {
			t.Errorf("supplied proxy file changed: %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var catalog moduleProjectionCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	var version string
	for _, m := range catalog.Modules {
		if m.Path == "example.com/lib" {
			version = m.Version
		}
	}
	if version == "" {
		t.Fatal("library missing")
	}
	write("consumer/go.mod", "module example.com/consumer\n\ngo 1.24\nrequire example.com/lib "+version+"\n")
	write("consumer/main.go", "package main\nimport (\"fmt\"; \"example.com/lib\")\nfunc main(){fmt.Println(lib.Message())}\n")
	cmd := exec.Command(goBin, "run", "-mod=mod", ".")
	cmd.Dir = filepath.Join(root, "consumer")
	cmd.Env = append(os.Environ(), "GOPROXY=file://"+proxy, "GOSUMDB=off", "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOMODCACHE="+filepath.Join(root, "cache"))
	defer func() {
		clean := exec.Command(goBin, "clean", "-modcache")
		clean.Env = cmd.Env
		if out, err := clean.CombinedOutput(); err != nil {
			t.Errorf("clean module cache: %v: %s", err, out)
		}
	}()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ordinary downstream: %v\n%s", err, out)
	}
	if !strings.HasSuffix(string(out), "native projection\n") {
		t.Fatal(string(out))
	}

	// A consumer may retain an arbitrary upstream version in its manifest.
	write("consumer/go.mod", "module example.com/consumer\n\ngo 1.24\nrequire example.com/lib v0.99.0\n")
	work := filepath.Join(root, "bound.work")
	if err := bindModuleInputs([]string{proxy}, cmd.Dir, work); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(goBin, "run", ".")
	cmd.Dir = filepath.Join(root, "consumer")
	cmd.Env = append(os.Environ(), "GOPROXY=file://"+proxy, "GOSUMDB=off", "GOTOOLCHAIN=local", "GOENV=off", "GOWORK="+work, "GOMODCACHE="+filepath.Join(root, "cache"))
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native workspace binding: %v\n%s", err, out)
	}
	original, err := os.ReadFile(filepath.Join(cmd.Dir, "go.mod"))
	if err != nil || !strings.Contains(string(original), "v0.99.0") {
		t.Fatalf("binding changed manifest: %s, %v", original, err)
	}

	locked, err := makeLock(goBin, cmd.Dir, false, "off", lockSettings{Work: work, Repositories: []string{proxy}, NoProxy: "*", Private: "*", VCS: "*:off"}, vcsTools{})
	if err != nil {
		t.Fatalf("lock supplied graph: %v", err)
	}
	if len(locked.Files) != 0 || len(locked.Checkouts) != 0 {
		t.Fatalf("provided modules entered lock: %+v", locked)
	}

	alternate, finish, err := moduleEdit(cmd.Dir, []string{proxy})
	if err != nil {
		t.Fatal(err)
	}
	tidy := exec.Command(goBin, "mod", "tidy")
	tidy.Dir = cmd.Dir
	tidy.Env = append(cmd.Env, "GOWORK=off", "GOFLAGS=-modfile="+alternate)
	tidyOut, tidyErr := tidy.CombinedOutput()
	if err := finish(tidyErr == nil); err != nil {
		t.Fatal(err)
	}
	if tidyErr != nil {
		t.Fatalf("native module editing: %v: %s", tidyErr, tidyOut)
	}
	edited, err := os.ReadFile(filepath.Join(cmd.Dir, "go.mod"))
	if err != nil || strings.Contains(string(edited), "replace") || !strings.Contains(string(edited), "v0.99.0") {
		t.Fatalf("module edit leaked binding: %s, %v", edited, err)
	}
	if sums, err := os.ReadFile(filepath.Join(cmd.Dir, "go.sum")); err == nil && strings.Contains(string(sums), "example.com") {
		t.Fatalf("provided module hashes leaked: %s", sums)
	}
	mod, err := os.ReadFile(filepath.Join(proxy, "example.com/lib/@v", version+".mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), "replace") || strings.Contains(string(mod), "v2.99.0") {
		t.Fatalf("unportable metadata: %s", mod)
	}
}
