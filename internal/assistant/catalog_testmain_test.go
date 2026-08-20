package assistant_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/testutil"
	"go.uber.org/goleak"
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

	goleak.VerifyTestMain(
		m,
		goleak.IgnoreAnyFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http/internal/http2.(*ClientConn).readLoop"),
		goleak.Cleanup(testutil.TestMainHome("assistant")),
	)
}
