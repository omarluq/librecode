package assistant

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"
)

type providerError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func providerStatusError(code string, status int, content []byte) error {
	message := errorMessageFromBytes(content)
	if message == "" {
		message = fmt.Sprintf("provider returned HTTP %d", status)
	}

	return oops.In("assistant").Code(code).With("status", status).Errorf("%s", message)
}

func providerErrorToOops(code string, providerError *providerError) error {
	message := providerError.Message
	if message == "" {
		message = "provider returned an error"
	}

	return oops.In("assistant").
		Code(code).
		With(jsonTypeKey, providerError.Type).
		With("provider_code", providerError.Code).
		Errorf("%s", message)
}

func errorMessageFromBytes(content []byte) string {
	var payload any
	if err := json.Unmarshal(content, &payload); err != nil {
		return strings.TrimSpace(string(content))
	}

	return errorMessage(payload)
}

func errorMessage(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if message, ok := typed["message"].(string); ok {
			return message
		}
		if nested, ok := typed["error"]; ok {
			return errorMessage(nested)
		}
	case string:
		return typed
	}

	return ""
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
