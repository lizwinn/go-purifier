// SPDX-License-Identifier: MIT

// Command vcs prints the nil UUID and the display width of a string with
// an ANSI escape sequence. Its lockfile fetches both libraries from their
// git repositories rather than from the module proxy; the second is a
// module in a subdirectory of a repository of several modules.
package main

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

func main() {
	fmt.Println(uuid.Nil.String())
	fmt.Println(ansi.StringWidth("\x1b[1mbold\x1b[0m"))
}
