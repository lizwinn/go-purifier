// SPDX-License-Identifier: MIT

// Command greet prints a greeting for the TOML configuration on stdin.
package main

import (
	"fmt"
	"io"
	"os"

	"example.com/hello/internal/greeting"
)

func main() {
	text, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	c, err := greeting.Parse(string(text))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(greeting.Greet(c))
}
