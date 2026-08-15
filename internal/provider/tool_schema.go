package provider

import (
	"encoding/json"
	"maps"
	"reflect"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

func requestToolDefinitions(request *CompletionRequest) []llm.ToolDefinition {
	if request != nil && request.Request.DisableTools {
		return nil
	}

	if request != nil && len(request.Request.Tools) > 0 {
		return request.Request.Tools
	}

	return builtinToolDefinitions()
}

func builtinToolDefinitions() []llm.ToolDefinition {
	definitions := tool.AllDefinitions()

	tools := make([]llm.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, llm.ToolDefinition{
			Schema:      definition.Schema,
			Name:        string(definition.Name),
			Description: definition.Description,
			ReadOnly:    definition.ReadOnly,
		})
	}

	return tools
}

// ResponseToolsFromDefinitions returns OpenAI Responses API tool declarations for definitions.
func ResponseToolsFromDefinitions(definitions []llm.ToolDefinition) []map[string]any {
	return responseTools(definitions)
}

func responseTools(definitions []llm.ToolDefinition) []map[string]any {
	tools := make([]map[string]any, 0, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		tools = append(tools, map[string]any{
			jsonTypeKey:        functionToolType,
			jsonToolNameKey:    definition.Name,
			jsonDescriptionKey: definition.Description,
			jsonToolParamsKey:  toolParameterSchemaForDefinition(definition),
			"strict":           false,
		})
	}

	return tools
}

// OpenAIChatToolsFromDefinitions returns OpenAI Chat Completions tool declarations for definitions.
func OpenAIChatToolsFromDefinitions(definitions []llm.ToolDefinition) []map[string]any {
	return openAIChatTools(definitions)
}

func openAIChatTools(definitions []llm.ToolDefinition) []map[string]any {
	tools := make([]map[string]any, 0, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		tools = append(tools, map[string]any{
			jsonTypeKey: functionToolType,
			jsonFunctionKey: map[string]any{
				jsonToolNameKey:    definition.Name,
				jsonDescriptionKey: definition.Description,
				jsonToolParamsKey:  toolParameterSchemaForDefinition(definition),
			},
		})
	}

	return tools
}

func toolArgumentsFromJSON(argumentsJSON string) tool.Arguments {
	arguments, err := tool.ArgumentsFromRaw([]byte(argumentsJSON))
	if err != nil {
		return tool.EmptyArguments()
	}

	return arguments
}

type toolParameterSchema struct {
	raw tool.Schema
}

func (schema toolParameterSchema) MarshalJSON() ([]byte, error) {
	encoded := schema.raw.RawMessage()
	if len(encoded) == 0 {
		return []byte("null"), nil
	}

	return encoded, nil
}

func rawToolParameterSchema(schema tool.Schema) toolParameterSchema {
	return toolParameterSchema{raw: schema}
}

func toolParameterSchemaForDefinition(definition *llm.ToolDefinition) toolParameterSchema {
	if definition == nil {
		return rawToolParameterSchema(freeformToolSchema())
	}

	if !definition.Schema.IsEmpty() {
		return rawToolParameterSchema(definition.Schema)
	}

	return rawToolParameterSchema(freeformToolSchema())
}

func anthropicToolParameterSchemaForDefinition(definition *llm.ToolDefinition) any {
	schema := toolParameterSchemaForDefinition(definition)

	var document map[string]any
	if err := json.Unmarshal(schema.raw.RawMessage(), &document); err != nil {
		return schema
	}

	variants, variantsOK := document["oneOf"].([]any)
	if !variantsOK || len(variants) == 0 {
		return schema
	}

	flattened, flattenedOK := mergeObjectSchemaVariants(variants)
	if !flattenedOK {
		return schema
	}

	for key, value := range document {
		if key != "oneOf" {
			flattened[key] = value
		}
	}

	return flattened
}

func mergeObjectSchemaVariants(variants []any) (map[string]any, bool) {
	properties := map[string]any{}

	for _, variant := range variants {
		object, objectOK := variant.(map[string]any)
		if !objectOK || object[jsonTypeKey] != jsonObjectType {
			return nil, false
		}

		variantProperties := schemaProperties(object)
		mergeSchemaProperties(properties, variantProperties)
	}

	return map[string]any{
		jsonTypeKey:            jsonObjectType,
		"additionalProperties": false,
		jsonPropertiesKey:      properties,
	}, true
}

func mergeSchemaProperties(destination, source map[string]any) {
	for name, property := range source {
		if existing, found := destination[name]; found {
			destination[name] = mergeObjectPropertySchemas(existing, property)
		} else {
			destination[name] = property
		}
	}
}

func mergeObjectPropertySchemas(left, right any) any {
	if reflect.DeepEqual(left, right) {
		return left
	}

	leftObject, leftOK := objectSchema(left)
	rightObject, rightOK := objectSchema(right)

	if !leftOK || !rightOK {
		return map[string]any{}
	}

	leftProperties, leftPropertiesOK := leftObject[jsonPropertiesKey].(map[string]any)
	rightProperties, rightPropertiesOK := rightObject[jsonPropertiesKey].(map[string]any)

	if !leftPropertiesOK || !rightPropertiesOK {
		return map[string]any{}
	}

	mergedProperties := maps.Clone(leftProperties)
	mergeSchemaProperties(mergedProperties, rightProperties)

	merged := maps.Clone(leftObject)
	delete(merged, jsonRequiredKey)
	merged[jsonPropertiesKey] = mergedProperties

	return merged
}

func objectSchema(value any) (map[string]any, bool) {
	object, objectOK := value.(map[string]any)

	return object, objectOK && object[jsonTypeKey] == jsonObjectType
}

func schemaProperties(schema map[string]any) map[string]any {
	properties, propertiesOK := schema[jsonPropertiesKey].(map[string]any)
	if !propertiesOK {
		return nil
	}

	return properties
}

func freeformToolSchema() tool.Schema {
	schema, err := tool.SchemaFromRaw([]byte(`{"type":"object","additionalProperties":true}`))
	if err != nil {
		panic(oops.In("provider").Code("tool_schema_freeform").Wrapf(err, "build freeform tool schema"))
	}

	return schema
}
