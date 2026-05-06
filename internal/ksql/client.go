// Package ksql integrates with ksqlDB through Confluent's REST API.
package ksql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ConfluentProjectURL documents the upstream ksqlDB project this adapter targets.
const ConfluentProjectURL = "https://github.com/confluentinc/ksql"

// Client is a small ksqlDB REST client.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// Request is a ksqlDB statement request.
type Request struct {
	KSQL              string            `json:"ksql"`
	StreamsProperties map[string]string `json:"streamsProperties"`
}

// NewClient creates a ksqlDB REST client. Empty endpoints create a disabled client.
func NewClient(endpoint string, timeout time.Duration) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Enabled reports whether an endpoint is configured.
func (client *Client) Enabled() bool {
	return client.endpoint != ""
}

// Info calls the ksqlDB /info endpoint.
func (client *Client) Info(ctx context.Context) ([]byte, error) {
	if !client.Enabled() {
		return nil, fmt.Errorf("ksql: endpoint is not configured")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint+"/info", nil)
	if err != nil {
		return nil, fmt.Errorf("ksql: create info request: %w", err)
	}

	return client.do(request)
}

// Execute posts a statement to /ksql.
func (client *Client) Execute(ctx context.Context, statement string) ([]byte, error) {
	if !client.Enabled() {
		return nil, fmt.Errorf("ksql: endpoint is not configured")
	}

	payload := Request{
		KSQL:              statement,
		StreamsProperties: map[string]string{},
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

func (client *Client) do(request *http.Request) ([]byte, error) {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ksql: request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("ksql: read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ksql: unexpected status %d: %s", response.StatusCode, string(body))
	}

	return body, nil
}
