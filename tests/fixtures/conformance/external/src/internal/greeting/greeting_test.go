// SPDX-License-Identifier: MIT

package greeting

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	got, err := Parse("name = \"Nix\"\nlanguage = \"de\"\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Name: "Nix", Language: "de"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Parse (-want +got):\n%s", diff)
	}
}

func TestGreet(t *testing.T) {
	if got, want := Greet(Config{Name: "Nix", Language: "de"}), "Hello, Nix (German)"; got != want {
		t.Errorf("Greet = %q, want %q", got, want)
	}
}
