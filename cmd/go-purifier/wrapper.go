// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
// downloads when the build has a current lockfile.
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

// noProxyMatchesNothing is a GONOPROXY pattern that matches no module. It
// keeps GOPRIVATE, which GONOPROXY defaults to, from sending the go command
// past the locked downloads to version control.
const noProxyMatchesNothing = "go-purifier.invalid"

// interactiveEnv is what the go command's environment gains when it runs
// in dir, inside a build with a current lockfile: the locked downloads as
// its only proxy. A build without a lockfile, or with a stale one,
// resolves the usual way.
func interactiveEnv(dir string) ([]string, error) {
	root, ok := findLockRoot(dir)
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
	definition, err := definitionHash(root, lock.DefinitionFiles)
	if err != nil {
		return nil, err
	}
	if lock.DefinitionHash != definition {
		fmt.Fprintf(os.Stderr, "go-purifier: the module definitions changed since %s was written; resolving the usual way until `go-purifier lock` renews it\n", lockPath)
		return nil, nil
	}
	proxyDir, err := realize(root, lockPath)
	if err != nil {
		return nil, err
	}
	return []string{
		"GOPROXY=file://" + proxyDir,
		"GONOPROXY=" + noProxyMatchesNothing,
		// The lockfile pins every download by hash, and go.sum still holds
		// the module checksums; the checksum database is not reachable
		// offline.
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
	}, nil
}

// realize builds the proxy tree of a lockfile through Nix and returns the
// path of a GC root that links to it, under a per-build directory of the
// user cache directory. Nix fetches every locked download in parallel,
// makes the files of modules from version control, and links them into
// one tree in the store. A tree built from the same lockfile content is
// reused.
func realize(root, lockPath string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	rootSum := sha256.Sum256([]byte(root))
	dir := filepath.Join(cache, "go-purifier", hex.EncodeToString(rootSum[:8]))
	proxyLink := filepath.Join(dir, "proxy")
	stampPath := filepath.Join(dir, "stamp")

	content, err := os.ReadFile(lockPath)
	if err != nil {
		return "", err
	}
	contentSum := sha256.Sum256(content)
	stamp := hex.EncodeToString(contentSum[:])
	if old, err := os.ReadFile(stampPath); err == nil && string(old) == stamp {
		if _, err := os.Stat(proxyLink); err == nil {
			return proxyLink, nil
		}
	}

	nixLib := os.Getenv("GO_PURIFIER_NIX_LIB")
	if nixLib == "" {
		return "", errors.New("GO_PURIFIER_NIX_LIB is not set; run go through the go-purifier flake (`nix develop` or `nix shell` of its `toolchain` package), or set GO_PURIFIER=0")
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "go-purifier: realizing %s through Nix\n", lockPath)
	args := []string{filepath.Join(nixLib, "realize.nix"),
		"--argstr", "lockFile", lockPath,
		"--argstr", "cli", self,
		"--out-link", proxyLink, "--quiet"}
	if nixpkgs := os.Getenv("GO_PURIFIER_NIXPKGS"); nixpkgs != "" {
		args = append(args, "--argstr", "nixpkgs", nixpkgs)
	}
	if err := run("nix-build", args...); err != nil {
		return "", err
	}
	if err := os.WriteFile(stampPath, []byte(stamp), 0o644); err != nil {
		return "", err
	}
	return proxyLink, nil
}

// linkTreeCommand is the builder of the proxy tree that interactive mode
// realizes. The file $manifestPath holds a line of a tree path, a tab, and
// a store path per entry; the builder makes $out a directory with a link
// to each store path at its tree path.
func linkTreeCommand() error {
	out, manifestPath := os.Getenv("out"), os.Getenv("manifestPath")
	if out == "" || manifestPath == "" {
		return errors.New("link-tree builds $out from $manifestPath, and one of them is not set")
	}
	f, err := os.Open(manifestPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		rel, target, ok := strings.Cut(scanner.Text(), "\t")
		if !ok {
			continue
		}
		link := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	return scanner.Err()
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

func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
