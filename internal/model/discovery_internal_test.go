package model

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	discoveryCatalogSourceURL = "https://models.invalid/api.json"
	discoveryNetworkDown      = "network down"
)

const discoveryCatalogFixture = `{
	"openai": {
		"models": {
			"gpt-5.2": {
				"name": "GPT-5.2",
				"tool_call": true,
				"reasoning": true,
				"modalities": {"input": ["text"]},
				"limit": {"context": 128000, "output": 32000}
			}
		}
	}
}`

func TestDiscoverModelsFetchesCatalog(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "application/json", request.Header.Get("Accept"))

		_, err := writer.Write([]byte(discoveryCatalogFixture))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	models, err := DiscoverModels(t.Context(), DiscoveryOptions{
		Client:       server.Client(),
		CachePath:    "",
		SourceURL:    server.URL,
		CacheTTL:     0,
		FetchTimeout: time.Second,
		Enabled:      true,
	})
	require.NoError(t, err)
	assert.Contains(t, discoveredModelIDs(models), "openai/gpt-5.2")
}

func TestDiscoverModelsValidationAndFetchErrors(t *testing.T) {
	t.Parallel()

	testCases := []discoveryErrorCase{
		{
			name: "disabled",
			options: DiscoveryOptions{
				Client:       nil,
				CachePath:    "",
				SourceURL:    "",
				CacheTTL:     0,
				FetchTimeout: 0,
				Enabled:      false,
			},
			error: "",
			empty: true,
		},
		{
			name: "missing source url",
			options: DiscoveryOptions{
				Client:       nil,
				CachePath:    "",
				SourceURL:    "",
				CacheTTL:     0,
				FetchTimeout: 0,
				Enabled:      true,
			},
			error: "model discovery source URL is required",
			empty: false,
		},
		{
			name: "invalid request url",
			options: DiscoveryOptions{
				Client:       nil,
				CachePath:    "",
				SourceURL:    string([]byte{0x7f}),
				CacheTTL:     0,
				FetchTimeout: 0,
				Enabled:      true,
			},
			error: "create model discovery request",
			empty: false,
		},
		{
			name: "transport error",
			options: DiscoveryOptions{
				Client:       newErrorHTTPClient("boom"),
				CachePath:    "",
				SourceURL:    discoveryCatalogSourceURL,
				CacheTTL:     0,
				FetchTimeout: 0,
				Enabled:      true,
			},
			error: "fetch model discovery catalog",
			empty: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			models, err := DiscoverModels(t.Context(), testCase.options)
			if testCase.error != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.error)

				return
			}

			require.NoError(t, err)

			if testCase.empty {
				assert.Empty(t, models)
			}
		})
	}
}

type discoveryErrorCase struct {
	error   string
	name    string
	options DiscoveryOptions
	empty   bool
}

func TestFetchDiscoveryCatalogErrors(t *testing.T) {
	t.Parallel()

	statusServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "nope", http.StatusTeapot)
	}))
	t.Cleanup(statusServer.Close)

	_, err := fetchDiscoveryCatalog(t.Context(), DiscoveryOptions{
		Client:       statusServer.Client(),
		CachePath:    "",
		SourceURL:    statusServer.URL,
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")

	_, err = fetchDiscoveryCatalog(t.Context(), DiscoveryOptions{
		Client:       newReadErrorHTTPClient(),
		CachePath:    "",
		SourceURL:    discoveryCatalogSourceURL,
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read model discovery catalog")
}

func TestDiscoveryCacheFreshFetchAndStaleFallback(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(cachePath, []byte(discoveryCatalogFixture), 0o600))

	models, err := DiscoverModelsCached(t.Context(), CachedDiscoveryOptions{
		Client:       newErrorHTTPClient("should not fetch"),
		CachePath:    cachePath,
		SourceURL:    discoveryCatalogSourceURL,
		CacheTTL:     time.Hour,
		FetchTimeout: 0,
		Enabled:      true,
	})
	require.NoError(t, err)
	assert.Contains(t, discoveredModelIDs(models), "openai/gpt-5.2")

	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(cachePath, oldTime, oldTime))
	models, err = DiscoverModelsCached(t.Context(), CachedDiscoveryOptions{
		Client:       newErrorHTTPClient(discoveryNetworkDown),
		CachePath:    cachePath,
		SourceURL:    discoveryCatalogSourceURL,
		CacheTTL:     time.Hour,
		FetchTimeout: 0,
		Enabled:      true,
	})
	require.NoError(t, err)
	assert.Contains(t, discoveredModelIDs(models), "openai/gpt-5.2")
}

func TestDiscoveryCacheFetchWritesCatalog(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(discoveryCatalogFixture))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	cachePath := filepath.Join(t.TempDir(), "nested", "models.json")

	models, err := DiscoverModelsCached(t.Context(), CachedDiscoveryOptions{
		Client:       server.Client(),
		CachePath:    cachePath,
		SourceURL:    server.URL,
		CacheTTL:     time.Hour,
		FetchTimeout: 0,
		Enabled:      true,
	})
	require.NoError(t, err)
	assert.Contains(t, discoveredModelIDs(models), "openai/gpt-5.2")

	content, err := os.ReadFile(filepath.Clean(cachePath))
	require.NoError(t, err)
	assert.JSONEq(t, discoveryCatalogFixture, string(content))
}

func TestDiscoveryCacheHelpers(t *testing.T) {
	t.Parallel()

	require.NoError(t, writeDiscoveryCache("", []byte("{}")))

	content, ok := readDiscoveryCache("")
	assert.False(t, ok)
	assert.Nil(t, content)
	content, ok = readFreshDiscoveryCache(filepath.Join(t.TempDir(), "missing.json"), time.Hour)
	assert.False(t, ok)
	assert.Nil(t, content)

	models, err := DiscoverModelsCached(t.Context(), CachedDiscoveryOptions{
		Client:       nil,
		CachePath:    "",
		SourceURL:    "",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	})
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestModelFromDiscoveryDefaults(t *testing.T) {
	t.Parallel()

	model := modelFromDiscovery("fallback-id", &discoveryModel{
		Modalities: nil,
		Limit:      nil,
		Cost:       nil,
		Name:       "",
		ID:         "",
		ToolCall:   true,
		Reasoning:  false,
	}, mapping(providerOpenRouter, "openrouter", apiOpenAICompletions, "https://openrouter.ai/api/v1"))

	assert.Equal(t, "fallback-id", model.ID)
	assert.Equal(t, "fallback-id", model.Name)
	assert.Equal(t, []InputMode{InputText}, model.Input)
	assert.Zero(t, model.ContextWindow)
	assert.Zero(t, model.MaxTokens)
	assert.Zero(t, model.Cost)
	assert.Nil(t, model.ThinkingLevelMap)
}

func TestOpenAIThinkingHelpers(t *testing.T) {
	t.Parallel()

	assert.True(t, openAISupportsXHigh(gpt52))
	assert.True(t, openAISupportsXHigh("gpt-5.3-codex"))
	assert.True(t, openAIResponsesNoReasoningModel("gpt-5.1"))
	assert.False(t, openAIResponsesNoReasoningModel("gpt-4.1"))
}

func discoveredModelIDs(models []Model) []string {
	ids := make([]string, 0, len(models))
	for index := range models {
		ids = append(ids, models[index].Provider+"/"+models[index].ID)
	}

	return ids
}

func newErrorHTTPClient(message string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(message)
	})}
}

func newReadErrorHTTPClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Body:       errorReadCloser{},
			Header:     http.Header{},
		}, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errorReadCloser) Close() error {
	return nil
}

var _ io.ReadCloser = errorReadCloser{}
