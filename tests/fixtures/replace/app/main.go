// SPDX-License-Identifier: MIT

// Command replaceapp prints a greeting from a module that a replace
// directive points at the directory next to this one.
package main

import (
	"fmt"

	"example.com/greeter"
)

func main() {
	fmt.Println(greeter.Greeting("Nix"))
}
