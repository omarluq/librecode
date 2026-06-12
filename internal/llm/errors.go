package llm

import "errors"

// ErrorKind identifies provider error classes that assistant orchestration can handle.
type ErrorKind string

const (
	// ErrorKindUnknown is an unclassified provider error.
	ErrorKindUnknown ErrorKind = "unknown"
	// ErrorKindAuth is an authentication or authorization error.
	ErrorKindAuth ErrorKind = "auth"
	// ErrorKindRateLimit is a provider rate-limit error.
	ErrorKindRateLimit ErrorKind = "rate_limit"
	// ErrorKindContextOverflow means the provider rejected the request as too large.
	ErrorKindContextOverflow ErrorKind = "context_overflow"
	// ErrorKindTimeout is a provider timeout.
	ErrorKindTimeout ErrorKind = "timeout"
	// ErrorKindNetwork is a transport error before a provider response was received.
	ErrorKindNetwork ErrorKind = "network"
	// ErrorKindDecode is an invalid or unsupported provider response shape.
	ErrorKindDecode ErrorKind = "decode"
	// ErrorKindServer is a provider server-side error.
	ErrorKindServer ErrorKind = "server"
	// ErrorKindBadRequest is a provider-side request validation error.
	ErrorKindBadRequest ErrorKind = "bad_request"
)

// ProviderError is a typed provider error with optional provider metadata.
type ProviderError struct {
	Cause        error          `json:"-"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Kind         ErrorKind      `json:"kind"`
	Provider     string         `json:"provider,omitempty"`
	Model        string         `json:"model,omitempty"`
	Code         string         `json:"code,omitempty"`
	ProviderCode string         `json:"provider_code,omitempty"`
	Message      string         `json:"message"`
	StatusCode   int            `json:"status_code,omitempty"`
}

// Error returns a human-readable provider error message.
func (err *ProviderError) Error() string {
	if err == nil {
		return ""
	}
	if err.Message != "" {
		return err.Message
	}
	if err.Cause != nil {
		return err.Cause.Error()
	}
	if err.Code != "" {
		return "provider error: " + err.Code
	}

	return "provider error"
}

// Unwrap returns the wrapped cause.
func (err *ProviderError) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}

// IsKind reports whether err has the given provider error kind.
func IsKind(err error, kind ErrorKind) bool {
	providerError, ok := AsProviderError(err)

	return ok && providerError.Kind == kind
}

// AsProviderError returns err as a ProviderError when possible.
func AsProviderError(err error) (*ProviderError, bool) {
	if providerError, ok := errors.AsType[*ProviderError](err); ok {
		return providerError, true
	}

	return nil, false
}
