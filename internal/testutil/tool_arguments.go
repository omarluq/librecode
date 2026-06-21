package testutil

import (
	"encoding/json"

	"github.com/omarluq/librecode/internal/tool"
)

// ToolArguments converts a generic test argument map into validated tool arguments.
func ToolArguments(input map[string]any) tool.Arguments {
	payload, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}

	arguments, err := tool.ArgumentsFromRaw(payload)
	if err != nil {
		panic(err)
	}

	return arguments
}

// ToolArgumentFields decodes tool arguments into generic fields for assertions.
func ToolArgumentFields(input tool.Arguments) map[string]any {
	fields := map[string]any{}
	if err := json.Unmarshal(input.RawMessage(), &fields); err != nil {
		panic(err)
	}

	return fields
}
