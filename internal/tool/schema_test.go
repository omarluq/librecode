package tool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestSchemaRawRoundTrip(t *testing.T) {
	t.Parallel()

	schema, err := tool.SchemaFromRaw([]byte(`{"type":"object","required":["path"]}`))
	require.NoError(t, err)

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
}

func TestSchemaRejectsInvalidRawJSON(t *testing.T) {
	t.Parallel()

	_, err := tool.SchemaFromRaw([]byte(`{"type"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid JSON")
}
