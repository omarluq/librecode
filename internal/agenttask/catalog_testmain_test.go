package agenttask_test

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "librecode-agenttask-test-home-*")
	if err != nil {
		panic(err)
	}

	if err := os.Setenv("LIBRECODE_HOME", home); err != nil {
		panic(err)
	}

	goleak.VerifyTestMain(m, goleak.Cleanup(func(code int) {
		if err := os.RemoveAll(home); err != nil {
			panic(err)
		}

		os.Exit(code)
	}))
}
