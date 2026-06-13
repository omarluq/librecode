package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeKeyNameAliasesBacktabToShiftTab(t *testing.T) {
	t.Parallel()

	assert.Equal(t, keyShiftTab, normalizeKeyName("BackTab"))

	keys := normalizedEventKeys(tcell.NewEventKey(tcell.KeyBacktab, "", tcell.ModNone))
	_, ok := keys[keyShiftTab]
	assert.True(t, ok)
}
