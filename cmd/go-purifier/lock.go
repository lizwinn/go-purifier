// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The lockfile sits next to go.mod.
const lockFileName = "go-purifier.lock.json"

// The lock format version this binary writes and reads. nix/lib/lock-version.nix
// holds the same number.
const lockFormatVersion = 1

const defaultProxy = "https://proxy.golang.org"

// Lock is the content of go-purifier.lock.json.
type Lock struct {
	Version int `json:"version"`
	// The path of the main module, from go.mod.
	Module string `json:"module"`
	// The go version the lock was made with, such as go1.26.10.
	GoVersion string `json:"goVersion"`
	// The module proxy every artifact was downloaded from.
	Proxy string `json:"proxy"`
	// A digest of go.mod and go.sum. A lockfile whose digest differs from
	// the module's files is stale.
	DefinitionHash string `json:"definitionHash"`
	// The main packages of the module: what a Nix build installs.
	Binaries []Binary `json:"binaries"`
	// Every file the lock run downloaded from the proxy.
	Artifacts []Artifact `json:"artifacts"`
}

// Binary is a main package of the module.
type Binary struct {
	// The command name `go install` gives the package.
	Name string `json:"name"`
	// The import path of the package.
	Package string `json:"package"`
}

// Artifact is one downloaded file.
type Artifact struct {
	// The file's path in a module proxy, which is also its path under
	// $GOMODCACHE/cache/download.
	Path string `json:"path"`
	URL  string `json:"url"`
	// The SHA-256 of the file in SRI form.
	Hash string `json:"hash"`
}

func lockCommand(force bool) error {
	goBin, err := realGo()
	if err != nil {
		return err
	}
	root, err := moduleRoot(goBin)
	if err != nil {
		return err
	}
	lockPath := filepath.Join(root, lockFileName)
	definition, err := definitionHash(root)
	if err != nil {
		return err
	}
	if !force {
		if existing, err := readLock(lockPath); err == nil && existing.Version == lockFormatVersion && existing.DefinitionHash == definition {
			fmt.Fprintf(os.Stderr, "go-purifier: %s is up to date\n", lockPath)
			return nil
		}
	}
	proxy, err := lockProxy(goBin)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "go-purifier: locking %s from %s\n", root, proxy)
	lock, err := makeLock(goBin, root, proxy)
	if err != nil {
		return err
	}
	lock.DefinitionHash = definition
	if err := writeLock(lockPath, lock); err != nil {
		return err
	}
	var names []string
	for _, b := range lock.Binaries {
		names = append(names, b.Name)
	}
	fmt.Fprintf(os.Stderr, "go-purifier: wrote %s with %d locked downloads and the commands [%s]\n", lockPath, len(lock.Artifacts), strings.Join(names, " "))
	return nil
}

// moduleRoot is the directory of the main module's go.mod.
func moduleRoot(goBin string) (string, error) {
	out, err := goOutput(goBin, "", os.Environ(), "env", "GOMOD")
	if err != nil {
		return "", err
	}
	gomod := strings.TrimSpace(out)
	if gomod == "" || gomod == os.DevNull {
		return "", errors.New("the current directory is not inside a Go module")
	}
	return filepath.Dir(gomod), nil
}

// definitionHash is the SHA-256 of go.mod and go.sum, in SRI form. A
// missing go.sum and an empty one hash differently.
func definitionHash(root string) (string, error) {
	h := sha256.New()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
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

// lockProxy is the proxy a lock downloads from: GO_PURIFIER_PROXY, or the
// first proxy URL in GOPROXY, or the default proxy. Every download of the
// lock run goes through this one proxy, so each locked file has a URL.
func lockProxy(goBin string) (string, error) {
	if p := os.Getenv("GO_PURIFIER_PROXY"); p != "" {
		return strings.TrimSuffix(p, "/"), nil
	}
	out, err := goOutput(goBin, "", os.Environ(), "env", "GOPROXY")
	if err != nil {
		return "", err
	}
	for _, entry := range strings.FieldsFunc(strings.TrimSpace(out), func(r rune) bool { return r == ',' || r == '|' }) {
		if strings.HasPrefix(entry, "https://") || strings.HasPrefix(entry, "http://") {
			return strings.TrimSuffix(entry, "/"), nil
		}
	}
	return defaultProxy, nil
}

// lockEnv is the environment of the lock run: an empty GOPATH, module
// cache and build cache under tmp, every module from the one proxy, and
// none of the user's go env file, which could route modules around the
// proxy.
func lockEnv(tmp, proxy string) []string {
	drop := map[string]bool{
		"GOENV": true, "GOPATH": true, "GOMODCACHE": true, "GOCACHE": true, "GOPROXY": true,
		"GOFLAGS": true, "GOTOOLCHAIN": true, "GOWORK": true, "GOPRIVATE": true,
		"GONOPROXY": true, "GONOSUMDB": true, "GOINSECURE": true, "GO_PURIFIER": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			env = append(env, kv)
		}
	}
	return append(env,
		"GOENV=off",
		"GOPATH="+filepath.Join(tmp, "gopath"),
		"GOMODCACHE="+filepath.Join(tmp, "mod"),
		"GOCACHE="+filepath.Join(tmp, "build"),
		"GOPROXY="+proxy,
		"GOFLAGS=-mod=readonly",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
	)
}

// makeLock runs the lock: it downloads what building and testing every
// package of the module needs, then records the downloads.
func makeLock(goBin, root, proxy string) (*Lock, error) {
	tmp, err := os.MkdirTemp("", "go-purifier-lock-")
	if err != nil {
		return nil, err
	}
	env := lockEnv(tmp, proxy)
	defer removeModCache(goBin, root, env, tmp)

	module, err := goOutput(goBin, root, env, "list", "-m")
	if err != nil {
		return nil, err
	}
	goVersion, err := goOutput(goBin, root, env, "env", "GOVERSION")
	if err != nil {
		return nil, err
	}
	// The module graph and the modules go.mod requires.
	if err := goRun(goBin, root, env, "mod", "download"); err != nil {
		return nil, err
	}
	// Every package that building or testing the module's packages loads.
	if _, err := goOutput(goBin, root, env, "list", "-deps", "-test", "./..."); err != nil {
		return nil, err
	}
	mains, err := goOutput(goBin, root, env, "list", "-f", `{{if eq .Name "main"}}{{.ImportPath}}{{end}}`, "./...")
	if err != nil {
		return nil, err
	}
	binaries, err := binariesOf(strings.Fields(mains))
	if err != nil {
		return nil, err
	}
	artifacts, err := collectArtifacts(filepath.Join(tmp, "mod", "cache", "download"), proxy)
	if err != nil {
		return nil, err
	}
	return &Lock{
		Version:   lockFormatVersion,
		Module:    strings.TrimSpace(module),
		GoVersion: strings.TrimSpace(goVersion),
		Proxy:     proxy,
		Binaries:  binaries,
		Artifacts: artifacts,
	}, nil
}

var majorVersionElem = regexp.MustCompile(`^v[0-9]+$`)

// binaryName is the command name `go install` gives a main package: the
// last element of its import path, or the one before it when the last is a
// major version suffix such as v2.
func binaryName(importPath string) string {
	elems := strings.Split(importPath, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && majorVersionElem.MatchString(name) {
		name = elems[len(elems)-2]
	}
	return name
}

func binariesOf(importPaths []string) ([]Binary, error) {
	binaries := []Binary{}
	byName := map[string]string{}
	for _, p := range importPaths {
		name := binaryName(p)
		if other, ok := byName[name]; ok {
			return nil, fmt.Errorf("main packages %s and %s both install as %q", other, p, name)
		}
		byName[name] = p
		binaries = append(binaries, Binary{Name: name, Package: p})
	}
	sort.Slice(binaries, func(i, j int) bool { return binaries[i].Name < binaries[j].Name })
	return binaries, nil
}

// lockable reports whether a file of the module download cache holds
// exactly the bytes the proxy served, and those bytes never change. That
// holds for the .mod and .zip file of a version, which go.sum verifies.
// The go command writes each .info file in the cache itself, so its bytes
// differ from the .info file at the proxy URL. A version list (@v/list)
// changes as versions are published. The .lock, .ziphash and .partial
// files are the cache's bookkeeping.
func lockable(rel string) bool {
	if !strings.Contains(rel, "/@v/") {
		return false
	}
	switch filepath.Ext(rel) {
	case ".mod", ".zip":
		return true
	}
	return false
}

// collectArtifacts lists the lockable files of a module download cache.
// The download cache has the layout of a module proxy, so a file's path in
// it is also its path under the proxy URL. The checksum database's tiles
// (sumdb/) are left out: a build from the lockfile does not consult it.
func collectArtifacts(dir, proxy string) ([]Artifact, error) {
	artifacts := []Artifact{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == dir {
				return filepath.SkipDir
			}
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "sumdb" {
				return filepath.SkipDir
			}
			return nil
		}
		if !lockable(rel) {
			return nil
		}
		hash, err := fileHash(p)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, Artifact{Path: rel, URL: proxy + "/" + rel, Hash: hash})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return sri(h.Sum(nil)), nil
}

func sri(sum []byte) string {
	return "sha256-" + base64.StdEncoding.EncodeToString(sum)
}

// removeModCache deletes the lock run's caches. The go command makes the
// module cache read-only, and `go clean -modcache` is how it removes it.
func removeModCache(goBin, root string, env []string, tmp string) {
	_ = goRun(goBin, root, env, "clean", "-modcache")
	_ = os.RemoveAll(tmp)
}

func readLock(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &lock, nil
}

func writeLock(path string, lock *Lock) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func goRun(goBin, dir string, env []string, args ...string) error {
	cmd := exec.Command(goBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func goOutput(goBin, dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(goBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
