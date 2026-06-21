package tool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestArgumentsFromRawNormalizesEmptyAndObjectInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
		raw  []byte
	}{
		{name: "empty bytes", raw: nil, want: `{}`},
		{name: "blank bytes", raw: []byte(" \n\t "), want: `{}`},
		{name: "null", raw: []byte(`null`), want: `{}`},
		{name: "object", raw: []byte(` { "path" : "README.md" } `), want: `{"path":"README.md"}`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			arguments, err := tool.ArgumentsFromRaw(testCase.raw)
			require.NoError(t, err)
			assert.JSONEq(t, testCase.want, string(arguments.RawMessage()))
		})
	}
}

func TestArgumentsFromRawRejectsInvalidJSONAndNonObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "invalid json", raw: []byte(`{`)},
		{name: "array", raw: []byte(`[]`)},
		{name: "string", raw: []byte(`"value"`)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := tool.ArgumentsFromRaw(testCase.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "decode tool arguments")
		})
	}
}

func TestArgumentsRawMessageIsDefensiveCopy(t *testing.T) {
	t.Parallel()

	arguments, err := tool.ArgumentsFromRaw([]byte(`{"path":"README.md"}`))
	require.NoError(t, err)

	raw := arguments.RawMessage()
	raw[0] = '['

	assert.JSONEq(t, `{"path":"README.md"}`, string(arguments.RawMessage()))
}

func TestArgumentsAccessors(t *testing.T) {
	t.Parallel()

	arguments, err := tool.ArgumentsFromRaw([]byte(`{"path":"README.md"}`))
	require.NoError(t, err)

	assert.False(t, arguments.IsEmpty())
	assert.False(t, arguments.IsZero())
	assert.True(t, arguments.HasField("path"))
	assert.False(t, arguments.HasField("missing"))
	assert.JSONEq(t, `{"path":"README.md"}`, arguments.String())

	fields, err := arguments.Fields()
	require.NoError(t, err)
	assert.JSONEq(t, `"README.md"`, string(fields["path"]))

	var decoded struct {
		Path string `json:"path"`
	}
	require.NoError(t, arguments.Decode(&decoded))
	assert.Equal(t, "README.md", decoded.Path)

	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	assert.JSONEq(t, `{"path":"README.md"}`, string(encoded))
}

func TestArgumentsEmptyAccessors(t *testing.T) {
	t.Parallel()

	arguments := tool.EmptyArguments()

	assert.True(t, arguments.IsEmpty())
	assert.True(t, arguments.IsZero())
	assert.JSONEq(t, `{}`, arguments.String())
	assert.False(t, arguments.HasField("path"))

	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(encoded))
}

func TestArgumentsZeroValueAccessors(t *testing.T) {
	t.Parallel()

	var arguments tool.Arguments

	assert.JSONEq(t, `{}`, string(arguments.RawMessage()))
	assert.True(t, arguments.IsEmpty())
	assert.True(t, arguments.IsZero())
}

func TestArgumentsDecodeReturnsError(t *testing.T) {
	t.Parallel()

	arguments, err := tool.ArgumentsFromRaw([]byte(`{"path":"README.md"}`))
	require.NoError(t, err)

	var invalidTarget chan string

	err = arguments.Decode(&invalidTarget)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode tool input")
}
