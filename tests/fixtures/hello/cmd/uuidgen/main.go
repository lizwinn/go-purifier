// SPDX-License-Identifier: MIT

// Command uuidgen prints a random UUID.
package main

import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	fmt.Println(uuid.New())
}
