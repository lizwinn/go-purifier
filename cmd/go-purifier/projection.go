// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

// Module identities come from Nix, rather than user-selected release numbers.
// Native module zips contain the rewritten go.mod as well as its proxy copy.
type moduleProjectionRequest struct {
	Identity     string   `json:"identity"`
	Modules      []string `json:"modules"`
	Repositories []string `json:"repositories"`
}
type moduleProjection struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}
type moduleProjectionCatalog struct {
	Format  string             `json:"format"`
	Modules []moduleProjection `json:"modules"`
}

func projectModules(requestPath, out string) error {
	data, err := os.ReadFile(requestPath)
	if err != nil {
		return err
	}
	var request moduleProjectionRequest
	if err = json.Unmarshal(data, &request); err != nil {
		return err
	}
	if request.Identity == "" {
		return fmt.Errorf("module projection requires a Nix identity")
	}
	versions := map[string]string{}
	files := map[string]*modfile.File{}
	dirs := map[string]string{}
	for _, repo := range request.Repositories {
		data, err := os.ReadFile(filepath.Join(repo, ".catalog"))
		if err != nil {
			return err
		}
		var catalog moduleProjectionCatalog
		if err = json.Unmarshal(data, &catalog); err != nil {
			return err
		}
		if catalog.Format != "go" {
			return fmt.Errorf("not a Go repository: %s", repo)
		}
		for _, m := range catalog.Modules {
			if old, ok := versions[m.Path]; ok && old != m.Version {
				return fmt.Errorf("conflicting Nix inputs for %s", m.Path)
			}
			versions[m.Path] = m.Version
		}
	}
	var names []string
	for _, dir := range request.Modules {
		p := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := modfile.Parse(p, data, nil)
		if err != nil {
			return err
		}
		if f.Module == nil {
			return fmt.Errorf("missing module directive: %s", p)
		}
		name := f.Module.Mod.Path
		if _, ok := versions[name]; ok {
			return fmt.Errorf("input shadows produced module %s", name)
		}
		if _, ok := files[name]; ok {
			return fmt.Errorf("duplicate produced module %s", name)
		}
		files[name] = f
		dirs[name] = dir
		names = append(names, name)
	}
	sort.Strings(names)
	// Sorted dependency coordinates bind metadata and its complete supplied graph.
	var coordinates []string
	for path, version := range versions {
		coordinates = append(coordinates, path+"@"+version)
	}
	sort.Strings(coordinates)
	digest := sha256.Sum256([]byte(request.Identity + "\n" + strings.Join(coordinates, "\n")))
	identity := hex.EncodeToString(digest[:])
	for _, name := range names {
		_, suffix, ok := module.SplitPathVersion(name)
		if !ok {
			return fmt.Errorf("invalid module path %s", name)
		}
		major := "v0"
		if strings.HasPrefix(suffix, "/v") {
			major = strings.TrimPrefix(suffix, "/")
		}
		if strings.HasPrefix(suffix, ".v") {
			major = strings.TrimPrefix(suffix, ".")
		}
		version := major + ".0.0-nix.h" + identity
		if err := module.Check(name, version); err != nil {
			return err
		}
		versions[name] = version
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	// Reuse supplied proxy archives byte for byte; never repackage their sources.
	for _, repo := range request.Repositories {
		err := filepath.WalkDir(repo, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return err
			}
			if rel == "." || rel == ".catalog" || strings.HasPrefix(rel, "nix-support") {
				if d.IsDir() && rel != "." {
					return filepath.SkipDir
				}
				return nil
			}
			dest := filepath.Join(out, rel)
			if d.IsDir() {
				return os.MkdirAll(dest, 0755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if old, err := os.ReadFile(dest); err == nil && !bytes.Equal(old, data) {
				return fmt.Errorf("conflicting module proxy file %s", rel)
			}
			return os.WriteFile(dest, data, 0644)
		})
		if err != nil {
			return err
		}
	}
	for _, name := range names {
		f := files[name]
		for _, r := range append([]*modfile.Require{}, f.Require...) {
			if v, ok := versions[r.Mod.Path]; ok {
				if err := f.AddRequire(r.Mod.Path, v); err != nil {
					return err
				}
			}
		}
		for _, r := range append([]*modfile.Replace{}, f.Replace...) {
			if _, ok := versions[r.Old.Path]; ok {
				if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
					return err
				}
			} else if r.New.Version == "" {
				return fmt.Errorf("%s: local replacement %s has no Nix-provided module", name, r.Old.Path)
			}
		}
		f.Cleanup()
		mod, err := f.Format()
		if err != nil {
			return err
		}
		version := versions[name]
		escaped, err := module.EscapePath(name)
		if err != nil {
			return err
		}
		dir := filepath.Join(out, escaped, "@v")
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp("", "go-projection-")
		if err != nil {
			return err
		}
		err = func() error {
			defer os.RemoveAll(tmp)
			original := filepath.Join(tmp, "original.zip")
			archive, err := os.Create(original)
			if err != nil {
				return err
			}
			err = modzip.CreateFromDir(archive, module.Version{Path: name, Version: version}, dirs[name])
			closeErr := archive.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			source := filepath.Join(out, ".sources", escaped)
			if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
				return err
			}
			if err = modzip.Unzip(source, module.Version{Path: name, Version: version}, original); err != nil {
				return err
			}
			if err = os.Chmod(filepath.Join(source, "go.mod"), 0644); err != nil {
				return err
			}
			if err = os.WriteFile(filepath.Join(source, "go.mod"), mod, 0644); err != nil {
				return err
			}
			target, err := os.Create(filepath.Join(dir, version+".zip"))
			if err != nil {
				return err
			}
			err = modzip.CreateFromDir(target, module.Version{Path: name, Version: version}, source)
			closeErr = target.Close()
			if err != nil {
				return err
			}
			return closeErr
		}()
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, version+".mod"), mod, 0644); err != nil {
			return err
		}
		info, _ := json.Marshal(map[string]string{"Version": version, "Time": "1970-01-01T00:00:00Z"})
		if err = os.WriteFile(filepath.Join(dir, version+".info"), info, 0644); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "list"), []byte(version+"\n"), 0644); err != nil {
			return err
		}
	}
	catalog := moduleProjectionCatalog{Format: "go"}
	for path, version := range versions {
		catalog.Modules = append(catalog.Modules, moduleProjection{path, version})
	}
	sort.Slice(catalog.Modules, func(i, j int) bool { return catalog.Modules[i].Path < catalog.Modules[j].Path })
	data, err = json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, ".catalog"), append(data, '\n'), 0644)
}
