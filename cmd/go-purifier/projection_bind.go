// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/mod/module"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// A native workspace replaces all versions of supplied module paths. The
// project's go.mod and go.sum remain untouched; Go still selects other modules.
func bindModuleInputs(repositories []string, root, target string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	work := &modfile.WorkFile{Syntax: &modfile.FileSyntax{}}
	if data, err := os.ReadFile(filepath.Join(root, "go.work")); err == nil {
		work, err = modfile.ParseWork("go.work", data, nil)
		if err != nil {
			return err
		}
		uses := make([]modfile.Use, len(work.Use))
		for i, use := range work.Use {
			uses[i] = *use
			if err := work.DropUse(use.Path); err != nil {
				return err
			}
			if !filepath.IsAbs(uses[i].Path) {
				uses[i].Path = filepath.Join(root, uses[i].Path)
			}
		}
		work.Cleanup()
		for _, use := range uses {
			if err := work.AddUse(use.Path, use.ModulePath); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		if err := work.AddUse(root, ""); err != nil {
			return err
		}
	}
	goVersion := "1.24"
	if work.Go != nil {
		goVersion = work.Go.Version
	}
	for _, use := range work.Use {
		data, err := os.ReadFile(filepath.Join(use.Path, "go.mod"))
		if err != nil {
			return err
		}
		f, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return err
		}
		if f.Go != nil && semver.Compare("v"+f.Go.Version, "v"+goVersion) > 0 {
			goVersion = f.Go.Version
		}
	}
	if err := work.AddGoStmt(goVersion); err != nil {
		return err
	}
	known := map[string]string{}
	for _, repository := range repositories {
		data, err := os.ReadFile(filepath.Join(repository, ".catalog"))
		if err != nil {
			return err
		}
		var catalog moduleProjectionCatalog
		if err = json.Unmarshal(data, &catalog); err != nil {
			return err
		}
		if catalog.Format != "go" {
			return fmt.Errorf("not a Go module projection: %s", repository)
		}
		for _, m := range catalog.Modules {
			if old, ok := known[m.Path]; ok && old != m.Version {
				return fmt.Errorf("conflicting Nix inputs for %s", m.Path)
			}
			data, err := os.ReadFile(filepath.Join(moduleInputSource(repository, m), "go.mod"))
			if err != nil {
				return err
			}
			dependency, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				return err
			}
			if dependency.Go != nil && semver.Compare("v"+dependency.Go.Version, "v"+goVersion) > 0 {
				goVersion = dependency.Go.Version
				if err := work.AddGoStmt(goVersion); err != nil {
					return err
				}
			}
			known[m.Path] = m.Version
			// Remove version-specific user replacements, which would otherwise win.
			for _, r := range append([]*modfile.Replace{}, work.Replace...) {
				if r.Old.Path == m.Path {
					if err := work.DropReplace(r.Old.Path, r.Old.Version); err != nil {
						return err
					}
				}
			}
			if err := work.AddReplace(m.Path, "", moduleInputSource(repository, m), ""); err != nil {
				return err
			}
		}
	}
	work.Cleanup()
	return os.WriteFile(target, modfile.Format(work.Syntax), 0644)
}

// Supplied module archives are identified by Nix and never enter the download lock.
func omitModuleInputs(files map[string]string, repositories []string) error {
	for _, repository := range repositories {
		data, err := os.ReadFile(filepath.Join(repository, ".catalog"))
		if err != nil {
			return err
		}
		var catalog moduleProjectionCatalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			return err
		}
		for _, m := range catalog.Modules {
			escaped, err := module.EscapePath(m.Path)
			if err != nil {
				return err
			}
			for path := range files {
				if strings.HasPrefix(path, escaped+"/@v/") {
					delete(files, path)
				}
			}
		}
	}
	return nil
}
func moduleInputProxy(repositories []string, fallback string) string {
	var proxies []string
	for _, repo := range repositories {
		proxies = append(proxies, "file://"+filepath.ToSlash(repo))
	}
	if fallback != "" {
		proxies = append(proxies, fallback)
	}
	return strings.Join(proxies, ",")
}

func moduleInputPatterns(repositories []string) (string, error) {
	var patterns []string
	for _, repo := range repositories {
		data, err := os.ReadFile(filepath.Join(repo, ".catalog"))
		if err != nil {
			return "", err
		}
		var catalog moduleProjectionCatalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			return "", err
		}
		for _, m := range catalog.Modules {
			patterns = append(patterns, m.Path)
		}
	}
	return strings.Join(patterns, ","), nil
}

func moduleInputSource(repository string, m moduleProjection) string {
	escaped, _ := module.EscapePath(m.Path)
	return filepath.Join(repository, ".sources", escaped)
}
func moduleInputNames(repositories []string) (map[string]bool, error) {
	names := map[string]bool{}
	for _, repository := range repositories {
		data, err := os.ReadFile(filepath.Join(repository, ".catalog"))
		if err != nil {
			return nil, err
		}
		var catalog moduleProjectionCatalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			return nil, err
		}
		for _, m := range catalog.Modules {
			names[m.Path] = true
		}
	}
	return names, nil
}
