// SPDX-License-Identifier: MIT

// Package greeter makes greetings with a fixed request ID.
package greeter

import "github.com/google/uuid"

// Greeting greets name, with the request ID of the nil UUID.
func Greeting(name string) string {
	return "Hello, " + name + " (request " + uuid.Nil.String() + ")"
}
