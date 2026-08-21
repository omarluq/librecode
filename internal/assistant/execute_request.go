package assistant

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	executeArgumentsKey     = "arguments"
	maxExecuteArgumentsSize = 64 << 10
	maxExecuteNameSize      = 80
)

// executeToolInput is the single provider-facing request contract. Presence is
// retained separately so omitted fields can be distinguished from zero values.
type executeToolInput struct {
	Source       string              `json:"source"`
	Profile      MVMExecutionProfile `json:"profile,omitempty"`
	Name         string              `json:"name,omitempty"`
	Arguments    json.RawMessage     `json:"arguments,omitempty"`
	Limits       json.RawMessage     `json:"limits,omitempty"`
	OutputSchema json.RawMessage     `json:"output_schema,omitempty"`

	hasName         bool
	hasArguments    bool
	hasLimits       bool
	hasOutputSchema bool
}

func decodeExecuteToolInput(input tool.Arguments, durableAvailable bool) (executeToolInput, error) {
	fields, err := input.Fields()
	if err != nil {
		return executeToolInput{}, executeRequestError("execute_input", err, "decode execute input")
	}

	if err := validateExecuteFieldNames(fields); err != nil {
		return executeToolInput{}, err
	}

	request := executeToolInput{
		Source: "", Profile: MVMExecutionProfileTurn, Name: "", Arguments: nil, Limits: nil,
		OutputSchema: nil, hasName: fields[executeNameKey] != nil,
		hasArguments: fields[executeArgumentsKey] != nil, hasLimits: fields["limits"] != nil,
		hasOutputSchema: fields["output_schema"] != nil,
	}
	if err := decodeExecuteScalars(fields, &request); err != nil {
		return request, err
	}

	if err := validateExecuteSourceAndName(&request); err != nil {
		return request, err
	}

	if err := decodeExecuteObjects(fields, &request); err != nil {
		return request, err
	}

	if err := validateExecuteProfile(&request, durableAvailable); err != nil {
		return request, err
	}

	return request, nil
}

func validateExecuteFieldNames(fields map[string]json.RawMessage) error {
	for name := range fields {
		switch name {
		case "source", "profile", executeNameKey, executeArgumentsKey, "limits", "output_schema":
		default:
			return oops.In("assistant").Code("execute_input_field_unsupported").
				Errorf("execute input field %q is unsupported", name)
		}
	}

	return nil
}

func decodeExecuteScalars(fields map[string]json.RawMessage, request *executeToolInput) error {
	if raw, ok := fields["source"]; ok {
		if err := json.Unmarshal(raw, &request.Source); err != nil {
			return executeRequestError("execute_input", err, "decode execute source")
		}
	}

	if raw, ok := fields["profile"]; ok {
		if err := json.Unmarshal(raw, &request.Profile); err != nil {
			return executeRequestError("execute_profile_invalid", err, "execute profile must be a string")
		}
	}

	if raw, ok := fields[executeNameKey]; ok {
		if err := json.Unmarshal(raw, &request.Name); err != nil {
			return executeRequestError("execute_input", err, "decode execute name")
		}
	}

	return nil
}

func validateExecuteSourceAndName(request *executeToolInput) error {
	if strings.TrimSpace(request.Source) == "" {
		return oops.In("assistant").Code("execute_source_required").
			Errorf("execute source is required")
	}

	if !utf8.ValidString(request.Source) {
		return oops.In("assistant").Code("execute_source_invalid_utf8").
			Errorf("execute source must be valid UTF-8")
	}

	request.Name = strings.TrimSpace(request.Name)
	if request.hasName && request.Name == "" {
		return oops.In("assistant").Code("execute_name_required").
			Errorf("execute name must not be blank")
	}

	if !utf8.ValidString(request.Name) {
		return oops.In("assistant").Code("execute_name_invalid_utf8").
			Errorf("execute name must be valid UTF-8")
	}

	if len(request.Name) > maxExecuteNameSize {
		return oops.In("assistant").Code("execute_name_limit").
			Errorf("execute name is %d bytes; limit is %d", len(request.Name), maxExecuteNameSize)
	}

	return nil
}

func decodeExecuteObjects(fields map[string]json.RawMessage, request *executeToolInput) error {
	if request.hasArguments {
		arguments, err := canonicalExecuteJSONObject(fields[executeArgumentsKey], "execute_arguments_invalid")
		if err != nil {
			return err
		}

		if len(arguments) > maxExecuteArgumentsSize {
			return oops.In("assistant").Code("execute_arguments_limit").Errorf(
				"canonical execute arguments are %d bytes; limit is %d",
				len(arguments), maxExecuteArgumentsSize,
			)
		}

		request.Arguments = arguments
	}

	if request.hasLimits {
		limits, err := canonicalExecuteJSONObject(fields["limits"], "execute_limits_invalid")
		if err != nil {
			return err
		}

		if !bytes.Equal(limits, []byte("{}")) {
			return oops.In("assistant").Code("execute_limit_unsupported").
				Errorf("execute limits do not currently accept any fields")
		}

		request.Limits = limits
	}

	if request.hasOutputSchema {
		return oops.In("assistant").Code("execute_output_schema_unsupported").
			Errorf("execute output_schema is not supported yet")
	}

	return nil
}

func validateExecuteProfile(request *executeToolInput, durableAvailable bool) error {
	if request.Profile != MVMExecutionProfileTurn && request.Profile != MVMExecutionProfileDurable {
		return oops.In("assistant").Code("execute_profile_invalid").
			Errorf("execute profile %q is invalid; expected turn or durable", request.Profile)
	}

	if request.Profile == MVMExecutionProfileTurn {
		if request.hasName {
			return oops.In("assistant").Code("execute_turn_name_unsupported").
				Errorf("execute name is only valid for the durable profile")
		}

		if request.hasArguments {
			return oops.In("assistant").Code("execute_turn_arguments_unsupported").
				Errorf("execute arguments are only valid for the durable profile")
		}

		return nil
	}

	if !durableAvailable {
		return oops.In("assistant").Code("execute_durable_unavailable").
			Errorf("durable execute is unavailable because no workflow submitter is configured")
	}

	if request.Name == "" {
		request.Name = deriveExecuteName(request.Source)
	}

	return nil
}

// canonicalExecuteJSONObject emits compact JSON with lexicographically sorted
// object keys. json.Number retains submitted number spelling until the shared
// numeric semantics required by Phase 2 are specified.
func canonicalExecuteJSONObject(raw json.RawMessage, code string) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, executeRequestError(code, err, "decode execute object")
	}

	if object == nil {
		return nil, oops.In("assistant").Code(code).
			Errorf("execute value must be a JSON object")
	}

	if err := ensureJSONEOF(decoder); err != nil {
		return nil, executeRequestError(code, err, "decode execute object")
	}

	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, executeRequestError(code, err, "encode canonical execute object")
	}

	return encoded, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}

		return executeRequestError("execute_json_trailing", err, "decode trailing execute JSON")
	}

	return nil
}

func deriveExecuteName(source string) string {
	const splitFirstLine = 2

	line := strings.TrimSpace(strings.SplitN(source, "\n", splitFirstLine)[0])
	if line == "" {
		line = "Durable execution"
	}

	if len(line) <= maxExecuteNameSize {
		return line
	}

	line = line[:maxExecuteNameSize]
	for !utf8.ValidString(line) {
		line = line[:len(line)-1]
	}

	return strings.TrimSpace(line)
}

func executeRequestError(code string, err error, message string) error {
	return oops.In("assistant").Code(code).Wrapf(err, "%s", message)
}
