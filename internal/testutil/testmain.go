package testutil

import (
	"fmt"
	"os"
)

// TestMainHome creates and configures an isolated librecode home and returns a
// TestMain cleanup callback that removes it before exiting with code.
func TestMainHome(packageName string) func(code int) {
	home, err := os.MkdirTemp("", fmt.Sprintf("librecode-%s-test-home-*", packageName))
	if err != nil {
		panic(err)
	}

	if err := os.Setenv("LIBRECODE_HOME", home); err != nil {
		panic(err)
	}

	return func(code int) {
		if err := os.RemoveAll(home); err != nil {
			panic(err)
		}

		os.Exit(code)
	}
}
