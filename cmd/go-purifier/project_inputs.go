// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type projectInput struct {
	Reference string `json:"reference"`
	URL       string `json:"url"`
	NarHash   string `json:"narHash"`
	Attribute string `json:"attribute"`
}
type resolvedModuleInputs struct {
	Inputs       []projectInput `json:"inputs"`
	Repositories []string       `json:"repositories"`
}

func moduleInputFlags(args []string) ([]string, []string, error) {
	var references, rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--no-inputs" {
			references = []string{}
		} else if args[i] == "--input" {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--input requires a Nix project reference")
			}
			i++
			references = append(references, args[i])
		} else {
			rest = append(rest, args[i])
		}
	}
	return references, rest, nil
}
func resolveModuleInputs(root string, previous []projectInput, references []string) (resolvedModuleInputs, error) {
	if len(previous) == 0 && len(references) == 0 {
		return resolvedModuleInputs{}, nil
	}
	helper := os.Getenv("GO_PURIFIER_INPUT_HELPER")
	if helper == "" {
		return resolvedModuleInputs{}, fmt.Errorf("Nix project inputs require the go-purifier toolchain wrapper")
	}
	request, err := json.Marshal(map[string]any{"previous": previous, "references": references, "roots": filepath.Join(root, stateDirName, "inputs")})
	if err != nil {
		return resolvedModuleInputs{}, err
	}
	cmd := exec.Command(helper)
	cmd.Stdin = bytes.NewReader(request)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return resolvedModuleInputs{}, err
	}
	var resolved resolvedModuleInputs
	err = json.Unmarshal(data, &resolved)
	return resolved, err
}
func (inputs resolvedModuleInputs) bind(root string) (string, error) {
	if len(inputs.Repositories) == 0 {
		for _, name := range []string{"input.work", "input.work.sum"} {
			if err := os.Remove(filepath.Join(root, stateDirName, name)); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
		return "", nil
	}
	state := filepath.Join(root, stateDirName)
	if err := os.MkdirAll(state, 0755); err != nil {
		return "", err
	}
	work := filepath.Join(state, "input.work")
	return work, bindModuleInputs(inputs.Repositories, root, work)
}

func stripModuleInputSums(root string, definitions []string, repositories []string) error {
	names, err := moduleInputNames(repositories)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	for _, definition := range definitions {
		if filepath.Base(definition) != "go.sum" && filepath.Base(definition) != "go.work.sum" {
			continue
		}
		path := filepath.Join(root, definition)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var lines []string
		changed := false
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && names[fields[0]] {
				changed = true
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
		}
		if !changed {
			continue
		}
		if len(lines) == 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			return err
		}
	}
	return nil
}
