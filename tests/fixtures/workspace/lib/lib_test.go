// SPDX-License-Identifier: MIT

package lib

import "testing"

func TestTitle(t *testing.T) {
	if got, want := Title("hello nix"), "Hello Nix"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
}
