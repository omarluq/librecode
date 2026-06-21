package provider

import (
	"encoding/json"
	"reflect"

	"github.com/invopop/jsonschema"
	"github.com/samber/hot"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	toolSchemaPathField         = "Path"
	toolSchemaLimitField        = "Limit"
	toolSchemaAllowIgnoredField = "AllowIgnored"
	builtinToolSchemaCacheSize  = 8
)

func toolSchemaAllowIgnoredDescription() string {
	return "Set true only when an ignored file is explicitly needed despite .gitignore/default ignores."
}

func astToolPathDescription() string {
	return "Path to the source file to inspect, relative to the current workspace or absolute."
}

// ResponseTools returns OpenAI Responses API tool declarations for a completion request.
func ResponseTools(request *CompletionRequest) []map[string]any {
	cache := newBuiltinToolSchemaCache()

	return cache.responseTools(requestToolDefinitions(request))
}

// OpenAIChatTools returns OpenAI Chat Completions tool declarations for a completion request.
func OpenAIChatTools(request *CompletionRequest) []map[string]any {
	cache := newBuiltinToolSchemaCache()

	return cache.openAIChatTools(requestToolDefinitions(request))
}

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
	names := builtinToolNames()

	definitions := make([]llm.ToolDefinition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, llm.ToolDefinition{
			Schema:      tool.EmptySchema(),
			Name:        name,
			Description: builtinToolDescription(name),
			ReadOnly:    name != jsonBashToolName && name != jsonEditToolName && name != jsonWriteToolName,
		})
	}

	return definitions
}

func builtinToolNames() []string {
	return []string{
		jsonReadToolName,
		jsonBashToolName,
		jsonEditToolName,
		jsonWriteToolName,
		jsonGrepToolName,
		jsonFindToolName,
		jsonLSToolName,
		jsonASTToolName,
	}
}

func builtinToolDescription(name string) string {
	switch name {
	case jsonReadToolName:
		return "Read file contents."
	case jsonBashToolName:
		return "Execute a bash command."
	case jsonEditToolName:
		return "Edit a file."
	case jsonWriteToolName:
		return "Write a file."
	case jsonGrepToolName:
		return "Search file contents."
	case jsonFindToolName:
		return "Find files by glob."
	case jsonLSToolName:
		return "List directory contents."
	case jsonASTToolName:
		return "Inspect source syntax trees."
	default:
		return "Tool."
	}
}

type builtinToolSchemaCache struct {
	schemas *hot.HotCache[string, *jsonschema.Schema]
}

func newBuiltinToolSchemaCache() builtinToolSchemaCache {
	return builtinToolSchemaCache{
		schemas: hot.NewHotCache[string, *jsonschema.Schema](hot.WTinyLFU, builtinToolSchemaCacheSize).
			WithLoaders(loadBuiltinToolSchemas).
			WithCopyOnRead(cloneGeneratedToolSchema).
			WithCopyOnWrite(cloneGeneratedToolSchema).
			Build(),
	}
}

// ResponseToolsFromDefinitions returns OpenAI Responses API tool declarations for definitions.
func ResponseToolsFromDefinitions(definitions []llm.ToolDefinition) []map[string]any {
	cache := newBuiltinToolSchemaCache()

	return cache.responseTools(definitions)
}

func (cache *builtinToolSchemaCache) responseTools(definitions []llm.ToolDefinition) []map[string]any {
	tools := make([]map[string]any, 0, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		tools = append(tools, map[string]any{
			jsonTypeKey:        functionToolType,
			jsonToolNameKey:    definition.Name,
			jsonDescriptionKey: definition.Description,
			jsonToolParamsKey:  cache.parameterSchema(definition),
			"strict":           false,
		})
	}

	return tools
}

// OpenAIChatToolsFromDefinitions returns OpenAI Chat Completions tool declarations for definitions.
func OpenAIChatToolsFromDefinitions(definitions []llm.ToolDefinition) []map[string]any {
	cache := newBuiltinToolSchemaCache()

	return cache.openAIChatTools(definitions)
}

func (cache *builtinToolSchemaCache) openAIChatTools(definitions []llm.ToolDefinition) []map[string]any {
	tools := make([]map[string]any, 0, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		tools = append(tools, map[string]any{
			jsonTypeKey: functionToolType,
			jsonFunctionKey: map[string]any{
				jsonToolNameKey:    definition.Name,
				jsonDescriptionKey: definition.Description,
				jsonToolParamsKey:  cache.parameterSchema(definition),
			},
		})
	}

	return tools
}

func toolArgumentsFromJSON(argumentsJSON string) map[string]any {
	arguments := map[string]any{}
	if argumentsJSON == "" {
		return arguments
	}

	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		return map[string]any{}
	}

	return arguments
}

type toolParameterSchema struct {
	generated *jsonschema.Schema
	raw       tool.Schema
}

func (schema toolParameterSchema) MarshalJSON() ([]byte, error) {
	if schema.generated != nil {
		return marshalGeneratedToolSchema(schema.generated), nil
	}

	encoded := schema.raw.RawMessage()
	if len(encoded) == 0 {
		return []byte("null"), nil
	}

	return encoded, nil
}

func rawToolParameterSchema(schema tool.Schema) toolParameterSchema {
	return toolParameterSchema{generated: nil, raw: schema}
}

func generatedToolParameterSchema(schema *jsonschema.Schema) toolParameterSchema {
	return toolParameterSchema{generated: schema, raw: tool.EmptySchema()}
}

func (cache *builtinToolSchemaCache) parameterSchema(definition *llm.ToolDefinition) toolParameterSchema {
	if definition != nil && !definition.Schema.IsEmpty() {
		return rawToolParameterSchema(definition.Schema)
	}

	if definition == nil {
		return rawToolParameterSchema(freeformToolSchema())
	}

	schema, ok := cache.schemaForName(definition.Name)
	if !ok {
		return rawToolParameterSchema(freeformToolSchema())
	}

	return generatedToolParameterSchema(schema)
}

func (schema toolParameterSchema) payloadMap() map[string]any {
	wrapped, err := tool.SchemaFromRaw(mustMarshalToolParameterSchema(schema))
	if err != nil {
		panic(oops.In("provider").Code("tool_schema_payload").Wrapf(err, "build tool schema payload"))
	}

	return wrapped.MustToMap()
}

func (cache *builtinToolSchemaCache) schemaForName(name string) (*jsonschema.Schema, bool) {
	schema, found, err := cache.schemas.Get(name)
	if err != nil || !found {
		return nil, false
	}

	return schema, true
}

func loadBuiltinToolSchemas(names []string) (map[string]*jsonschema.Schema, error) {
	inputTypes := builtinToolInputTypes()

	schemas := make(map[string]*jsonschema.Schema, len(names))
	for _, name := range names {
		inputType, ok := inputTypes[name]
		if !ok {
			continue
		}

		schemas[name] = builtinToolSchema(inputType)
	}

	return schemas, nil
}

func builtinToolInputTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		jsonReadToolName:  reflect.TypeFor[tool.ReadInput](),
		jsonBashToolName:  reflect.TypeFor[tool.BashInput](),
		jsonEditToolName:  reflect.TypeFor[tool.EditInput](),
		jsonWriteToolName: reflect.TypeFor[tool.WriteInput](),
		jsonGrepToolName:  reflect.TypeFor[tool.GrepInput](),
		jsonFindToolName:  reflect.TypeFor[tool.FindInput](),
		jsonLSToolName:    reflect.TypeFor[tool.LSInput](),
		jsonASTToolName:   reflect.TypeFor[tool.ASTInput](),
	}
}

// ToolParameterSchema returns the JSON parameter schema for a local tool definition.
func ToolParameterSchema(definition *llm.ToolDefinition) map[string]any {
	cache := newBuiltinToolSchemaCache()

	return cache.parameterSchema(definition).payloadMap()
}

func freeformToolSchema() tool.Schema {
	schema, err := tool.SchemaFromRaw([]byte(`{"type":"object","additionalProperties":true}`))
	if err != nil {
		panic(oops.In("provider").Code("tool_schema_freeform").Wrapf(err, "build freeform tool schema"))
	}

	return schema
}

func builtinToolSchema(inputType reflect.Type) *jsonschema.Schema {
	reflector := jsonschema.Reflector{
		Anonymous:      true,
		DoNotReference: true,
		LookupComment:  lookupToolSchemaComment,
	}
	schema := reflector.ReflectFromType(inputType)
	schema.Version = ""

	return schema
}

func lookupToolSchemaComment(inputType reflect.Type, fieldName string) string {
	if fieldName == "" {
		return ""
	}

	comments, ok := toolSchemaFieldComments()[inputType]
	if !ok {
		return ""
	}

	return comments[fieldName]
}

func toolSchemaFieldComments() map[reflect.Type]map[string]string {
	return map[reflect.Type]map[string]string{
		reflect.TypeFor[tool.ASTInput](): {
			"Line":                      "One-based line number for mode=node or mode=tree.",
			"Depth":                     "Optional recursion depth for mode=symbols.",
			toolSchemaPathField:         astToolPathDescription(),
			"Mode":                      "Inspection mode: 'outline' (default), 'symbols', 'query', 'node', or 'tree'.",
			"Query":                     "Tree-sitter S-expression query for mode=query.",
			toolSchemaAllowIgnoredField: toolSchemaAllowIgnoredDescription(),
		},
		reflect.TypeFor[tool.BashInput](): {
			"Timeout": "Optional timeout in seconds.",
			"Command": "Bash command to execute in the current workspace.",
		},
		reflect.TypeFor[tool.EditInput](): {
			toolSchemaPathField: "Path to the file to edit, relative to the current workspace or absolute.",
		},
		reflect.TypeFor[tool.FindInput](): {
			toolSchemaLimitField: "Optional maximum number of paths.",
			"Pattern":            "Glob pattern for file paths, such as **/*.go.",
			toolSchemaPathField:  "Optional directory to search under.",
		},
		reflect.TypeFor[tool.GrepInput](): {
			"Context":            "Optional number of context lines around each match.",
			toolSchemaLimitField: "Optional maximum number of matches.",
			"Pattern":            "Regular expression or literal string to search for.",
			toolSchemaPathField:  "Optional file or directory to search under.",
			"Glob":               "Optional glob filter such as **/*.go.",
			"IgnoreCase":         "Whether to match case-insensitively.",
			"Literal":            "Whether pattern should be treated as literal text.",
		},
		reflect.TypeFor[tool.LSInput](): {
			toolSchemaLimitField: "Optional maximum number of entries.",
			toolSchemaPathField:  "Optional directory to list.",
		},
		reflect.TypeFor[tool.ReadInput](): {
			"Offset":                    "Optional 1-indexed line number to start reading from.",
			toolSchemaLimitField:        "Optional maximum number of lines to return.",
			toolSchemaPathField:         "Path to the file to read, relative to the current workspace or absolute.",
			toolSchemaAllowIgnoredField: toolSchemaAllowIgnoredDescription(),
		},
		reflect.TypeFor[tool.Replacement](): {
			"OldText": "Exact text to replace. Must match a unique, non-overlapping region.",
			"NewText": "Replacement text.",
		},
		reflect.TypeFor[tool.WriteInput](): {
			"Content":           "Complete file content to write.",
			toolSchemaPathField: "Path to create or overwrite, relative to the current workspace or absolute.",
		},
	}
}

func cloneGeneratedToolSchema(schema *jsonschema.Schema) *jsonschema.Schema {
	if schema == nil {
		return nil
	}

	var clone jsonschema.Schema
	if err := json.Unmarshal(marshalGeneratedToolSchema(schema), &clone); err != nil {
		panic(oops.In("provider").Code("tool_schema_clone").Wrapf(err, "clone generated tool schema"))
	}

	return &clone
}

func marshalGeneratedToolSchema(schema *jsonschema.Schema) []byte {
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(oops.In("provider").Code("tool_schema_marshal").Wrapf(err, "marshal generated tool schema"))
	}

	return encoded
}

func mustMarshalToolParameterSchema(schema toolParameterSchema) []byte {
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(oops.In("provider").Code("tool_schema_marshal").Wrapf(err, "marshal tool schema"))
	}

	return encoded
}
