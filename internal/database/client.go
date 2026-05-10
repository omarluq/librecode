package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/limitio"
)

const (
	// KSQLProjectURL documents the upstream ksqlDB project this adapter targets.
	KSQLProjectURL = "https://github.com/confluentinc/ksql"

	ksqlResponseLimitBytes int64 = 8 << 20
)

// KSQLClient is a small ksqlDB REST client.
type KSQLClient struct {
	httpClient *http.Client
	endpoint   string
}

// NewKSQLClient creates a ksqlDB REST client. Empty endpoints create a disabled client.
func NewKSQLClient(endpoint string, timeout time.Duration) *KSQLClient {
	return &KSQLClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		endpoint: strings.TrimRight(endpoint, "/"),
	}
}

// Enabled reports whether a ksqlDB endpoint is configured.
func (client *KSQLClient) Enabled() bool {
	return client.endpoint != ""
}

// Info calls the ksqlDB information endpoint.
func (client *KSQLClient) Info(ctx context.Context) ([]byte, error) {
	if !client.Enabled() {
		return nil, fmt.Errorf("ksql: endpoint is not configured")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint+"/info", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("ksql: create info request: %w", err)
	}

	return client.do(request)
}

// Execute posts a ksqlDB statement to the REST API.
func (client *KSQLClient) Execute(ctx context.Context, statement string) ([]byte, error) {
	if !client.Enabled() {
		return nil, fmt.Errorf("ksql: endpoint is not configured")
	}

	payload := KSQLRequestEntity{
		StreamsProperties: map[string]string{},
		KSQL:              statement,
	}
	if err := validateKSQLRequestEntity(&payload); err != nil {
		return nil, fmt.Errorf("ksql: validate request: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ksql: encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+"/ksql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ksql: create statement request: %w", err)
	}
	request.Header.Set("Content-Type", "application/vnd.ksql.v1+json; charset=utf-8")

	return client.do(request)
}

func (client *KSQLClient) do(request *http.Request) (body []byte, err error) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ksql: request failed: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("ksql: close response body: %w", closeErr)
		}
	}()

	body, err = limitio.ReadAll(response.Body, ksqlResponseLimitBytes, "ksql response")
	if err != nil {
		return nil, fmt.Errorf("ksql: read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ksql: unexpected status %d: %s", response.StatusCode, string(body))
	}

	return body, nil
}
