// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

// A module that GONOPROXY (or GOPRIVATE) names is fetched by the go
// command from its version control repository rather than from the proxy.
// The lock records the repository checkout as a git entry, and the Nix
// build makes the module's .mod and .zip files from the checkout with
// `go-purifier module-files`, the same way the go command makes them.
// go.sum then verifies both.

// VCSModule is a module version the lock run fetched from version control.
type VCSModule struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	// The proxy directory of the module's versions, such as
	// github.com/!burnt!sushi/toml/@v.
	ProxyDir string `json:"proxyDir"`
	// The commit time, for the .info file.
	Time string `json:"time"`
	// The path of the git entry that holds the checkout.
	Entry string `json:"entry"`
	// The module's directory in the repository, or "".
	Subdir string `json:"subdir"`
}

// origin is the part of a cached .info file that records where the go
// command fetched a module from.
type origin struct {
	Version string
	Time    string
	Origin  *struct {
		VCS    string
		URL    string
		Subdir string
		Hash   string
	}
}

// splitProxyPath splits a proxy path such as
// github.com/!burnt!sushi/toml/@v/v1.4.0.zip into the escaped module path
// and the file name.
func splitProxyPath(rel string) (escapedModule, file string, ok bool) {
	i := strings.LastIndex(rel, "/@v/")
	if i < 0 {
		return "", "", false
	}
	return rel[:i], rel[i+len("/@v/"):], true
}

// vcsModules finds the module versions in a module download cache that
// the go command fetched from version control, which are the ones
// noProxy matches, and records each with a git entry for its checkout.
// Modules of one repository revision share an entry.
func vcsModules(cacheDir, noProxy string, prefetch func(url, rev string) (string, error)) ([]VCSModule, []Artifact, error) {
	if noProxy == "" {
		return []VCSModule{}, nil, nil
	}
	mods := []VCSModule{}
	entries := map[string]Artifact{}
	var order []string
	err := filepath.WalkDir(cacheDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == cacheDir {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".info" {
			return nil
		}
		rel, err := filepath.Rel(cacheDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		escaped, file, ok := splitProxyPath(rel)
		if !ok {
			return nil
		}
		modPath, err := module.UnescapePath(escaped)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if !module.MatchPrefixPatterns(noProxy, modPath) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var info origin
		if err := json.Unmarshal(data, &info); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		version := strings.TrimSuffix(file, ".info")
		if info.Origin == nil || info.Origin.URL == "" || info.Origin.Hash == "" {
			return fmt.Errorf("%s@%s: the go command recorded no origin for this module", modPath, version)
		}
		if info.Origin.VCS != "git" {
			return fmt.Errorf("%s@%s comes from a %s repository; go-purifier locks modules from git repositories and module proxies", modPath, version, info.Origin.VCS)
		}
		url := httpsURL(info.Origin.URL)
		urlSum := sha256.Sum256([]byte(url))
		entryPath := "vcs/" + hex.EncodeToString(urlSum[:6]) + "-" + info.Origin.Hash
		if _, ok := entries[entryPath]; !ok {
			hash, err := prefetch(url, info.Origin.Hash)
			if err != nil {
				return err
			}
			entries[entryPath] = Artifact{
				Path: entryPath,
				Git:  &GitSource{URL: url, Rev: info.Origin.Hash, Submodules: false},
				Hash: hash,
			}
			order = append(order, entryPath)
		}
		mods = append(mods, VCSModule{
			Module:   modPath,
			Version:  version,
			ProxyDir: escaped + "/@v",
			Time:     info.Time,
			Entry:    entryPath,
			Subdir:   info.Origin.Subdir,
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var arts []Artifact
	for _, p := range order {
		arts = append(arts, entries[p])
	}
	return mods, arts, nil
}

// httpsURL is the HTTPS URL of the repository at an SSH URL, such as
// https://github.com/org/repo for git@github.com:org/repo.git or
// ssh://git@github.com/org/repo. The Nix sandbox has no SSH keys, so the
// nix-purifiers record a git entry under the HTTPS URL of the same host;
// a private repository then takes a fetchgit with credentials. Other URLs
// stay as they are.
func httpsURL(url string) string {
	var host, path string
	switch {
	case strings.HasPrefix(url, "ssh://"):
		rest := strings.TrimPrefix(url, "ssh://")
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		var ok bool
		if host, path, ok = strings.Cut(rest, "/"); !ok {
			return url
		}
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
	case !strings.Contains(url, "://") && strings.Contains(url, "@") && strings.Contains(url, ":"):
		rest := url[strings.Index(url, "@")+1:]
		host, path, _ = strings.Cut(rest, ":")
	default:
		return url
	}
	return "https://" + host + "/" + strings.TrimPrefix(path, "/")
}

// prefetchGit is the NAR hash of a git checkout without submodules, as
// nixpkgs' fetchgit and builtins.fetchGit produce it, from
// nix-prefetch-git.
func prefetchGit(url, rev string) (string, error) {
	cmd := exec.Command("nix-prefetch-git", "--url", url, "--rev", rev, "--quiet")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("nix-prefetch-git %s %s: %w", url, rev, err)
	}
	var out struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil || !strings.HasPrefix(out.Hash, "sha256-") {
		return "", fmt.Errorf("nix-prefetch-git %s %s printed no SRI hash", url, rev)
	}
	return out.Hash, nil
}

// moduleFilesCommand writes the .mod, .zip and .info file of a module
// version from a checkout of its repository into the directory $out. It
// is the builder of the Nix derivation for each locked VCS module:
//
//	go-purifier module-files <checkout> <subdir> <module> <version> <time>
func moduleFilesCommand(args []string) error {
	if len(args) != 5 {
		return errors.New("usage: go-purifier module-files <checkout> <subdir> <module> <version> <time>")
	}
	checkout, subdir, modPath, version, modTime := args[0], args[1], args[2], args[3], args[4]
	out := os.Getenv("out")
	if out == "" {
		return errors.New("module-files writes into $out, which is not set")
	}
	return writeModuleFiles(checkout, subdir, modPath, version, modTime, out)
}

// writeModuleFiles writes the files of the module in directory subdir of
// a checkout. The zip holds what the go command's zip of the module holds
// (cmd/go/internal/modfetch/coderepo.go): every file under the module
// directory, filtered by the rules of golang.org/x/mod/zip, and the
// LICENSE file of the repository root when the module is in a
// subdirectory without one of its own.
func writeModuleFiles(checkout, subdir, modPath, version, modTime, out string) error {
	dir := filepath.Join(checkout, filepath.FromSlash(subdir))
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	// The go command serves a module without a go.mod file at the version
	// with a go.mod that holds only the module statement.
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if errors.Is(err, fs.ErrNotExist) {
		gomod = []byte("module " + modfileQuote(modPath) + "\n")
	} else if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, version+".mod"), gomod, 0o444); err != nil {
		return err
	}
	info, err := json.Marshal(struct{ Version, Time string }{version, modTime})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, version+".info"), info, 0o444); err != nil {
		return err
	}
	var files []modzip.File
	haveLicense := false
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "LICENSE" {
			haveLicense = true
		}
		files = append(files, checkoutFile{name: rel, path: p})
		return nil
	})
	if err != nil {
		return err
	}
	if !haveLicense && strings.Trim(subdir, "/") != "" {
		root := filepath.Join(checkout, "LICENSE")
		if info, err := os.Lstat(root); err == nil && info.Mode().IsRegular() {
			files = append(files, checkoutFile{name: "LICENSE", path: root})
		}
	}
	f, err := os.Create(filepath.Join(out, version+".zip"))
	if err != nil {
		return err
	}
	if err := modzip.Create(f, module.Version{Path: modPath, Version: version}, files); err != nil {
		f.Close()
		return fmt.Errorf("zipping %s@%s: %w", modPath, version, err)
	}
	return f.Close()
}

// checkoutFile is a file of a checkout, under its name in the module.
type checkoutFile struct {
	name, path string
}

func (f checkoutFile) Path() string                 { return f.name }
func (f checkoutFile) Lstat() (fs.FileInfo, error)  { return os.Lstat(f.path) }
func (f checkoutFile) Open() (io.ReadCloser, error) { return os.Open(f.path) }

// modfileQuote quotes a module path in a go.mod statement when it needs
// quoting.
func modfileQuote(s string) string {
	if strings.ContainsAny(s, " \t\"'`") {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return s
}
