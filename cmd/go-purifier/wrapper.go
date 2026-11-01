// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// realGo is the go command this binary runs: GO_PURIFIER_GO, or the first
// go on PATH that is not this binary.
func realGo() (string, error) {
	if g := os.Getenv("GO_PURIFIER_GO"); g != "" {
		return g, nil
	}
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate := filepath.Join(dir, "go")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil && resolved == self {
			continue
		}
		return candidate, nil
	}
	return "", errors.New("no go command found; set GO_PURIFIER_GO")
}

// runGo runs the real go command, with GOPROXY pointing at the locked
// downloads when the module has a current lockfile.
func runGo(args []string) int {
	goBin, err := realGo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-purifier: %v\n", err)
		return 1
	}
	env := os.Environ()
	if os.Getenv("GO_PURIFIER") != "0" {
		dir, err := workingDir(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-purifier: %v\n", err)
			return 1
		}
		extra, err := interactiveEnv(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-purifier: %v\n", err)
			return 1
		}
		env = append(env, extra...)
	}
	if err := syscall.Exec(goBin, append([]string{"go"}, args...), env); err != nil {
		fmt.Fprintf(os.Stderr, "go-purifier: running %s: %v\n", goBin, err)
		return 1
	}
	return 0
}

// workingDir is the directory the go command runs in: the directory of a
// leading -C flag, which the go command accepts only as the first flag, or
// the current directory.
func workingDir(args []string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	var dir string
	switch {
	case len(args) >= 2 && args[0] == "-C":
		dir = args[1]
	case len(args) >= 1 && strings.HasPrefix(args[0], "-C="):
		dir = strings.TrimPrefix(args[0], "-C=")
	default:
		return cwd, nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	return dir, nil
}

// findModuleRoot is the nearest directory at or above dir that holds a
// go.mod.
func findModuleRoot(dir string) (string, bool) {
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// interactiveEnv is what the go command's environment gains when it runs
// in dir, inside a module with a current lockfile: the locked downloads as
// its only proxy. A module without a lockfile, or with a stale one,
// resolves the usual way.
func interactiveEnv(dir string) ([]string, error) {
	root, ok := findModuleRoot(dir)
	if !ok {
		return nil, nil
	}
	lockPath := filepath.Join(root, lockFileName)
	lock, err := readLock(lockPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lock.Version != lockFormatVersion {
		fmt.Fprintf(os.Stderr, "go-purifier: %s has lock format version %d and this go-purifier reads version %d; resolving the usual way until `go-purifier relock` renews it\n", lockPath, lock.Version, lockFormatVersion)
		return nil, nil
	}
	definition, err := definitionHash(root)
	if err != nil {
		return nil, err
	}
	if lock.DefinitionHash != definition {
		fmt.Fprintf(os.Stderr, "go-purifier: go.mod or go.sum changed since %s was written; resolving the usual way until `go-purifier lock` renews it\n", lockPath)
		return nil, nil
	}
	proxyDir, err := realize(root, lockPath)
	if err != nil {
		return nil, err
	}
	return []string{
		"GOPROXY=file://" + proxyDir,
		// The lockfile pins every file by hash, and go.sum still holds the
		// module checksums; the checksum database is not reachable offline.
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
	}, nil
}

// realize makes the locked downloads available under a per-module
// directory of the user cache directory and returns the proxy tree there.
// Nix fetches each locked file into the store; a manifest in the store,
// held by a GC root next to the tree, maps each proxy path to its store
// path; and the tree is a directory of links into the store. A tree made
// from the same lockfile content is reused.
func realize(root, lockPath string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	rootSum := sha256.Sum256([]byte(root))
	dir := filepath.Join(cache, "go-purifier", hex.EncodeToString(rootSum[:8]))
	proxyDir := filepath.Join(dir, "proxy")
	stampPath := filepath.Join(dir, "stamp")

	content, err := os.ReadFile(lockPath)
	if err != nil {
		return "", err
	}
	contentSum := sha256.Sum256(content)
	stamp := hex.EncodeToString(contentSum[:])
	if old, err := os.ReadFile(stampPath); err == nil && string(old) == stamp {
		if _, err := os.Stat(proxyDir); err == nil {
			return proxyDir, nil
		}
	}

	nixLib := os.Getenv("GO_PURIFIER_NIX_LIB")
	if nixLib == "" {
		return "", errors.New("GO_PURIFIER_NIX_LIB is not set; run go through the go-purifier flake (`nix develop` or `nix shell` of its `go` package), or set GO_PURIFIER=0")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	expr := filepath.Join(nixLib, "realize.nix")
	args := []string{expr, "--argstr", "lockFile", lockPath}

	fmt.Fprintf(os.Stderr, "go-purifier: realizing %s through Nix\n", lockPath)
	// nix-build of an attribute set builds every derivation in it, so the
	// downloads run in parallel.
	if err := run("nix-build", append([]string{"--no-out-link", "--quiet", "-A", "fetches"}, args...)...); err != nil {
		return "", err
	}
	manifestJSON, err := output("nix-instantiate", append([]string{"--eval", "--strict", "--json", "--read-write-mode", "-A", "manifest"}, args...)...)
	if err != nil {
		return "", err
	}
	var manifestPath string
	if err := json.Unmarshal([]byte(manifestJSON), &manifestPath); err != nil {
		return "", fmt.Errorf("reading the manifest path from nix-instantiate: %w", err)
	}
	if _, err := output("nix-store", "--add-root", filepath.Join(dir, "gcroot"), "--realise", manifestPath); err != nil {
		return "", err
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", err
	}
	var manifest map[string]string
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", fmt.Errorf("%s: %w", manifestPath, err)
	}

	tmpDir, err := os.MkdirTemp(dir, "proxy-")
	if err != nil {
		return "", err
	}
	for rel, target := range manifest {
		link := filepath.Join(tmpDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return "", err
		}
		if err := os.Symlink(target, link); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(proxyDir); err != nil {
		return "", err
	}
	if err := os.Rename(tmpDir, proxyDir); err != nil {
		return "", err
	}
	if err := os.WriteFile(stampPath, []byte(stamp), 0o644); err != nil {
		return "", err
	}
	return proxyDir, nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
