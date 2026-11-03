// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"
)

// updateCommand updates the dependencies of the module or workspace of the
// current directory, then locks it:
//
//	go-purifier update [--lockfile <path>] [--tool-lockfile <path>] [<module>...]
//
// Without module paths, `go get -u -t ./...` in each main module updates
// every module its packages and their tests import to the latest minor or
// patch version. With module paths, `go get <module>@latest` updates only
// those. `go mod tidy` then settles go.mod and go.sum, and in a workspace
// `go work sync` brings the main modules to the same versions. The go
// command resolves the updates through the user's GOPROXY, the way a `go
// get` outside go-purifier does. With --tool-lockfile, the update and the
// lock work on the go.sum at that path (withToolLock).
func updateCommand(args []string) error {
	lockFlag, args, err := lockfileFlag(args)
	if err != nil {
		return err
	}
	toolFlag, modules, err := toolLockfileFlag(args)
	if err != nil {
		return err
	}
	goBin, err := realGo()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return withToolLock(goBin, cwd, toolFlag, func() error {
		return updateAt(goBin, cwd, lockFlag, modules)
	})
}

// updateAt updates the module or workspace of directory dir, then locks
// it into the lockfile that lockFlag or the lock root names.
func updateAt(goBin, cwd, lockFlag string, modules []string) error {
	env, err := readGoEnv(goBin, cwd)
	if err != nil {
		return err
	}
	root, workspace, err := env.lockRoot()
	if err != nil {
		return err
	}
	// Each main module is updated on its own go.mod. In a workspace,
	// GOWORK=off makes go get and go mod tidy act on that module alone.
	goEnviron := append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	hasInputs := false
	if old, err := readLock(lockPathAt(root, lockFlag)); err == nil && len(old.ProjectInputs) > 0 {
		inputs, err := resolveModuleInputs(root, old.ProjectInputs, nil)
		if err != nil {
			return err
		}
		work, err := inputs.bind(root)
		if err != nil {
			return err
		}
		hasInputs = true
		goEnviron = append(goEnviron, "GOWORK="+work, "GOFLAGS=-mod=readonly")
	}
	moduleEnviron := goEnviron
	if workspace || hasInputs {
		moduleEnviron = append(append([]string{}, goEnviron...), "GOWORK=off", "GOFLAGS=-mod=mod")
	}
	dirs, err := goOutput(goBin, root, goEnviron, "list", "-m", "-f", "{{.Dir}}")
	if err != nil {
		return err
	}
	get := []string{"get", "-u", "-t", "./..."}
	if len(modules) > 0 {
		get = []string{"get"}
		for _, m := range modules {
			if !strings.Contains(m, "@") {
				m += "@latest"
			}
			get = append(get, m)
		}
	}
	for _, dir := range strings.Fields(dirs) {
		fmt.Fprintf(os.Stderr, "go-purifier: updating %s\n", dir)
		if err := runGoIn(goBin, dir, moduleEnviron, get...); err != nil {
			return err
		}
		if err := runGoIn(goBin, dir, moduleEnviron, "mod", "tidy"); err != nil {
			return err
		}
	}
	if workspace {
		if err := runGoIn(goBin, root, goEnviron, "work", "sync"); err != nil {
			return err
		}
	}
	return lockAt(goBin, cwd, lockFlag)
}

// runGoIn runs the go command in directory dir with the output of the
// go command passed through.
func runGoIn(goBin, dir string, env []string, args ...string) error {
	if len(args) > 0 && (args[0] == "get" || (len(args) > 1 && args[0] == "mod" && args[1] == "tidy")) {
		if root, ok := findLockRoot(dir); ok {
			if lock, err := readLock(lockPathAt(root, "")); err == nil && len(lock.ProjectInputs) > 0 {
				inputs, err := resolveModuleInputs(root, lock.ProjectInputs, nil)
				if err != nil {
					return err
				}
				alternate, cleanup, err := moduleEdit(dir, inputs.Repositories)
				if err != nil {
					return err
				}
				patterns, err := moduleInputPatterns(inputs.Repositories)
				if err != nil {
					cleanup(false)
					return err
				}
				proxy, err := goOutput(goBin, dir, env, "env", "GOPROXY")
				if err != nil {
					cleanup(false)
					return err
				}
				env = append(env, "GOWORK=off", "GOFLAGS=-mod=mod -modfile="+alternate,
					"GOPROXY="+moduleInputProxy(inputs.Repositories, strings.TrimSpace(proxy)),
					"GONOSUMDB="+strings.Trim(os.Getenv("GONOSUMDB")+","+patterns, ","))
				_, err = goOutput(goBin, dir, env, args...)
				if finishErr := cleanup(err == nil); finishErr != nil {
					return finishErr
				}
				return err
			}
		}
	}
	_, err := goOutput(goBin, dir, env, args...)
	return err
}
