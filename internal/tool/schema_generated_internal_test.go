package tool

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInputSchemaForName(t *testing.T) {
	t.Parallel()

	schema := inputSchemaForName(NameRead)
	require.False(t, schema.IsEmpty())

	var document map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(schema.RawMessage(), &document))
	assert.JSONEq(t, `false`, string(document["additionalProperties"]))

	assert.True(t, inputSchemaForName(Name("unknown")).IsEmpty())
}

func TestLookupSchemaFieldCommentFallbacks(t *testing.T) {
	t.Parallel()

	assert.Empty(t, lookupSchemaFieldComment(reflect.TypeFor[ReadInput](), ""))
	assert.Empty(t, lookupSchemaFieldComment(reflect.TypeFor[Result](), "Path"))
	assert.Empty(t, lookupSchemaFieldComment(reflect.TypeFor[ReadInput](), "Unknown"))
}
