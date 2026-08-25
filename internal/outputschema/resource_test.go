package outputschema_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/outputschema"
)

func TestSchemaResourceLimits(t *testing.T) {
	t.Parallel()

	deep := `{}`
	for range outputschema.MaxDocumentDepth {
		deep = `{"default":` + deep + `}`
	}

	manyValues := `{"default":[` + strings.Repeat("null,", outputschema.MaxDocumentValues) + `null]}`

	var nodesBuilder strings.Builder
	nodesBuilder.WriteString(`{"properties":{`)

	for index := range outputschema.MaxSchemaNodes {
		if index > 0 {
			nodesBuilder.WriteByte(',')
		}

		nodesBuilder.WriteString(`"p`)
		nodesBuilder.WriteString(strconv.Itoa(index))
		nodesBuilder.WriteString(`":true`)
	}

	nodesBuilder.WriteString(`}}`)
	manyNodes := nodesBuilder.String()

	manyBranches := `{"allOf":[` + strings.Repeat("true,", outputschema.MaxBranches) + `true]}`
	manyEntries := `{"enum":[` + strings.Repeat("null,", outputschema.MaxArrayEntries) + `null]}`
	longString := `{"title":"` + strings.Repeat("x", outputschema.MaxStringBytes+1) + `"}`
	longPattern := `{"pattern":"` + strings.Repeat("x", outputschema.MaxPatternBytes+1) + `"}`

	var refsBuilder strings.Builder
	refsBuilder.WriteString(`{"$defs":{`)

	for index := 0; index <= outputschema.MaxRefs; index++ {
		if index > 0 {
			refsBuilder.WriteByte(',')
		}

		refsBuilder.WriteString(`"d`)
		refsBuilder.WriteString(strconv.Itoa(index))
		refsBuilder.WriteString(`":true`)
	}

	refsBuilder.WriteString(`},"properties":{`)

	for index := 0; index <= outputschema.MaxRefs; index++ {
		if index > 0 {
			refsBuilder.WriteByte(',')
		}

		refsBuilder.WriteString(`"p`)
		refsBuilder.WriteString(strconv.Itoa(index))
		refsBuilder.WriteString(`":{"$ref":"#/$defs/d`)
		refsBuilder.WriteString(strconv.Itoa(index))
		refsBuilder.WriteString(`"}`)
	}

	refsBuilder.WriteString(`}}`)
	manyRefs := refsBuilder.String()

	cases := []struct{ name, schema, reason string }{
		{"document depth", deep, "JSON depth exceeds limit"},
		{"document values", manyValues, "JSON values exceed limit"},
		{"schema nodes", manyNodes, "schema nodes exceed limit"},
		{"composition branches", manyBranches, "composition branches exceed limit"},
		{"reference occurrences", manyRefs, "$ref occurrences exceed limit"},
		{"array entries", manyEntries, "enum entries exceed limit"},
		{"schema string bytes", longString, "JSON string bytes exceed limit"},
		{"pattern bytes", longPattern, "pattern bytes exceed limit"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertAdmissionError(t, test.schema, test.reason)
		})
	}
}

func TestCandidateResourceLimits(t *testing.T) {
	t.Parallel()

	contract, err := outputschema.Admit(`{}`)
	if err != nil {
		t.Fatal(err)
	}

	const nullValue = `null`

	deep := nullValue
	for range outputschema.MaxCandidateDepth {
		deep = `[` + deep + `]`
	}

	manyValues := `[` + strings.Repeat("null,", outputschema.MaxCandidateValues) + `null]`
	largeCanonical := `[` + strings.Repeat(`"`+strings.Repeat("x", outputschema.MaxStringBytes)+`",`, 4) +
		`"x"]`

	cases := []struct{ name, candidate, reason string }{
		{"depth", deep, "JSON depth exceeds limit"},
		{"values", manyValues, "JSON values exceed limit"},
		{"canonical bytes", largeCanonical, "canonical domain-value bytes exceed limit"},
		{"invalid UTF-8", string([]byte{0xff}), "not valid UTF-8"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, validationErr := contract.Validate(test.candidate)
			if validationErr == nil || !strings.Contains(validationErr.Error(), test.reason) {
				t.Fatalf("Validate() error = %v, want substring %q", validationErr, test.reason)
			}
		})
	}
}

func TestAdmissionMalformedAndWrongTypes(t *testing.T) {
	t.Parallel()

	cases := []string{
		`true`,
		`[]`,
		`{"type":[]}`,
		`{"type":["string","string"]}`,
		`{"required":["x","x"]}`,
		`{"pattern":"["}`,
		`{"properties":[]}`,
		`{"items":1}`,
		`{"allOf":{}}`,
		`{"$schema":"http://json-schema.org/draft-07/schema#"}`,
		`{"type":"unknown"}`,
	}

	for index, schema := range cases {
		t.Run(fmt.Sprintf("case_%d", index), func(t *testing.T) {
			t.Parallel()
			assertAdmissionError(t, schema, "")
		})
	}
}
