package assistant

import (
	"math"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	anthropicProvider           = "anthropic"
	contextWindowExceededCode   = "context_window_exceeded"
	staleIdentityRefusal        = "stale_identity"
	minimumOutputLimitTolerance = 8
	outputLimitTolerancePercent = 0.02
)

// ResponseClass identifies the provider response category used by recovery policy.
type ResponseClass string

// Provider response classifications.
const (
	ResponseSuccess                 ResponseClass = "success"
	ResponseExplicitContextOverflow ResponseClass = "explicit_context_overflow"
	ResponseMetadataContextOverflow ResponseClass = "metadata_context_overflow"
	ResponseOutputLengthTruncation  ResponseClass = "output_length_truncation"
	ResponseContentFilter           ResponseClass = "content_filter"
	ResponseRefusal                 ResponseClass = "refusal"
	ResponseProviderError           ResponseClass = "provider_error"
)

// ResponseClassificationInput contains bounded provider metadata used to classify a response.
type ResponseClassificationInput struct {
	Err                  error
	Termination          llm.TerminationMetadata
	Provider             string
	API                  string
	Model                string
	FinishReason         llm.FinishReason
	RequestedMaxOutput   int
	ReportedOutputTokens int
}

// ResponseClassification records a response category and the metadata that determined it.
type ResponseClassification struct {
	Class                 ResponseClass
	FinishReason          llm.FinishReason
	IncompleteReason      string
	ContextOverflowSignal string
	OutputLimit           int
	ReportedOutputTokens  int
}

func classificationInput(value any) ResponseClassificationInput {
	switch input := value.(type) {
	case ResponseClassificationInput:
		return input
	case *ResponseClassificationInput:
		if input != nil {
			return *input
		}
	}

	return ResponseClassificationInput{
		Err: nil, Termination: llm.NewTerminationMetadata("", "", ""), Provider: "", API: "", Model: "",
		FinishReason: llm.FinishReasonUnknown, RequestedMaxOutput: 0, ReportedOutputTokens: 0,
	}
}

func recoveryDecisionInput(value any) OverflowRecoveryDecisionInput {
	switch input := value.(type) {
	case OverflowRecoveryDecisionInput:
		return input
	case *OverflowRecoveryDecisionInput:
		if input != nil {
			return *input
		}
	}

	return OverflowRecoveryDecisionInput{
		Classification: ResponseClassification{
			Class: ResponseProviderError, FinishReason: llm.FinishReasonUnknown, IncompleteReason: "",
			ContextOverflowSignal: "", OutputLimit: 0, ReportedOutputTokens: 0,
		},
		Identity: emptyRequestIdentity(), ActiveIdentity: emptyRequestIdentity(),
		Replay: ReplayState{
			RecoveryConsumed: false, ToolDispatchStarted: false, LineageAdvanced: false,
		},
		HasCompactionCandidate: false,
	}
}

// ClassifyResponse is provider-aware and deliberately uses only documented,
// bounded metadata. Error text is never searched for successful responses.
func ClassifyResponse(value any) ResponseClassification {
	input := classificationInput(value)

	out := ResponseClassification{
		Class:                 ResponseSuccess,
		FinishReason:          input.FinishReason,
		IncompleteReason:      input.Termination.IncompleteReason,
		ContextOverflowSignal: "",
		OutputLimit:           input.RequestedMaxOutput,
		ReportedOutputTokens:  input.ReportedOutputTokens,
	}
	if input.Err != nil {
		return classifyResponseError(input.Err, &out)
	}

	if classified, ok := classifyAnthropicResponse(&input, &out); ok {
		return classified
	}

	out.Class = finishReasonClass(input.FinishReason)

	return out
}

func classifyResponseError(err error, out *ResponseClassification) ResponseClassification {
	if IsStructuredContextWindowError(err) {
		out.Class = ResponseExplicitContextOverflow
		out.ContextOverflowSignal = "structured_error"
	} else {
		out.Class = ResponseProviderError
	}

	return *out
}

func classifyAnthropicResponse(
	input *ResponseClassificationInput,
	out *ResponseClassification,
) (ResponseClassification, bool) {
	if input.Provider != anthropicProvider {
		return *out, false
	}

	switch input.Termination.ProviderFinishReason {
	case "model_context_window_exceeded":
		out.Class = ResponseMetadataContextOverflow
		out.ContextOverflowSignal = "anthropic_model_context_window_exceeded"

		return *out, true
	case "max_tokens":
		premature := input.FinishReason == llm.FinishReasonLength && input.RequestedMaxOutput > 0 &&
			input.ReportedOutputTokens > 0 && !outputAtLimit(input.RequestedMaxOutput, input.ReportedOutputTokens)
		if premature {
			out.Class = ResponseMetadataContextOverflow
			out.ContextOverflowSignal = "anthropic_premature_max_tokens"

			return *out, true
		}
	}

	return *out, false
}

func finishReasonClass(reason llm.FinishReason) ResponseClass {
	switch reason {
	case llm.FinishReasonContentFilter:
		return ResponseContentFilter
	case llm.FinishReasonRefusal:
		return ResponseRefusal
	case llm.FinishReasonLength:
		return ResponseOutputLengthTruncation
	case llm.FinishReasonStop, llm.FinishReasonToolCalls:
		return ResponseSuccess
	case llm.FinishReasonUnknown, llm.FinishReasonError, llm.FinishReasonAborted:
		return ResponseProviderError
	}

	return ResponseProviderError
}

func outputLimitTolerance(maximum int) int {
	if maximum <= 0 {
		return 0
	}

	return max(minimumOutputLimitTolerance, int(math.Ceil(float64(maximum)*outputLimitTolerancePercent)))
}

func outputAtLimit(requested, reported int) bool {
	return requested > 0 && reported > 0 && reported >= requested-outputLimitTolerance(requested)
}

// IsStructuredContextWindowError excludes broad message guesses from replay.
func IsStructuredContextWindowError(err error) bool {
	code, ok := providerErrorCode(err)

	return ok && (code == contextWindowExceededCode || code == "context_length_exceeded")
}

// RequestIdentity identifies one provider request and its active prompt lineage.
type RequestIdentity struct {
	LogicalRequestID     string
	Provider             string
	Model                string
	LineageParentEntryID string
	ProviderAttempt      int
	CompactionGeneration uint64
	RecoveryAttempt      uint8
}

func emptyRequestIdentity() RequestIdentity {
	return RequestIdentity{
		LogicalRequestID: "", Provider: "", Model: "", LineageParentEntryID: "", ProviderAttempt: 0,
		CompactionGeneration: 0, RecoveryAttempt: 0,
	}
}

// ReplayState records conditions that make replay unsafe or redundant.
type ReplayState struct{ RecoveryConsumed, ToolDispatchStarted, LineageAdvanced bool }

// OverflowRecoveryDecisionInput contains all state used by overflow recovery policy.
type OverflowRecoveryDecisionInput struct {
	Classification           ResponseClassification
	Identity, ActiveIdentity RequestIdentity
	Replay                   ReplayState
	HasCompactionCandidate   bool
}

// OverflowRecoveryDecision reports whether recovery is allowed and why it may be refused.
type OverflowRecoveryDecision struct {
	Refusal string
	Recover bool
}

// DecideOverflowRecovery applies the bounded one-replay overflow recovery policy.
func DecideOverflowRecovery(value any) OverflowRecoveryDecision {
	input := recoveryDecisionInput(value)
	if input.Classification.Class != ResponseExplicitContextOverflow &&
		input.Classification.Class != ResponseMetadataContextOverflow {
		return OverflowRecoveryDecision{Refusal: "ineligible_class", Recover: false}
	}

	if input.Identity != input.ActiveIdentity {
		return OverflowRecoveryDecision{Refusal: staleIdentityRefusal, Recover: false}
	}

	if input.Replay.ToolDispatchStarted {
		return OverflowRecoveryDecision{Refusal: "side_effects_started", Recover: false}
	}

	if input.Replay.LineageAdvanced {
		return OverflowRecoveryDecision{Refusal: "lineage_advanced", Recover: false}
	}

	if input.Replay.RecoveryConsumed || input.Identity.RecoveryAttempt != 0 {
		return OverflowRecoveryDecision{Refusal: "retry_consumed", Recover: false}
	}

	if !input.HasCompactionCandidate {
		return OverflowRecoveryDecision{Refusal: "nothing_to_compact", Recover: false}
	}

	return OverflowRecoveryDecision{Refusal: "", Recover: true}
}
