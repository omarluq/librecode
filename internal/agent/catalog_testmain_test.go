package agent_test

import (
	"testing"

	"github.com/omarluq/librecode/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.TestMainHome("agent")(m.Run())
}
