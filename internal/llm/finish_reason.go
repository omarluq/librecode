package llm

// FinishReason describes why a model response stopped.
type FinishReason string

const (
	// FinishReasonUnknown means the provider did not report a stop reason.
	FinishReasonUnknown FinishReason = ""
	// FinishReasonStop means the model completed normally.
	FinishReasonStop FinishReason = "stop"
	// FinishReasonLength means the model hit an output or context limit.
	FinishReasonLength FinishReason = "length"
	// FinishReasonToolCalls means the model stopped to request tool execution.
	FinishReasonToolCalls FinishReason = "tool-calls"
	// FinishReasonContentFilter means provider policy filtered the response.
	FinishReasonContentFilter FinishReason = "content-filter"
	// FinishReasonError means the provider reported a generation error.
	FinishReasonError FinishReason = "error"
	// FinishReasonAborted means generation was canceled or aborted.
	FinishReasonAborted FinishReason = "aborted"
)
