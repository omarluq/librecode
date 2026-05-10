//nolint:testpackage // These tests exercise unexported terminal autocomplete helpers.
package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlashSuggestionsIncludesSkill(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(slashSuggestions()))
	for _, suggestion := range slashSuggestions() {
		names = append(names, suggestion.Name)
	}

	assert.Contains(t, names, "skill")
}
