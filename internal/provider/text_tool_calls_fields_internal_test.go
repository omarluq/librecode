package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTextToolFieldsNestedContainersAndMissingClosingTags(t *testing.T) {
	t.Parallel()

	fields := textToolFields(
		`<tool_name>read</tool_name><input>{"path":"README.md","count":2,"empty":null}</input><broken>ignored`,
	)

	assert.Equal(t, expectedReadToolName, fields[textToolNameField])
	assert.Equal(t, testToolPath, fields[jsonPathKey])
	assert.Equal(t, "2", fields["count"])
	assert.Empty(t, fields["empty"])
	assert.NotContains(t, fields, "broken")
}

func TestApplyTextToolAliasesDoesNotOverwriteMissingValues(t *testing.T) {
	t.Parallel()

	arguments := map[string]any{jsonContentKey: testExistingKey}
	applyTextToolAliases(jsonWriteToolName, map[string]string{}, arguments)
	assert.Equal(t, testExistingKey, arguments[jsonContentKey])
}

func TestNormalizeTextToolNameAliasesAndUnknowns(t *testing.T) {
	t.Parallel()

	assert.Equal(t, expectedBashToolName, NormalizeTextToolName(" command "))
	assert.Equal(t, expectedFindToolName, NormalizeTextToolName("find"))
	assert.Empty(t, NormalizeTextToolName("nope"))
}

func TestTextToolArgumentNameMappings(t *testing.T) {
	t.Parallel()

	assert.Equal(t, expectedPathKey, textToolArgumentName(jsonReadToolName, "filename"))
	assert.Equal(t, expectedAllowIgnoredKey, textToolArgumentName(jsonReadToolName, jsonAllowIgnoredKey))
	assert.Contains(t, []string{textToolArgumentName("grep-tool", jsonIgnoreCaseKey)}, jsonIgnoreCaseKey)
	assert.Equal(t, expectedCommandKey, textToolArgumentName(jsonBashToolName, "cmd"))
	assert.Equal(t, "other", textToolArgumentName(jsonReadToolName, "other"))
}

func TestEncodeToolArgumentsInvalidValueFallback(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "{}", EncodeToolArguments(map[string]any{"bad": func() {}}))
}

func TestHasTextFallbackToolCallsFalse(t *testing.T) {
	t.Parallel()

	assert.False(t, HasTextFallbackToolCalls([]ToolCall{{
		Arguments:     nil,
		Metadata:      nil,
		ID:            testCallID,
		Name:          jsonReadToolName,
		ArgumentsJSON: `{}`,
		TextFallback:  false,
	}}))
}
