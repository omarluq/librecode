package assistant

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/tool"
)

// MVMExecutionProfile identifies the guarantees selected for an MVM execution.
type MVMExecutionProfile string

const (
	// MVMExecutionProfileTurn selects synchronous provider-turn execution.
	MVMExecutionProfileTurn MVMExecutionProfile = "turn"
	// MVMExecutionProfileDurable selects detached persisted workflow execution.
	MVMExecutionProfileDurable MVMExecutionProfile = "durable"
)

// ExecutionResultKind discriminates the terminal or acceptance outcome of an
// MVM execution independently of the provider-facing tool name.
type ExecutionResultKind string

const (
	// ExecutionResultCompleted indicates successful synchronous completion.
	ExecutionResultCompleted ExecutionResultKind = "completed"
	// ExecutionResultAccepted indicates accepted durable work.
	ExecutionResultAccepted ExecutionResultKind = "accepted"
	// ExecutionResultFailed indicates execution failed after admission.
	ExecutionResultFailed ExecutionResultKind = "failed"
	// ExecutionResultCanceled indicates execution was canceled.
	ExecutionResultCanceled ExecutionResultKind = "canceled"
	// ExecutionResultTimedOut indicates execution exceeded its deadline.
	ExecutionResultTimedOut ExecutionResultKind = "timed_out"
	// ExecutionResultRejected indicates the request was not admitted.
	ExecutionResultRejected ExecutionResultKind = "rejected"
)

const (
	executionResultKindKey = "result_kind"
	executionIdentityKey   = "execution"
	executionProfileKey    = "profile"
	executionIdentityMVM   = "mvm"

	// MaxProviderVisibleExecutionResultSize is the byte limit for an encoded
	// execution value returned in provider-visible text. Other worker output is
	// independently bounded by the worker protocol.
	MaxProviderVisibleExecutionResultSize = 1 << 20
)

// ExecutionResultEnvelope defines the additive common result contract. Fields
// used only by later phases are optional so producers need not synthesize them.
type ExecutionResultEnvelope struct {
	PartialValue   any                  `json:"partial_value,omitempty"`
	ResultValue    any                  `json:"result_value,omitempty"`
	ToolDetails    map[string]any       `json:"tool_details,omitempty"`
	Truncation     *ExecutionTruncation `json:"truncation,omitempty"`
	Usage          map[string]any       `json:"usage,omitempty"`
	RunID          string               `json:"run_id,omitempty"`
	Stderr         string               `json:"stderr,omitempty"`
	ResultKind     ExecutionResultKind  `json:"result_kind"`
	WorkflowTaskID string               `json:"workflow_task_id,omitempty"`
	Kind           string               `json:"kind,omitempty"`
	Name           string               `json:"name,omitempty"`
	State          string               `json:"state,omitempty"`
	Stdout         string               `json:"stdout,omitempty"`
	Profile        MVMExecutionProfile  `json:"profile"`
	Execution      string               `json:"execution"`
	Content        []tool.ContentBlock  `json:"content,omitempty"`
	Artifacts      []map[string]any     `json:"artifacts,omitempty"`
	Warnings       []string             `json:"warnings,omitempty"`
}

// ExecutionTruncation makes bounded projections deterministic and measurable.
type ExecutionTruncation struct {
	Field         string `json:"field"`
	LimitBytes    int    `json:"limit_bytes"`
	OriginalBytes int    `json:"original_bytes"`
	VisibleBytes  int    `json:"visible_bytes"`
	OmittedBytes  int    `json:"omitted_bytes"`
}

func executionResultDetails(
	base map[string]any,
	profile MVMExecutionProfile,
	kind ExecutionResultKind,
) map[string]any {
	if base == nil {
		base = map[string]any{}
	}

	base[executionResultKindKey] = kind
	base[executionIdentityKey] = executionIdentityMVM
	base[executionProfileKey] = profile

	return base
}

func boundProviderVisibleExecutionResult(result tool.Result) tool.Result {
	originalBytes := providerVisibleExecutionResultSize(result)
	if originalBytes <= MaxProviderVisibleExecutionResultSize {
		return result
	}

	details := executionResultDetails(
		map[string]any{executeResultValueKey: nil},
		profileFromExecutionDetails(result.Details),
		resultKindFromExecutionDetails(result.Details),
	)
	truncation := &ExecutionTruncation{
		Field:         "provider_result",
		LimitBytes:    MaxProviderVisibleExecutionResultSize,
		OriginalBytes: originalBytes,
		VisibleBytes:  0,
		OmittedBytes:  originalBytes,
	}
	details["truncation"] = truncation

	bounded := tool.TextResult("", details)

	detailsBytes, err := json.Marshal(details)
	if err != nil {
		return bounded
	}

	const (
		detailsSeparator         = "\ndetails:\n"
		truncationMetadataMargin = 64
	)

	available := MaxProviderVisibleExecutionResultSize - len(detailsSeparator) - len(detailsBytes) -
		truncationMetadataMargin

	text := result.Text()
	if available > 0 {
		text = truncateExecutionUTF8(text, available)
		bounded = tool.TextResult(text, details)
	}

	truncation.VisibleBytes = providerVisibleExecutionResultSize(bounded)
	truncation.OmittedBytes = originalBytes - truncation.VisibleBytes

	return bounded
}

func providerVisibleExecutionResultSize(result tool.Result) int {
	text := strings.TrimSpace(result.Text())
	if len(result.Details) == 0 {
		return len(text)
	}

	details, err := json.Marshal(result.Details)
	if err != nil {
		return len(text)
	}

	if text == "" {
		return len("details:\n") + len(details)
	}

	return len(text) + len("\ndetails:\n") + len(details)
}

func profileFromExecutionDetails(details map[string]any) MVMExecutionProfile {
	profile, ok := details[executionProfileKey].(MVMExecutionProfile)
	if ok {
		return profile
	}

	if raw, rawOK := details[executionProfileKey].(string); rawOK {
		return MVMExecutionProfile(raw)
	}

	return MVMExecutionProfileTurn
}

func resultKindFromExecutionDetails(details map[string]any) ExecutionResultKind {
	kind, ok := details[executionResultKindKey].(ExecutionResultKind)
	if ok {
		return kind
	}

	if raw, rawOK := details[executionResultKindKey].(string); rawOK {
		return ExecutionResultKind(raw)
	}

	return ExecutionResultFailed
}

func truncateExecutionUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}

	return value
}

func encodeProviderVisibleExecutionValue(value any) (
	text string,
	resultValue any,
	partialValue any,
	truncation *ExecutionTruncation,
	err error,
) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", nil, nil, nil, oops.In("assistant").Code("execute_result_encode").
			Wrapf(err, "encode execute result")
	}

	if len(encoded) <= MaxProviderVisibleExecutionResultSize {
		return string(encoded), value, nil, nil, nil
	}

	visible := encoded[:MaxProviderVisibleExecutionResultSize]
	for !utf8.Valid(visible) {
		visible = visible[:len(visible)-1]
	}

	partial := string(visible)
	truncation = &ExecutionTruncation{
		Field: "result_value", LimitBytes: MaxProviderVisibleExecutionResultSize,
		OriginalBytes: len(encoded), VisibleBytes: len(visible), OmittedBytes: len(encoded) - len(visible),
	}

	return partial, nil, partial, truncation, nil
}
