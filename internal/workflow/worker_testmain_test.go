package workflow_test

import (
	"os"
	"testing"

	"github.com/omarluq/librecode/internal/executeworker"
)

func TestMain(testMain *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__execute-worker" {
		if err := executeworker.Serve(os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}

		os.Exit(0)
	}

	os.Exit(testMain.Run())
}
