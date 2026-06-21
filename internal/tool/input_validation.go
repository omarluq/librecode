package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"
)

const (
	toolInputCommandKey = "command"
	toolInputContentKey = "content"
	toolInputPathKey    = "path"
)

func builtinRequiredToolArguments() map[Name][]string {
	return map[Name][]string{
		NameRead:  {toolInputPathKey},
		NameBash:  {toolInputCommandKey},
		NameEdit:  {toolInputPathKey, "edits"},
		NameWrite: {toolInputPathKey, toolInputContentKey},
		NameGrep:  {"pattern"},
		NameFind:  {"pattern"},
		NameAST:   {toolInputPathKey},
	}
}

func validateToolInput(definition *Definition, input map[string]any) error {
	required := requiredToolArguments(definition)
	if len(required) == 0 {
		return nil
	}

	for _, field := range required {
		if _, ok := input[field]; ok {
			continue
		}

		return missingToolArgumentError(definition.Name, field, required)
	}

	return nil
}

func requiredToolArguments(definition *Definition) []string {
	if definition == nil {
		return nil
	}

	if required, ok := schemaRequiredArguments(definition.Schema); ok {
		return required
	}

	if required, ok := builtinRequiredToolArguments()[definition.Name]; ok {
		return required
	}

	return nil
}

func schemaRequiredArguments(schema Schema) ([]string, bool) {
	if schema.IsEmpty() {
		return nil, false
	}

	var document struct {
		Required []json.RawMessage `json:"required"`
	}
	if err := json.Unmarshal(schema.RawMessage(), &document); err != nil || len(document.Required) == 0 {
		return nil, false
	}

	required := make([]string, 0, len(document.Required))
	for _, rawName := range document.Required {
		var name string
		if err := json.Unmarshal(rawName, &name); err != nil {
			continue
		}

		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		required = append(required, name)
	}

	if len(required) == 0 {
		return nil, false
	}

	return required, true
}

func missingToolArgumentError(name Name, field string, required []string) error {
	return oops.In("tool").
		Code("missing_tool_argument").
		With("tool", name).
		With("argument", field).
		With("expected", expectedToolInputShape(required)).
		Errorf("%s %s is required; call %s with %s", name, field, name, expectedToolInputShape(required))
}

func expectedToolInputShape(required []string) string {
	shape := make(map[string]string, len(required))
	for _, field := range required {
		shape[field] = fmt.Sprintf("<%s>", field)
	}

	encoded, err := json.Marshal(shape)
	if err != nil {
		return "{}"
	}

	return string(encoded)
}
