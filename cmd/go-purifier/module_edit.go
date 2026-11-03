// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// Go mod commands ignore workspace replacements. Give those commands an
// alternate module file, and merge their native edits without retaining our
// generated replacements or checksums for Nix-provided modules.
func moduleEdit(root string, repositories []string) (string, func(bool) error, error) {
	path := filepath.Join(root, "go.mod")
	original, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	f, err := modfile.Parse(path, original, nil)
	if err != nil {
		return "", nil, err
	}
	known := map[string]string{}
	for _, repo := range repositories {
		data, err := os.ReadFile(filepath.Join(repo, ".catalog"))
		if err != nil {
			return "", nil, err
		}
		var catalog moduleProjectionCatalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			return "", nil, err
		}
		for _, m := range catalog.Modules {
			known[m.Path] = moduleInputSource(repo, m)
		}
	}
	requirements := map[string]modfile.Require{}
	for _, r := range f.Require {
		if _, ok := known[r.Mod.Path]; ok {
			requirements[r.Mod.Path] = *r
		}
	}
	var replaces []modfile.Replace
	for _, r := range f.Replace {
		if _, ok := known[r.Old.Path]; ok {
			replaces = append(replaces, *r)
		}
	}
	for name, version := range known {
		for _, r := range append([]*modfile.Replace{}, f.Replace...) {
			if r.Old.Path == name {
				if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
					return "", nil, err
				}
			}
		}
		if err := f.AddReplace(name, "", version, ""); err != nil {
			return "", nil, err
		}
	}
	f.Cleanup()
	data, err := f.Format()
	if err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp(root, ".go-purifier-*.mod")
	if err != nil {
		return "", nil, err
	}
	alternate := file.Name()
	sum := strings.TrimSuffix(alternate, ".mod") + ".sum"
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(alternate)
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		os.Remove(alternate)
		return "", nil, err
	}
	if data, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		if err := os.WriteFile(sum, data, 0644); err != nil {
			os.Remove(alternate)
			return "", nil, err
		}
	}
	cleanup := func(commit bool) error {
		defer os.Remove(alternate)
		defer os.Remove(sum)
		if !commit {
			return nil
		}
		data, err := os.ReadFile(alternate)
		if err != nil {
			return err
		}
		changed, err := modfile.Parse(alternate, data, nil)
		if err != nil {
			return err
		}
		for _, r := range append([]*modfile.Replace{}, changed.Replace...) {
			if _, ok := known[r.Old.Path]; ok {
				if err := changed.DropReplace(r.Old.Path, r.Old.Version); err != nil {
					return err
				}
			}
		}
		for _, r := range replaces {
			if err := changed.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version); err != nil {
				return err
			}
		}
		for _, r := range changed.Require {
			if original, ok := requirements[r.Mod.Path]; ok {
				r.Mod.Version = original.Mod.Version
				if err := changed.AddRequire(r.Mod.Path, original.Mod.Version); err != nil {
					return err
				}
			}
		}
		changed.Cleanup()
		data, err = changed.Format()
		if err != nil {
			return err
		}
		if err = os.WriteFile(path, data, 0644); err != nil {
			return err
		}
		data, err = os.ReadFile(sum)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var lines []string
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				if _, ok := known[fields[0]]; !ok {
					lines = append(lines, line)
				}
			}
		}
		if len(lines) == 0 {
			err := os.Remove(filepath.Join(root, "go.sum"))
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return os.WriteFile(filepath.Join(root, "go.sum"), []byte(strings.Join(lines, "\n")+"\n"), 0644)
	}
	return alternate, cleanup, nil
}
