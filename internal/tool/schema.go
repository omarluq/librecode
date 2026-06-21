package tool

import (
	"encoding/json"

	"github.com/samber/oops"
)

// Schema is an immutable JSON Schema document.
type Schema struct {
	raw json.RawMessage
}

// EmptySchema returns the zero schema, meaning no explicit schema was provided.
func EmptySchema() Schema {
	return Schema{raw: nil}
}

// SchemaFromMap converts a generic JSON object into a typed schema blob.
func SchemaFromMap(value map[string]any) (Schema, error) {
	if len(value) == 0 {
		return EmptySchema(), nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return EmptySchema(), oops.In("tool").Code("tool_schema_marshal").Wrapf(err, "marshal tool schema")
	}

	return SchemaFromRaw(encoded)
}

// ToMap decodes the schema into a generic JSON object.
func (schema Schema) ToMap() (map[string]any, error) {
	if schema.IsEmpty() {
		return map[string]any{}, nil
	}

	var decoded map[string]any
	if err := json.Unmarshal(schema.RawMessage(), &decoded); err != nil {
		return nil, oops.In("tool").Code("tool_schema_unmarshal").Wrapf(err, "decode tool schema")
	}

	return decoded, nil
}

// MustToMap decodes the schema into a generic JSON object and panics on failure.
func (schema Schema) MustToMap() map[string]any {
	decoded, err := schema.ToMap()
	if err != nil {
		panic(err)
	}

	return decoded
}

// MustSchemaFromMap converts a generic JSON object into a typed schema blob and panics on failure.
func MustSchemaFromMap(value map[string]any) Schema {
	schema, err := SchemaFromMap(value)
	if err != nil {
		panic(err)
	}

	return schema
}

// SchemaFromRaw validates and stores a raw JSON Schema document.
func SchemaFromRaw(raw []byte) (Schema, error) {
	if len(raw) == 0 {
		return EmptySchema(), nil
	}

	if !json.Valid(raw) {
		return EmptySchema(), oops.In("tool").Code("tool_schema_invalid_json").Errorf("tool schema must be valid JSON")
	}

	copied := append(json.RawMessage(nil), raw...)

	return Schema{raw: copied}, nil
}

// IsEmpty reports whether the schema is unset.
func (schema Schema) IsEmpty() bool {
	return len(schema.raw) == 0
}

// RawMessage returns a defensive copy of the raw JSON schema.
func (schema Schema) RawMessage() json.RawMessage {
	if schema.IsEmpty() {
		return nil
	}

	return append(json.RawMessage(nil), schema.raw...)
}

// MarshalJSON implements json.Marshaler.
func (schema Schema) MarshalJSON() ([]byte, error) {
	if schema.IsEmpty() {
		return []byte("null"), nil
	}

	return schema.RawMessage(), nil
}
