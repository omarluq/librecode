package assistant_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/omarluq/librecode/internal/executeworker"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__execute-worker" {
		if err := executeworker.Serve(os.Stdin, os.Stdout); err != nil {
			if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
				os.Exit(1)
			}

			os.Exit(1)
		}

		os.Exit(0)
	}

	home, err := os.MkdirTemp("", "librecode-assistant-test-home-*")
	if err != nil {
		panic(err)
	}

	if err := os.Setenv("LIBRECODE_HOME", home); err != nil {
		panic(err)
	}

	code := m.Run()

	if err := os.RemoveAll(home); err != nil {
		panic(err)
	}

	os.Exit(code)
}
