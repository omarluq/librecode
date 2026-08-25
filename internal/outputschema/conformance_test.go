package outputschema_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/outputschema"
)

func TestAcceptedKeywordGroups(t *testing.T) {
	t.Parallel()

	const (
		nullValue = `null`
		xOne      = `{"x":1}`
	)

	cases := []struct {
		name, schema, valid, invalid string
	}{
		{
			"core",
			`{"$schema":"https://json-schema.org/draft/2020-12/schema",` +
				`"$defs":{"a/b~c":{"type":"string"}},"$ref":"#/$defs/a~1b~0c"}`,
			`"ok"`,
			`1`,
		},
		{"general type", `{"type":["null","string"]}`, nullValue, `1`},
		{"general enum", `{"enum":[1,"one"]}`, `1.0`, `2`},
		{"general const", `{"const":{"x":1}}`, `{"x":1.0}`, `{"x":2}`},
		{"composition allOf", `{"allOf":[{"type":"number"},{"minimum":1}]}`, `1`, `0`},
		{"composition anyOf", `{"anyOf":[{"type":"string"},{"type":"null"}]}`, nullValue, `false`},
		{"composition oneOf", `{"oneOf":[{"type":"number"},{"type":"string"}]}`, `"x"`, `false`},
		{"composition not", `{"not":{"type":"null"}}`, `0`, nullValue},
		{"object properties", `{"properties":{"x":{"type":"string"}}}`, `{"x":"a"}`, xOne},
		{"object required", `{"required":["x"]}`, `{"x":null}`, `{}`},
		{
			"object additionalProperties boolean",
			`{"properties":{"x":true},"additionalProperties":false}`,
			xOne,
			`{"y":1}`,
		},
		{"object additionalProperties schema", `{"additionalProperties":{"type":"string"}}`, `{"x":"a"}`, `{"x":1}`},
		{"object minProperties", `{"minProperties":1}`, `{"x":1}`, `{}`},
		{"object maxProperties", `{"maxProperties":1}`, xOne, `{"x":1,"y":2}`},
		{"array items boolean", `{"items":false}`, `[]`, `[1]`},
		{"array items schema", `{"items":{"type":"string"}}`, `["x"]`, `[1]`},
		{"array prefixItems", `{"prefixItems":[{"type":"string"},{"type":"number"}]}`, `["x",1]`, `[1,"x"]`},
		{"array minItems", `{"minItems":1}`, `[null]`, `[]`},
		{"array maxItems", `{"maxItems":1}`, `[null]`, `[null,null]`},
		{"array uniqueItems", `{"uniqueItems":true}`, `[1,2]`, `[1,1.0]`},
		{"string minLength", `{"minLength":2}`, `"ab"`, `"a"`},
		{"string maxLength", `{"maxLength":1}`, `"a"`, `"ab"`},
		{"string pattern", `{"pattern":"^[a-z]+$"}`, `"abc"`, `"123"`},
		{"number minimum", `{"minimum":1}`, `1`, `0`},
		{"number maximum", `{"maximum":1}`, `1`, `2`},
		{"number exclusiveMinimum", `{"exclusiveMinimum":1}`, `2`, `1`},
		{"number exclusiveMaximum", `{"exclusiveMaximum":1}`, `0`, `1`},
		{"number multipleOf", `{"multipleOf":0.1}`, `1e0`, `1.05`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			contract, err := outputschema.Admit(test.schema)
			if err != nil {
				t.Fatalf("Admit() error = %v", err)
			}

			if _, err = contract.Validate(test.valid); err != nil {
				t.Fatalf("Validate(valid) error = %v", err)
			}

			if _, err = contract.Validate(test.invalid); err == nil {
				t.Fatal("Validate(invalid) unexpectedly succeeded")
			}
		})
	}
}

func TestAnnotationsAreRetainedAndIgnored(t *testing.T) {
	t.Parallel()

	schema := `{"title":"t","description":"d","default":{"format":"data"},` +
		`"examples":[{"unknown":"data"}],"deprecated":true,"readOnly":true,"writeOnly":true,"type":"string"}`

	contract, err := outputschema.Admit(schema)
	if err != nil {
		t.Fatal(err)
	}

	annotations := []string{
		"title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly",
	}
	for _, keyword := range annotations {
		if !strings.Contains(string(contract.Canonical), `"`+keyword+`"`) {
			t.Fatalf("canonical schema omitted annotation %q", keyword)
		}
	}
}

func TestUnsupportedKeywordRejectedAtEverySchemaPosition(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, schema string }{
		{"root", `{"format":"email"}`},
		{"definition", `{"$defs":{"x":{"format":"email"}}}`},
		{"property", `{"properties":{"x":{"format":"email"}}}`},
		{"composition", `{"allOf":[{"format":"email"}]}`},
		{"not", `{"not":{"format":"email"}}`},
		{"items", `{"items":{"format":"email"}}`},
		{"prefix item", `{"prefixItems":[{"format":"email"}]}`},
		{"additional properties", `{"additionalProperties":{"format":"email"}}`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertAdmissionError(t, test.schema, "unsupported keyword")
		})
	}
}

func TestReferenceSecurityBoundaries(t *testing.T) {
	t.Parallel()

	cases := []string{
		`{"$defs":{"x":true},"$ref":""}`,
		`{"$defs":{"x":true},"$ref":"#"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/x/y"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/x?query"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/x#fragment"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/%78"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/~2"}`,
		`{"$defs":{"x":true},"$ref":"file:///tmp/schema.json"}`,
		`{"$defs":{"x":true},"$ref":"https://example.invalid/schema"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/missing"}`,
	}

	for index, schema := range cases {
		t.Run(fmt.Sprintf("form_%d", index), func(t *testing.T) {
			t.Parallel()
			assertAdmissionError(t, schema, "")
		})
	}
}

func assertAdmissionError(t *testing.T, schema, reason string) {
	t.Helper()

	_, err := outputschema.Admit(schema)
	if err == nil {
		t.Fatal("Admit() unexpectedly succeeded")
	}

	var contractErr *outputschema.Error
	if !errors.As(err, &contractErr) || contractErr.Code != "invalid_output_schema" {
		t.Fatalf("Admit() error = %v", err)
	}

	if reason != "" && !strings.Contains(contractErr.Reason, reason) {
		t.Fatalf("Admit() reason = %q, want substring %q", contractErr.Reason, reason)
	}
}
