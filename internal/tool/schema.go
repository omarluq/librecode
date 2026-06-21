package tool

import (
	"encoding/json"
	"reflect"

	invopopjsonschema "github.com/invopop/jsonschema"
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

// schemaFromType generates a strict JSON Schema document from a Go type.
func schemaFromType(inputType reflect.Type, lookupComment func(reflect.Type, string) string) (Schema, error) {
	reflector := invopopjsonschema.Reflector{
		Anonymous:      true,
		DoNotReference: true,
		LookupComment:  lookupComment,
	}
	schema := reflector.ReflectFromType(inputType)
	schema.Version = ""

	encoded, err := json.Marshal(schema)
	if err != nil {
		return EmptySchema(), oops.In("tool").Code("tool_schema_marshal").Wrapf(err, "marshal generated tool schema")
	}

	return SchemaFromRaw(encoded)
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
