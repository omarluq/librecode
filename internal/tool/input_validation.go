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

var builtinRequiredToolArguments = map[Name][]string{
	NameRead:  {toolInputPathKey},
	NameBash:  {toolInputCommandKey},
	NameEdit:  {toolInputPathKey, "edits"},
	NameWrite: {toolInputPathKey, toolInputContentKey},
	NameGrep:  {"pattern"},
	NameFind:  {"pattern"},
	NameAST:   {toolInputPathKey},
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
	if required, ok := builtinRequiredToolArguments[definition.Name]; ok {
		return required
	}

	return nil
}

func schemaRequiredArguments(schema map[string]any) ([]string, bool) {
	if len(schema) == 0 {
		return nil, false
	}
	raw, ok := schema["required"]
	if !ok {
		return nil, false
	}
	switch values := raw.(type) {
	case []string:
		return values, true
	case []any:
		required := make([]string, 0, len(values))
		for _, value := range values {
			name, ok := value.(string)
			if !ok || strings.TrimSpace(name) == "" {
				continue
			}
			required = append(required, name)
		}

		return required, true
	default:
		return nil, false
	}
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
