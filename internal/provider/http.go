package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/limitio"
	"github.com/omarluq/librecode/internal/units"
)

const providerResponseLimitBytes int64 = 16 * units.MiB

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

		return nil, providerStatusError(response.StatusCode, content)
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
	headers["chatgpt-account-id"] = request.Request.Auth.Headers["chatgpt-account-id"]
	headers["originator"] = "librecode"
	headers["User-Agent"] = "librecode"
	headers["OpenAI-Beta"] = "responses=experimental"
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
