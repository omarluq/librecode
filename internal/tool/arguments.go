package tool

import (
	"bytes"
	"encoding/json"

	"github.com/samber/oops"
)

// Arguments is an immutable raw JSON object for tool input arguments.
type Arguments struct {
	raw json.RawMessage
}

// EmptyArguments returns an empty JSON object argument payload.
func EmptyArguments() Arguments {
	return Arguments{raw: json.RawMessage(`{}`)}
}

// ArgumentsFromRaw validates and stores raw JSON object arguments.
func ArgumentsFromRaw(raw []byte) (Arguments, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return EmptyArguments(), nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return EmptyArguments(), oops.In("tool").Code("invalid_tool_arguments").Wrapf(err, "decode tool arguments")
	}

	encoded, err := json.Marshal(object)
	if err != nil {
		return EmptyArguments(), oops.In("tool").Code("encode_tool_arguments").Wrapf(err, "encode tool arguments")
	}

	return Arguments{raw: encoded}, nil
}

// RawMessage returns a defensive copy of the raw JSON object.
func (arguments Arguments) RawMessage() json.RawMessage {
	if len(arguments.raw) == 0 {
		return json.RawMessage(`{}`)
	}

	return append(json.RawMessage(nil), arguments.raw...)
}

// String returns the raw JSON object string.
func (arguments Arguments) String() string {
	return string(arguments.RawMessage())
}

// IsEmpty reports whether arguments contain no fields.
func (arguments Arguments) IsEmpty() bool {
	fields, err := arguments.Fields()

	return err != nil || len(fields) == 0
}

// IsZero reports whether arguments should be omitted from JSON payloads.
func (arguments Arguments) IsZero() bool {
	return arguments.IsEmpty()
}

// Fields decodes arguments as raw JSON object fields.
func (arguments Arguments) Fields() (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(arguments.RawMessage(), &fields); err != nil {
		return nil, oops.In("tool").Code("decode_tool_argument_fields").Wrapf(err, "decode tool argument fields")
	}

	return fields, nil
}

// HasField reports whether a top-level field is present.
func (arguments Arguments) HasField(name string) bool {
	fields, err := arguments.Fields()
	if err != nil {
		return false
	}

	_, ok := fields[name]

	return ok
}

// Decode unmarshals arguments into a concrete input struct.
func (arguments Arguments) Decode(target any) error {
	if err := json.Unmarshal(arguments.RawMessage(), target); err != nil {
		return oops.In("tool").Code("decode_input").Wrapf(err, "decode tool input")
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
func (arguments Arguments) MarshalJSON() ([]byte, error) {
	return arguments.RawMessage(), nil
}
