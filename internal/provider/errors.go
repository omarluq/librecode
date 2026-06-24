package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/samber/oops"
)

const (
	providerBodyPreviewBytes = 4096

	providerCodeContextKey          = "provider_code"
	providerParamContextKey         = "provider_param"
	providerTypeContextKey          = "provider_type"
	providerBodyPreviewContextKey   = "body_preview"
	providerBodyTruncatedContextKey = "body_truncated"
	providerRequestShapeContextKey  = "request_shape"
)

type providerError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ErrorDetails contains safe, bounded provider error diagnostics.
type ErrorDetails struct {
	Message       string
	Type          string
	Code          string
	Param         string
	BodyPreview   string
	BodyTruncated bool
}

// StatusError is a typed, inspectable provider HTTP status error.
type StatusError struct {
	Details      *ErrorDetails
	err          error
	RequestShape *RequestShape
	Status       int
}

func (err *StatusError) Error() string {
	if err == nil || err.err == nil {
		return ""
	}

	return err.err.Error()
}

func (err *StatusError) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.err
}

func providerStatusError(status int, content []byte, requestShape *RequestShape) error {
	details := providerErrorDetailsFromBytes(content)
	statusErr := &StatusError{
		Details:      &details,
		err:          nil,
		RequestShape: requestShape,
		Status:       status,
	}
	statusErr.err = providerStatusOops(statusErr)

	return statusErr
}

func providerStatusOops(statusErr *StatusError) error {
	message := providerStatusMessage(statusErr.Status, statusErr.Details)
	builder := oops.In("provider").Code("provider_status").With("status", statusErr.Status)

	if statusErr.Details.Type != "" {
		builder = builder.With(providerTypeContextKey, statusErr.Details.Type)
	}

	if statusErr.Details.Code != "" {
		builder = builder.With(providerCodeContextKey, statusErr.Details.Code)
	}

	if statusErr.Details.Param != "" {
		builder = builder.With(providerParamContextKey, statusErr.Details.Param)
	}

	if statusErr.Details.BodyPreview != "" {
		builder = builder.With(providerBodyPreviewContextKey, statusErr.Details.BodyPreview)
	}

	if statusErr.Details.BodyTruncated {
		builder = builder.With(providerBodyTruncatedContextKey, true)
	}

	if !statusErr.RequestShape.empty() {
		builder = builder.With(providerRequestShapeContextKey, statusErr.RequestShape.Payload())
	}

	return builder.Errorf("%s", message)
}

func providerStatusMessage(status int, details *ErrorDetails) string {
	if details.Message != "" {
		return details.Message
	}

	return fmt.Sprintf("provider returned HTTP %d", status)
}

func providerErrorToOops(code string, providerError *providerError) error {
	message := providerError.Message
	if message == "" {
		message = "provider returned an error"
	}

	return oops.In("provider").
		Code(code).
		With(jsonTypeKey, providerError.Type).
		With(providerCodeContextKey, providerError.Code).
		Errorf("%s", message)
}

func errorMessageFromBytes(content []byte) string {
	return providerErrorDetailsFromBytes(content).Message
}

func providerErrorDetailsFromBytes(content []byte) ErrorDetails {
	bodyPreview, bodyTruncated := providerTextPreview(string(content))
	details := ErrorDetails{
		Message:       "",
		Type:          "",
		Code:          "",
		Param:         "",
		BodyPreview:   bodyPreview,
		BodyTruncated: bodyTruncated,
	}

	parsed, ok := providerErrorDetailsFromJSON(content)
	if !ok {
		details.Message = bodyPreview

		return details
	}

	parsed.Message, _ = providerTextPreview(parsed.Message)
	parsed.BodyPreview = details.BodyPreview
	parsed.BodyTruncated = details.BodyTruncated

	return parsed
}

type providerErrorEnvelope struct {
	Message          providerOptionalString `json:"message"`
	Detail           providerOptionalString `json:"detail"`
	ErrorDescription providerOptionalString `json:"error_description"`
	Type             providerOptionalString `json:"type"`
	Code             providerOptionalString `json:"code"`
	Param            providerOptionalString `json:"param"`
	Error            json.RawMessage        `json:"error"`
	Errors           json.RawMessage        `json:"errors"`
}

type providerOptionalString string

func (value *providerOptionalString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*value = ""

		return nil
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		*value = providerOptionalString(text)

		return nil
	}

	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err == nil {
		*value = providerOptionalString(number.String())

		return nil
	}

	return nil
}

func (value providerOptionalString) String() string {
	return strings.TrimSpace(string(value))
}

func providerErrorDetailsFromJSON(content []byte) (ErrorDetails, bool) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return emptyProviderErrorDetails(), false
	}

	var message string
	if err := json.Unmarshal(trimmed, &message); err == nil {
		return newProviderErrorDetails(message, "", "", ""), true
	}

	var envelope providerErrorEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err == nil {
		return envelope.errorDetails(), true
	}

	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err == nil {
		return providerErrorDetailsFromList(items), true
	}

	return emptyProviderErrorDetails(), false
}

func (envelope *providerErrorEnvelope) errorDetails() ErrorDetails {
	details := newProviderErrorDetails(
		firstNonEmptyString(envelope.Message.String(), envelope.Detail.String(), envelope.ErrorDescription.String()),
		envelope.Type.String(),
		envelope.Code.String(),
		envelope.Param.String(),
	)

	if len(envelope.Error) > 0 {
		if nestedDetails, ok := providerErrorDetailsFromJSON(envelope.Error); ok {
			details.Message = firstNonEmptyString(details.Message, nestedDetails.Message)
			details.Type = firstNonEmptyString(nestedDetails.Type, details.Type)
			details.Code = firstNonEmptyString(nestedDetails.Code, details.Code)
			details.Param = firstNonEmptyString(nestedDetails.Param, details.Param)
		}
	}

	if details.Message == "" && len(envelope.Errors) > 0 {
		if errorsDetails, ok := providerErrorDetailsFromJSON(envelope.Errors); ok {
			details.Message = errorsDetails.Message
			details.Type = firstNonEmptyString(details.Type, errorsDetails.Type)
			details.Code = firstNonEmptyString(details.Code, errorsDetails.Code)
			details.Param = firstNonEmptyString(details.Param, errorsDetails.Param)
		}
	}

	return details
}

func providerErrorDetailsFromList(values []json.RawMessage) ErrorDetails {
	for _, value := range values {
		if details, ok := providerErrorDetailsFromJSON(value); ok && details.Message != "" {
			return details
		}
	}

	return emptyProviderErrorDetails()
}

func newProviderErrorDetails(message, errorType, code, param string) ErrorDetails {
	return ErrorDetails{
		Message:       message,
		Type:          errorType,
		Code:          code,
		Param:         param,
		BodyPreview:   "",
		BodyTruncated: false,
	}
}

func emptyProviderErrorDetails() ErrorDetails {
	return newProviderErrorDetails("", "", "", "")
}

func errorMessageFromMap(value map[string]any) string {
	content, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	return providerErrorDetailsFromBytes(content).Message
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}

	return ""
}

func providerTextPreview(text string) (string, bool) {
	preview := strings.ToValidUTF8(strings.TrimSpace(text), "�")
	if len(preview) <= providerBodyPreviewBytes {
		return preview, false
	}

	end := 0
	for end < len(preview) {
		_, size := utf8.DecodeRuneInString(preview[end:])
		if end+size > providerBodyPreviewBytes {
			break
		}

		end += size
	}

	return preview[:end], true
}
