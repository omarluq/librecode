package lifecyclepayload

import (
	"errors"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/provider"
)

// Provider error payload keys define safe provider diagnostics exposed to extensions.
const (
	ProviderStatusKey        = "status"
	ProviderCodeKey          = "provider_code"
	ProviderParamKey         = "provider_param"
	ProviderTypeKey          = "provider_type"
	ProviderBodyPreviewKey   = "body_preview"
	ProviderBodyTruncatedKey = "body_truncated"
	ProviderRequestShapeKey  = "request_shape"
)

// ProviderErrorDetails returns safe structured provider-error diagnostics from err.
func ProviderErrorDetails(err error) map[string]any {
	var statusErr *provider.StatusError
	if errors.As(err, &statusErr) {
		return statusProviderErrorDetails(statusErr)
	}

	return oopsProviderErrorDetails(err)
}

func statusProviderErrorDetails(statusErr *provider.StatusError) map[string]any {
	details := map[string]any{
		ProviderStatusKey: statusErr.Status,
	}
	if statusErr.Details != nil {
		copyString(details, ProviderCodeKey, statusErr.Details.Code)
		copyString(details, ProviderParamKey, statusErr.Details.Param)
		copyString(details, ProviderTypeKey, statusErr.Details.Type)
		copyString(details, ProviderBodyPreviewKey, statusErr.Details.BodyPreview)

		if statusErr.Details.BodyTruncated {
			details[ProviderBodyTruncatedKey] = true
		}
	}

	if shape := statusErr.RequestShape.Payload(); len(shape) > 0 {
		details[ProviderRequestShapeKey] = shape
	}

	return details
}

func oopsProviderErrorDetails(err error) map[string]any {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return map[string]any{}
	}

	context := oopsErr.Context()
	details := make(map[string]any, len(context))
	copyIfPresent(details, context, ProviderStatusKey)
	copyIfPresent(details, context, ProviderCodeKey)
	copyIfPresent(details, context, ProviderParamKey)
	copyIfPresent(details, context, ProviderTypeKey)
	copyIfPresent(details, context, ProviderBodyPreviewKey)
	copyIfPresent(details, context, ProviderBodyTruncatedKey)
	copyIfPresent(details, context, ProviderRequestShapeKey)

	return details
}

func copyIfPresent(dst, src map[string]any, key string) {
	if value, ok := src[key]; ok {
		dst[key] = value
	}
}

func copyString(dst map[string]any, key, value string) {
	if value != "" {
		dst[key] = value
	}
}
