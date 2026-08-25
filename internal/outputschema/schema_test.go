package outputschema_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/outputschema"
)

func TestAdmitCanonicalIdentity(t *testing.T) {
	t.Parallel()

	first, err := outputschema.Admit(
		` { "properties": {"n":{"minimum":1.0,"type":"number"}}, "type":"object" } `,
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := outputschema.Admit(`{"type":"object","properties":{"n":{"type":"number","minimum":1.0}}}`)
	if err != nil {
		t.Fatal(err)
	}

	if first.Digest != second.Digest || !bytes.Equal(first.Canonical, second.Canonical) {
		t.Fatal("canonical identities differ")
	}

	third, err := outputschema.Admit(`{"type":"object","properties":{"n":{"type":"number","minimum":1}}}`)
	if err != nil {
		t.Fatal(err)
	}

	if first.Digest == third.Digest {
		t.Fatal("number spellings must have distinct identity")
	}

	if len(first.Digest) != len("sha256:")+64 {
		t.Fatalf("unexpected digest %q", first.Digest)
	}
}

func TestValidateNumericSemanticsAndStrictJSON(t *testing.T) {
	t.Parallel()

	contract, err := outputschema.Admit(`{"type":"integer","const":1}`)
	if err != nil {
		t.Fatal(err)
	}

	canonical, err := contract.Validate("1.0")
	if err != nil {
		t.Fatal(err)
	}

	if string(canonical) != "1.0" {
		t.Fatalf("canonical = %s", canonical)
	}

	for _, candidate := range []string{"1 2", `{"x":1,"x":2}`, "```1```"} {
		if _, validationErr := contract.Validate(candidate); validationErr == nil {
			t.Fatalf("accepted %q", candidate)
		}
	}
}

func TestAdmitRejectsUnsupportedAndEscapingRefs(t *testing.T) {
	t.Parallel()

	cases := []string{
		`{"format":"email"}`,
		`{"$ref":"https://example.test/schema"}`,
		`{"$defs":{"x":{"type":"string"}},"allOf":[{"$ref":"#/$defs/x"},{"$ref":"#/$defs/x"}]}`,
		`{"$defs":{"x":{"$ref":"#/$defs/y"},"y":true},"$ref":"#/$defs/x"}`,
		`{"$defs":{"x":true},"$ref":"#/$defs/%78"}`,
	}

	for _, schema := range cases {
		if _, err := outputschema.Admit(schema); err == nil {
			t.Fatalf("accepted %s", schema)
		}
	}
}

func TestDocumentAndCandidateLimits(t *testing.T) {
	t.Parallel()

	oversizedSchema := strings.Repeat(" ", outputschema.MaxSchemaBytes) + "{}"
	if _, err := outputschema.Admit(oversizedSchema); err == nil {
		t.Fatal("accepted oversized schema")
	}

	contract, err := outputschema.Admit(`{"type":"string"}`)
	if err != nil {
		t.Fatal(err)
	}

	oversizedCandidate := `"` + strings.Repeat("x", outputschema.MaxCandidateBytes) + `"`
	if _, err := contract.Validate(oversizedCandidate); err == nil {
		t.Fatal("accepted oversized candidate")
	}
}
