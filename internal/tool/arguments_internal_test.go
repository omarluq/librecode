package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgumentsInvalidStoredRawFallbacks(t *testing.T) {
	t.Parallel()

	arguments := Arguments{raw: []byte(`{`)}

	fields, err := arguments.Fields()
	require.Error(t, err)
	assert.Nil(t, fields)
	assert.False(t, arguments.HasField("path"))
	assert.True(t, arguments.IsEmpty())
}
