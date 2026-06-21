package tool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestSchemaMapAndRawRoundTrip(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"type":     "object",
		"required": []string{"path"},
	}
	schema := tool.MustSchemaFromMap(original)
	original["type"] = "mutated"

	decoded := schema.MustToMap()
	assert.Equal(t, "object", decoded["type"])
	assert.Equal(t, []any{"path"}, decoded["required"])

	raw := schema.RawMessage()
	raw[0] = '['

	assert.JSONEq(t, `{"type":"object","required":["path"]}`, string(schema.RawMessage()))
}

func TestSchemaEmptyAndMarshal(t *testing.T) {
	t.Parallel()

	schema := tool.EmptySchema()
	assert.True(t, schema.IsEmpty())
	assert.Nil(t, schema.RawMessage())

	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `null`, string(encoded))

	decoded := schema.MustToMap()
	assert.Empty(t, decoded)
}

func TestSchemaRejectsInvalidRawJSON(t *testing.T) {
	t.Parallel()

	_, err := tool.SchemaFromRaw([]byte(`{"type"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid JSON")
}
