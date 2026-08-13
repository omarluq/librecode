package terminal_test

import (
	"testing"

	"github.com/omarluq/librecode/internal/testutil"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.Cleanup(testutil.TestMainHome("terminal")))
}
