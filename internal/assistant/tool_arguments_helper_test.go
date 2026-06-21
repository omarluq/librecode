package assistant_test

import (
	"encoding/json"

	"github.com/omarluq/librecode/internal/tool"
)

func testToolArguments(input map[string]any) tool.Arguments {
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

func testToolArgumentFields(input tool.Arguments) map[string]any {
	fields := map[string]any{}
	if err := json.Unmarshal(input.RawMessage(), &fields); err != nil {
		panic(err)
	}

	return fields
}
