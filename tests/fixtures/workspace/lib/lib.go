// SPDX-License-Identifier: MIT

// Package lib converts text to title case.
package lib

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Title returns s in English title case.
func Title(s string) string {
	return cases.Title(language.English).String(s)
}
