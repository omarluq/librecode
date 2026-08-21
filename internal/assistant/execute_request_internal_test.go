package assistant

import (
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const executeRequestTestName = "review"

type executeRequestTestCase struct {
	raw              string
	wantName         string
	wantArguments    string
	wantCode         string
	name             string
	wantProfile      MVMExecutionProfile
	durableAvailable bool
}

func executeRequestFailure(name, raw, code string) executeRequestTestCase {
	return executeRequestTestCase{
		raw: raw, wantName: "", wantArguments: "", wantCode: code, name: name,
		wantProfile: "", durableAvailable: false,
	}
}

func TestDecodeExecuteToolInput(t *testing.T) {
	t.Parallel()

	tests := []executeRequestTestCase{
		{
			raw: `{"source":" 1 + 1 "}`, wantName: "", wantArguments: "", wantCode: "",
			name: "turn defaults", wantProfile: MVMExecutionProfileTurn, durableAvailable: false,
		},
		{
			raw: `{"source":"1","profile":"durable","name":"  review  ",` +
				`"arguments":{"z":2,"a":1},"limits":{}}`,
			wantName: executeRequestTestName, wantArguments: `{"a":1,"z":2}`, wantCode: "",
			name: "durable canonical arguments", wantProfile: MVMExecutionProfileDurable, durableAvailable: true,
		},
		{
			raw:      `{"source":"` + strings.Repeat("x", maxExecuteNameSize+20) + `\n1","profile":"durable"}`,
			wantName: strings.Repeat("x", maxExecuteNameSize), wantArguments: "", wantCode: "",
			name: "durable derived bounded name", wantProfile: MVMExecutionProfileDurable, durableAvailable: true,
		},
		executeRequestFailure("unknown profile", `{"source":"1","profile":"fast"}`, "execute_profile_invalid"),
		executeRequestFailure(
			"durable unavailable", `{"source":"1","profile":"durable"}`, "execute_durable_unavailable",
		),
		executeRequestFailure("turn name", `{"source":"1","name":"named"}`, "execute_turn_name_unsupported"),
		executeRequestFailure(
			"turn arguments", `{"source":"1","arguments":{}}`, "execute_turn_arguments_unsupported",
		),
		executeRequestFailure(
			"unsupported limit", `{"source":"1","limits":{"timeout":1}}`, "execute_limit_unsupported",
		),
		executeRequestFailure(
			"output schema reserved", `{"source":"1","output_schema":{}}`, "execute_output_schema_unsupported",
		),
		executeRequestFailure(
			"unknown field", `{"source":"1","future":true}`, "execute_input_field_unsupported",
		),
		{
			raw: `{"source":"1","profile":"durable","arguments":[]}`, wantName: "", wantArguments: "",
			wantCode: "execute_arguments_invalid", name: "arguments must be object", wantProfile: "",
			durableAvailable: true,
		},
		{
			raw: `{"source":"1","profile":"durable","arguments":{"value":"` +
				strings.Repeat("x", maxExecuteArgumentsSize) + `"}}`,
			wantName: "", wantArguments: "", wantCode: "execute_arguments_limit", name: "arguments byte limit",
			wantProfile: "", durableAvailable: true,
		},
		executeRequestFailure("blank source", `{"source":"  "}`, "execute_source_required"),
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			arguments, err := tool.ArgumentsFromRaw([]byte(test.raw))
			require.NoError(t, err)

			request, err := decodeExecuteToolInput(arguments, test.durableAvailable)
			if test.wantCode != "" {
				require.Error(t, err)
				coded, ok := oops.AsOops(err)
				require.True(t, ok)
				assert.Equal(t, test.wantCode, coded.Code())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.wantProfile, request.Profile)
			assert.Equal(t, test.wantName, request.Name)

			if test.wantArguments != "" {
				assert.JSONEq(t, test.wantArguments, string(request.Arguments))
			}
		})
	}
}

func TestDecodeExecuteToolInputPreservesRequestedProfileOnRejection(t *testing.T) {
	t.Parallel()

	arguments, err := tool.ArgumentsFromRaw([]byte(`{"source":"1","profile":"durable"}`))
	require.NoError(t, err)

	request, err := decodeExecuteToolInput(arguments, false)
	require.Error(t, err)
	assert.Equal(t, MVMExecutionProfileDurable, request.Profile)
}

func TestExecuteDefinitionPublishesAvailableProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		submitter   WorkflowSubmitter
		name        string
		wantEnum    string
		wantDurable bool
	}{
		{
			name: "turn only", submitter: nil,
			wantEnum: `"enum":["turn"]`, wantDurable: false,
		},
		{
			name:      "turn and durable",
			submitter: &workflowSubmitterStub{request: nil, run: nil, err: nil},
			wantEnum:  `"enum":["turn","durable"]`, wantDurable: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			definition := newExecuteFacade(nil, nil, test.submitter, "").Definition()
			raw := string(definition.Schema.RawMessage())

			assert.Contains(t, raw, test.wantEnum)
			assert.Contains(t, raw, `"default":"turn"`)
			assert.Contains(t, raw, `"arguments"`)
			assert.Contains(t, raw, `"limits"`)
			assert.Contains(t, raw, `"output_schema"`)
			assert.Contains(t, raw, "Imports do not select")
			assert.Equal(t, test.wantDurable, strings.Contains(definition.Description, "profile durable"))
			assert.Equal(t, test.wantDurable, strings.Contains(definition.PromptSnippet, "durable workflows"))
			guidelines := strings.Join(definition.PromptGuidelines, " ")
			assert.Equal(t, test.wantDurable, strings.Contains(guidelines, "Durable execution"))
		})
	}
}
