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

	"golang.org/x/mod/module"
)

// The lockfile sits at the lock root (root.go).
const lockFileName = "go-purifier.lock.json"

// The lock format version this binary writes and reads. nix/lib/lock-version.nix
// holds the same number.
const lockFormatVersion = 1

const defaultProxy = "https://proxy.golang.org"

// Lock is the content of go-purifier.lock.json.
type Lock struct {
	Version int `json:"version"`
	// The paths of the main modules: the module at the lock root, or the
	// modules of the workspace.
	Modules []string `json:"modules"`
	// The go version the lock was made with, such as go1.26.10.
	GoVersion string `json:"goVersion"`
	// The module proxy every file entry was downloaded from.
	Proxy string `json:"proxy"`
	// The GONOPROXY patterns of the lock run: the modules it fetched from
	// version control.
	NoProxy string `json:"noProxy"`
	// The files, relative to the lock root, that define the dependencies,
	// and their digest. A lockfile whose digest differs from the files is
	// stale.
	DefinitionFiles []string `json:"definitionFiles"`
	DefinitionHash  string   `json:"definitionHash"`
	// The main packages of the main modules: what a Nix build installs.
	Binaries []Binary `json:"binaries"`
	// Modules that a replace directive points at a local directory.
	LocalModules []LocalModule `json:"localModules"`
	// Module versions fetched from version control.
	VCSModules []VCSModule `json:"vcsModules"`
	// Every download: proxy files, and checkouts of git repositories.
	Artifacts []Artifact `json:"artifacts"`
}

// Binary is a main package of a main module.
type Binary struct {
	// The command name `go install` gives the package.
	Name string `json:"name"`
	// The import path of the package.
	Package string `json:"package"`
}

// LocalModule is a module that a replace directive points at a directory.
type LocalModule struct {
	Module string `json:"module"`
	// The directory, relative to the lock root.
	Path string `json:"path"`
}

// Artifact is one download, in the entry shapes the nix-purifiers share:
// a file entry has URL and Mirrors, and a git entry has Git. Path is its
// place in the output tree. For a file entry that tree is a module proxy,
// and Path is the file's proxy path, which is also its path under
// $GOMODCACHE/cache/download.
//
// Mirrors is always empty. Two module proxies serve zips with the same
// content, which go.sum verifies, but not necessarily the same bytes, so
// the other proxies in GOPROXY are not mirrors of the lock proxy.
type Artifact struct {
	Path    string     `json:"path"`
	URL     string     `json:"url,omitempty"`
	Mirrors *[]string  `json:"mirrors,omitempty"`
	Git     *GitSource `json:"git,omitempty"`
	// The SHA-256 of a file, or the NAR hash of a checkout, in SRI form.
	Hash string `json:"hash"`
}

// GitSource is the repository and revision of a git entry.
type GitSource struct {
	URL        string `json:"url"`
	Rev        string `json:"rev"`
	Submodules bool   `json:"submodules"`
}

// goEnv is the go command's view of the user's environment and go env
// file, read before the lock run replaces both.
type goEnv struct {
	GOWORK    string
	GOMOD     string
	GOPROXY   string
	GONOPROXY string
}

func readGoEnv(goBin, dir string) (goEnv, error) {
	out, err := goOutput(goBin, dir, os.Environ(), "env", "-json", "GOWORK", "GOMOD", "GOPROXY", "GONOPROXY")
	if err != nil {
		return goEnv{}, err
	}
	var env goEnv
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		return goEnv{}, fmt.Errorf("reading go env: %w", err)
	}
	return env, nil
}

// lockRoot is the lock root of the go command's environment, and whether
// it is a workspace.
func (e goEnv) lockRoot() (string, bool, error) {
	if e.GOWORK != "" && e.GOWORK != "off" {
		return filepath.Dir(e.GOWORK), true, nil
	}
	if e.GOMOD == "" || e.GOMOD == os.DevNull {
		return "", false, errors.New("the current directory is not inside a Go module or workspace")
	}
	return filepath.Dir(e.GOMOD), false, nil
}

func lockCommand(force bool) error {
	goBin, err := realGo()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	env, err := readGoEnv(goBin, cwd)
	if err != nil {
		return err
	}
	root, workspace, err := env.lockRoot()
	if err != nil {
		return err
	}
	lockPath := filepath.Join(root, lockFileName)
	existing, _ := readLock(lockPath)
	if !force && existing != nil && existing.Version == lockFormatVersion {
		if current, err := definitionHash(root, existing.DefinitionFiles); err == nil && current == existing.DefinitionHash {
			fmt.Fprintf(os.Stderr, "go-purifier: %s is up to date\n", lockPath)
			return nil
		}
	}
	proxy := lockProxy(env.GOPROXY)
	// A relock without GONOPROXY or GOPRIVATE in the environment keeps the
	// patterns the lockfile was made with.
	noProxy := env.GONOPROXY
	if noProxy == "" && existing != nil {
		noProxy = existing.NoProxy
	}
	fmt.Fprintf(os.Stderr, "go-purifier: locking %s from %s\n", root, proxy)
	lock, err := makeLock(goBin, root, workspace, proxy, noProxy, prefetchGit)
	if err != nil {
		return err
	}
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

// lockProxy is the proxy a lock downloads from: GO_PURIFIER_PROXY, or the
// first proxy URL in GOPROXY, or the default proxy. Every download of the
// lock run outside GONOPROXY goes through this one proxy, so each locked
// file has a URL.
func lockProxy(goproxy string) string {
	if p := os.Getenv("GO_PURIFIER_PROXY"); p != "" {
		return strings.TrimSuffix(p, "/")
	}
	for _, entry := range strings.FieldsFunc(goproxy, func(r rune) bool { return r == ',' || r == '|' }) {
		if strings.HasPrefix(entry, "https://") || strings.HasPrefix(entry, "http://") {
			return strings.TrimSuffix(entry, "/")
		}
	}
	return defaultProxy
}

// lockEnv is the environment of the lock run: an empty GOPATH, module
// cache and build cache under tmp, every module outside noProxy from the
// one proxy, and none of the user's go env file. The modules noProxy
// matches come from version control and skip the checksum database;
// go.sum still verifies them.
func lockEnv(tmp, proxy, noProxy string) []string {
	drop := map[string]bool{
		"GOENV": true, "GOPATH": true, "GOMODCACHE": true, "GOCACHE": true, "GOPROXY": true,
		"GOFLAGS": true, "GOTOOLCHAIN": true, "GOPRIVATE": true, "GONOPROXY": true,
		"GONOSUMDB": true, "GOINSECURE": true, "GO_PURIFIER": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			env = append(env, kv)
		}
	}
	env = append(env,
		"GOENV=off",
		"GOPATH="+filepath.Join(tmp, "gopath"),
		"GOMODCACHE="+filepath.Join(tmp, "mod"),
		"GOCACHE="+filepath.Join(tmp, "build"),
		"GOPROXY="+proxy,
		"GOFLAGS=-mod=readonly",
		"GOTOOLCHAIN=local",
	)
	if noProxy != "" {
		env = append(env, "GONOPROXY="+noProxy, "GONOSUMDB="+noProxy)
	}
	return env
}

// listedModule is an entry of `go list -m -json all`.
type listedModule struct {
	Path    string
	Main    bool
	Dir     string
	Replace *struct {
		Path    string
		Version string
		Dir     string
	}
}

func decodeModules(out string) ([]listedModule, error) {
	var mods []listedModule
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var m listedModule
		if err := dec.Decode(&m); err == io.EOF {
			return mods, nil
		} else if err != nil {
			return nil, fmt.Errorf("reading go list -m: %w", err)
		}
		mods = append(mods, m)
	}
}

// makeLock runs the lock: it downloads what building and testing every
// package of the main modules needs, then records the downloads.
func makeLock(goBin, root string, workspace bool, proxy, noProxy string, prefetch func(url, rev string) (string, error)) (*Lock, error) {
	tmp, err := os.MkdirTemp("", "go-purifier-lock-")
	if err != nil {
		return nil, err
	}
	env := lockEnv(tmp, proxy, noProxy)
	defer removeModCache(goBin, root, env, tmp)

	goVersion, err := goOutput(goBin, root, env, "env", "GOVERSION")
	if err != nil {
		return nil, err
	}
	// The module graph, which downloads the go.mod file of every module in
	// it.
	listed, err := goOutput(goBin, root, env, "list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	mods, err := decodeModules(listed)
	if err != nil {
		return nil, err
	}
	// Every package of the main modules and every package they and their
	// tests import, which downloads the module of each.
	mains, err := goOutput(goBin, root, env, "list", "-f", `{{if and (eq .Name "main") .Module .Module.Main}}{{.ImportPath}}{{end}}`, "all")
	if err != nil {
		return nil, err
	}
	binaries, err := binariesOf(strings.Fields(mains))
	if err != nil {
		return nil, err
	}

	lock := &Lock{
		Version:      lockFormatVersion,
		Modules:      []string{},
		GoVersion:    strings.TrimSpace(goVersion),
		Proxy:        proxy,
		NoProxy:      noProxy,
		Binaries:     binaries,
		LocalModules: []LocalModule{},
	}
	var dirs []string
	for _, m := range mods {
		switch {
		case m.Main:
			lock.Modules = append(lock.Modules, m.Path)
			dirs = append(dirs, m.Dir)
		case m.Replace != nil && m.Replace.Version == "":
			rel, err := filepath.Rel(root, m.Replace.Dir)
			if err != nil {
				return nil, err
			}
			lock.LocalModules = append(lock.LocalModules, LocalModule{Module: m.Path, Path: filepath.ToSlash(rel)})
			dirs = append(dirs, m.Replace.Dir)
		}
	}
	sort.Strings(lock.Modules)
	if lock.DefinitionFiles, err = definitionFiles(root, workspace, dirs); err != nil {
		return nil, err
	}
	if lock.DefinitionHash, err = definitionHash(root, lock.DefinitionFiles); err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(tmp, "mod", "cache", "download")
	files, err := collectArtifacts(cacheDir, proxy, noProxy)
	if err != nil {
		return nil, err
	}
	vcs, checkouts, err := vcsModules(cacheDir, noProxy, prefetch)
	if err != nil {
		return nil, err
	}
	lock.VCSModules = vcs
	lock.Artifacts = append(checkouts, files...)
	if lock.Artifacts == nil {
		lock.Artifacts = []Artifact{}
	}
	return lock, nil
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

// collectArtifacts lists the lockable files of a module download cache
// that came from the proxy: those of modules noProxy does not match. The
// download cache has the layout of a module proxy, so a file's path in it
// is also its path under the proxy URL. The checksum database's tiles
// (sumdb/) are left out: a build from the lockfile does not consult it.
func collectArtifacts(dir, proxy, noProxy string) ([]Artifact, error) {
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
		if noProxy != "" {
			escaped, _, _ := splitProxyPath(rel)
			modPath, err := module.UnescapePath(escaped)
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			if module.MatchPrefixPatterns(noProxy, modPath) {
				return nil
			}
		}
		hash, err := fileHash(p)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, Artifact{Path: rel, URL: proxy + "/" + rel, Mirrors: &[]string{}, Hash: hash})
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
