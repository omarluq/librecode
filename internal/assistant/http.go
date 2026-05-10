package assistant

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/limitio"
)

const providerResponseLimitBytes int64 = 16 << 20

func (client *HTTPCompletionClient) postJSON(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
) ([]byte, error) {
	request, err := jsonRequest(ctx, endpoint, headers, payload)
	if err != nil {
		return nil, err
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, oops.In("assistant").Code("provider_http").Wrapf(err, "request provider response")
	}
	defer closeBody(response.Body)
	content, err := readProviderBody(response.Body)
	if err != nil {
		return nil, oops.In("assistant").Code("provider_read").Wrapf(err, "read provider response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providerStatusError("provider_status", response.StatusCode, content)
	}

	return content, nil
}

func readProviderBody(reader io.Reader) ([]byte, error) {
	return limitio.ReadAll(reader, providerResponseLimitBytes, "provider response")
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
		return nil, oops.In("assistant").Code("provider_payload").Wrapf(err, "encode provider payload")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, oops.In("assistant").Code("provider_request").Wrapf(err, "create provider request")
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	return request, nil
}

func openAIHeaders(request *CompletionRequest) map[string]string {
	headers := cloneHeaders(request.Auth.Headers)
	headers["Authorization"] = "Bearer " + request.Auth.APIKey

	return headers
}

func codexHeaders(request *CompletionRequest) map[string]string {
	headers := openAIHeaders(request)
	accountID := request.Auth.Headers["chatgpt-account-id"]
	if accountID == "" {
		accountID = accountIDFromToken(request.Auth.APIKey)
	}
	headers["chatgpt-account-id"] = accountID
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

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers)+2)
	for key, value := range headers {
		cloned[key] = value
	}

	return cloned
}

func accountIDFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	decoded, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return ""
	}
	authClaims, ok := payload["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	accountID, ok := authClaims["chatgpt_account_id"].(string)
	if !ok {
		return ""
	}

	return accountID
}

func base64URLDecode(value string) ([]byte, error) {
	return base64RawURLDecode(value)
}

var base64RawURLDecode = func(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}

func minPositive(value, fallback int) int {
	if value > 0 && value < fallback {
		return value
	}

	return fallback
}
