package assistant_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
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
