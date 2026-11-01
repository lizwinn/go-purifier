// SPDX-License-Identifier: MIT

// Package greeting builds a greeting from a TOML configuration.
package greeting

import (
	"github.com/BurntSushi/toml"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Config is the greeting's configuration.
type Config struct {
	Name     string `toml:"name"`
	Language string `toml:"language"`
}

// Parse reads a configuration.
func Parse(text string) (Config, error) {
	var c Config
	_, err := toml.Decode(text, &c)
	return c, err
}

// Greet greets the configured name and names the configured language in
// English.
func Greet(c Config) string {
	tag := language.MustParse(c.Language)
	return "Hello, " + c.Name + " (" + display.English.Tags().Name(tag) + ")"
}
