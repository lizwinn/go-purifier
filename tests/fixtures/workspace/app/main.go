// SPDX-License-Identifier: MIT

// Command app prints its arguments in title case, with the library of the
// other module of its workspace.
package main

import (
	"fmt"
	"os"
	"strings"

	"example.com/lib"
)

func main() {
	fmt.Println(lib.Title(strings.Join(os.Args[1:], " ")))
}
