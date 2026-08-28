package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/limitio"
	"github.com/omarluq/librecode/internal/units"
)

const (
	// MaxRetryAfter bounds provider-directed waits to avoid unbounded or overflowing delays.
	MaxRetryAfter = 5 * time.Minute

	providerResponseLimitBytes int64 = 16 * units.MiB
	codexAccountIDHeader             = "chatgpt-account-id"
	codexOriginatorHeader            = "originator"
	codexUserAgentHeader             = "User-Agent"
	codexBetaHeader                  = "OpenAI-Beta"
	codexClientHeaderValue           = "librecode"
	codexResponsesBetaValue          = "responses=experimental"
)

func (client *HTTPCompletionClient) requestProviderStream(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
	parse func(io.Reader) (*providerResult, error),
) (*providerResult, error) {
	response, err := client.doProviderRequest(ctx, endpoint, headers, payload)
	if err != nil {
		return nil, err
	}
	defer closeBody(response.Body)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		content, readErr := readProviderBody(response.Body)
		if readErr != nil {
			return nil, oops.In("provider").Code("provider_error_read").Wrapf(readErr, "read provider error")
		}

		return nil, providerStatusErrorWithRetryAfter(
			response.StatusCode,
			content,
			providerRequestShape(payload),
			providerRetryAfter(response.Header, time.Now()),
			providerRequestID(response.Header),
		)
	}

	return parse(response.Body)
}

func (client *HTTPCompletionClient) doProviderRequest(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
) (*http.Response, error) {
	request, err := jsonRequest(ctx, endpoint, headers, payload)
	if err != nil {
		return nil, err
	}

	response, err := client.client.Do(request)
	if err != nil {
		return nil, oops.In("provider").Code("provider_http").Wrapf(err, "request provider response")
	}

	return response, nil
}

func readProviderBody(reader io.Reader) ([]byte, error) {
	body, err := limitio.ReadAll(reader, providerResponseLimitBytes, "provider response")

	return body, providerWrap(err, "read provider response")
}

func providerRequestID(header http.Header) string {
	return firstNonEmptyString(header.Get("X-Request-ID"), header.Get("Request-Id"))
}

// providerRetryAfter returns a bounded provider-requested delay.
func providerRetryAfter(header http.Header, now time.Time) time.Duration {
	millisecondsValue := strings.TrimSpace(header.Get("Retry-After-Ms"))
	if milliseconds, err := strconv.ParseInt(millisecondsValue, 10, 64); err == nil && milliseconds > 0 {
		return boundedRetryAfter(milliseconds, time.Millisecond)
	}

	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return boundedRetryAfter(seconds, time.Second)
	}

	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return min(date.Sub(now), MaxRetryAfter)
	}

	return 0
}

func boundedRetryAfter(value int64, unit time.Duration) time.Duration {
	maximum := int64(MaxRetryAfter / unit)
	if value >= maximum {
		return MaxRetryAfter
	}

	return time.Duration(value) * unit
}

func closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		return
	}
}

func jsonRequest(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.In("provider").Code("provider_payload").Wrapf(err, "encode provider payload")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, oops.In("provider").Code("provider_request").Wrapf(err, "create provider request")
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	return request, nil
}

func openAIHeaders(request *CompletionRequest) map[string]string {
	headers := cloneHeaders(request.Request.Auth.Headers)
	headers["Authorization"] = "Bearer " + request.Request.Auth.APIKey

	return headers
}

func codexHeaders(request *CompletionRequest) map[string]string {
	headers := openAIHeaders(request)
	if accountID := request.Request.Auth.Headers[codexAccountIDHeader]; accountID != "" {
		headers[codexAccountIDHeader] = accountID
	}

	headers[codexOriginatorHeader] = codexClientHeaderValue
	headers[codexUserAgentHeader] = codexClientHeaderValue
	headers[codexBetaHeader] = codexResponsesBetaValue
	headers["Accept"] = "text/event-stream"

	return headers
}

func joinEndpoint(baseURL, suffix string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + suffix
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix

	return parsed.String()
}

const providerHeaderExtraCapacity = 2

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers)+providerHeaderExtraCapacity)
	maps.Copy(cloned, headers)

	return cloned
}

func minPositive(value, fallback int) int {
	if value > 0 && value < fallback {
		return value
	}

	return fallback
}
