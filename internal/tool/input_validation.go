package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/hot"
	"github.com/samber/oops"
	santhoshjsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	toolInputCommandKey         = "command"
	toolInputContentKey         = "content"
	toolInputPathKey            = "path"
	toolSchemaValidatorCacheCap = 64
)

type schemaValidatorCache struct {
	validators *hot.HotCache[string, *santhoshjsonschema.Schema]
}

func newSchemaValidatorCache() *schemaValidatorCache {
	return &schemaValidatorCache{
		validators: hot.NewHotCache[string, *santhoshjsonschema.Schema](hot.WTinyLFU, toolSchemaValidatorCacheCap).
			Build(),
	}
}

func validateToolInput(definition *Definition, input Arguments, cache *schemaValidatorCache) error {
	if err := validateToolInputRequiredArguments(definition, input); err != nil {
		return err
	}

	return validateToolInputSchema(definition, normalizeToolInputForValidation(definition, input), cache)
}

func validateToolInputSchema(definition *Definition, input Arguments, cache *schemaValidatorCache) error {
	if definition == nil || definition.Schema.IsEmpty() {
		return nil
	}

	validator, err := compiledToolInputSchema(definition.Schema, cache)
	if err != nil {
		return oops.In("tool").
			Code("compile_tool_schema").
			With("tool", definition.Name).
			Wrapf(err, "compile tool input schema")
	}

	var document any
	if err := json.Unmarshal(input.RawMessage(), &document); err != nil {
		return oops.In("tool").
			Code("decode_tool_input").
			With("tool", definition.Name).
			Wrapf(err, "decode tool input")
	}

	if err := validator.Validate(document); err != nil {
		return oops.In("tool").
			Code("invalid_tool_input").
			With("tool", definition.Name).
			Wrapf(err, "validate tool input")
	}

	return nil
}

func compiledToolInputSchema(schema Schema, cache *schemaValidatorCache) (*santhoshjsonschema.Schema, error) {
	cacheKey := schemaCacheKey(schema)
	if cache == nil {
		return compileToolInputSchema(cacheKey, schema)
	}

	validator, found, err := cache.validators.Get(cacheKey)
	if err != nil {
		return nil, oops.In("tool").Code("schema_validator_cache_get").Wrapf(err, "get cached schema validator")
	}

	if found {
		return validator, nil
	}

	validator, err = compileToolInputSchema(cacheKey, schema)
	if err != nil {
		return nil, err
	}

	cache.validators.Set(cacheKey, validator)

	return validator, nil
}

func normalizeToolInputForValidation(definition *Definition, input Arguments) Arguments {
	if definition == nil || definition.Name != NameAST {
		return input
	}

	fields, err := input.Fields()
	if err != nil {
		return input
	}

	var mode string
	if unmarshalErr := json.Unmarshal(fields["mode"], &mode); unmarshalErr != nil {
		return input
	}

	normalizedMode := astMode(mode)
	if normalizedMode == mode {
		return input
	}

	fields["mode"] = mustMarshalRaw(normalizedMode)

	encoded, err := json.Marshal(fields)
	if err != nil {
		return input
	}

	normalized, err := ArgumentsFromRaw(encoded)
	if err != nil {
		return input
	}

	return normalized
}

func compileToolInputSchema(cacheKey string, schema Schema) (*santhoshjsonschema.Schema, error) {
	var document any
	if err := json.Unmarshal(schema.RawMessage(), &document); err != nil {
		return nil, oops.In("tool").Code("tool_schema_unmarshal").Wrapf(err, "decode tool schema")
	}

	compiler := santhoshjsonschema.NewCompiler()
	compiler.DefaultDraft(santhoshjsonschema.Draft2020)

	resourceName := "tool-input-schema-" + cacheKey + ".json"
	if err := compiler.AddResource(resourceName, document); err != nil {
		return nil, oops.In("tool").Code("tool_schema_resource").Wrapf(err, "add tool schema resource")
	}

	validator, err := compiler.Compile(resourceName)
	if err != nil {
		return nil, oops.In("tool").Code("tool_schema_compile").Wrapf(err, "compile tool schema")
	}

	return validator, nil
}

func schemaCacheKey(schema Schema) string {
	sum := sha256.Sum256(schema.RawMessage())

	return hex.EncodeToString(sum[:])
}

func validateToolInputRequiredArguments(definition *Definition, input Arguments) error {
	required := requiredToolArguments(definition)
	if len(required) == 0 {
		return nil
	}

	for _, field := range required {
		if input.HasField(field) {
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

	required, _ := schemaRequiredArguments(definition.Schema)

	return required
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

func mustMarshalRaw(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(oops.In("tool").Code("marshal_raw_json").Wrapf(err, "marshal raw JSON value"))
	}

	return encoded
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
