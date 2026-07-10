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

	xetag "github.com/charmbracelet/x/etag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	discoveryCatalogSourceURL   = "https://models.invalid/api.json"
	discoveryNetworkDown        = "network down"
	discoveryCacheWriteErrorMsg = "write model discovery cache"
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
		assert.Equal(t, "identity", request.Header.Get("Accept-Encoding"))

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

	testCases := []struct {
		name    string
		client  *http.Client
		server  func(t *testing.T) *httptest.Server
		source  string
		wantErr string
	}{
		{
			name:   "unexpected status",
			client: nil,
			server: func(t *testing.T) *httptest.Server {
				t.Helper()

				return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					http.Error(writer, "nope", http.StatusTeapot)
				}))
			},
			source:  "",
			wantErr: "unexpected status",
		},
		{
			name:    "read error",
			client:  newReadErrorHTTPClient(),
			server:  nil,
			source:  discoveryCatalogSourceURL,
			wantErr: "read model discovery catalog",
		},
		{
			name:   "not modified without cache",
			client: nil,
			server: func(t *testing.T) *httptest.Server {
				t.Helper()

				return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(http.StatusNotModified)
				}))
			},
			source:  "",
			wantErr: "unexpected not modified response",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := testCase.client
			source := testCase.source

			if testCase.server != nil {
				server := testCase.server(t)
				t.Cleanup(server.Close)
				client = server.Client()
				source = server.URL
			}

			_, err := fetchDiscoveryCatalog(t.Context(), DiscoveryOptions{
				Client:       client,
				CachePath:    "",
				SourceURL:    source,
				CacheTTL:     0,
				FetchTimeout: 0,
				Enabled:      true,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestDiscoverModelsUsesCachePath(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "models.json")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(discoveryCatalogFixture))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	models, err := DiscoverModels(t.Context(), DiscoveryOptions{
		Client:       server.Client(),
		CachePath:    cachePath,
		SourceURL:    server.URL,
		CacheTTL:     time.Hour,
		FetchTimeout: 0,
		Enabled:      true,
	})
	require.NoError(t, err)
	assert.Contains(t, discoveredModelIDs(models), "openai/gpt-5.2")
	assert.FileExists(t, cachePath)
}

func TestParseDiscoveredModelsErrors(t *testing.T) {
	t.Parallel()

	models, err := ParseDiscoveredModels([]byte(`{`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode model discovery catalog")
	assert.Empty(t, models)
}

func TestDiscoveryCacheNotModifiedErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		wantErr      string
		etag         string
		content      []byte
		hasContent   bool
		touchFailure bool
	}{
		{
			content:      nil,
			name:         "missing cache",
			etag:         "",
			wantErr:      "cache was not available",
			hasContent:   false,
			touchFailure: false,
		},
		{
			content:      []byte(`{`),
			name:         "invalid cached catalog",
			etag:         "",
			wantErr:      "decode model discovery catalog",
			hasContent:   true,
			touchFailure: false,
		},
		{
			content:      []byte(discoveryCatalogFixture),
			name:         "touch failure",
			etag:         "",
			wantErr:      "refresh model discovery cache",
			hasContent:   true,
			touchFailure: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cachePath := tempDiscoveryCachePath(t)
			if testCase.touchFailure {
				cachePath = filepath.Join(t.TempDir(), "missing", "models.json")
			}

			models, err := discoveredModelsFromNotModifiedCache(
				cachePath,
				testCase.content,
				testCase.hasContent,
				testCase.etag,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
			assert.Empty(t, models)
		})
	}
}

func TestDiscoveryCacheStaleFallbackReturnsFetchErrorWhenParseFails(t *testing.T) {
	t.Parallel()

	fetchErr := errors.New("fetch failed")
	models, err := staleDiscoveredModelsOrError([]byte(`{`), true, fetchErr)
	require.ErrorIs(t, err, fetchErr)
	assert.Empty(t, models)
}

func TestDiscoveryCacheNotModifiedWritesReturnedETag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		etag string
	}{
		{name: "writes returned etag", etag: "catalog-v2"},
		{name: "keeps existing etag when response has none", etag: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cachePath := filepath.Join(t.TempDir(), "models.json")
			require.NoError(t, writeDiscoveryCache(cachePath, []byte(discoveryCatalogFixture)))
			require.NoError(t, writeDiscoveryCacheETag(cachePath, "catalog-v1"))

			models, err := discoveredModelsFromNotModifiedCache(
				cachePath,
				[]byte(discoveryCatalogFixture),
				true,
				testCase.etag,
			)
			require.NoError(t, err)
			assert.Contains(t, discoveredModelIDs(models), "openai/gpt-5.2")

			cachedETag, etagExists := readDiscoveryCacheETag(cachePath)
			require.True(t, etagExists)

			if testCase.etag == "" {
				assert.Equal(t, "catalog-v1", cachedETag)

				return
			}

			assert.Equal(t, testCase.etag, cachedETag)
		})
	}
}

func TestDiscoveryCacheHelperEdgeCases(t *testing.T) {
	t.Parallel()

	assert.Empty(t, discoveryCachedETag("", false))
	assert.Empty(t, discoveryCachedETag(filepath.Join(t.TempDir(), "missing.json"), true))
	require.NoError(t, touchDiscoveryCache(""))
	require.Error(t, touchDiscoveryCache(filepath.Join(t.TempDir(), "missing", "models.json")))

	content, ok := readFreshDiscoveryCache("", time.Hour)
	assert.False(t, ok)
	assert.Nil(t, content)

	content, ok = readFreshDiscoveryCache(filepath.Join(t.TempDir(), "cache.json"), 0)
	assert.False(t, ok)
	assert.Nil(t, content)
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

	testCases := []struct {
		name       string
		serverETag string
		wantETag   string
	}{
		{name: "stores strong etag", serverETag: `"catalog-v2"`, wantETag: "catalog-v2"},
		{name: "removes stale etag without response etag", serverETag: "", wantETag: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if testCase.serverETag != "" {
					writer.Header().Set("ETag", testCase.serverETag)
				}

				_, err := writer.Write([]byte(discoveryCatalogFixture))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			cachePath := filepath.Join(t.TempDir(), "nested", "models.json")
			require.NoError(t, writeDiscoveryCacheETag(cachePath, "stale-etag"))

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

			cachedETag, etagExists := readDiscoveryCacheETag(cachePath)
			if testCase.wantETag == "" {
				assert.False(t, etagExists)
				assert.Empty(t, cachedETag)

				return
			}

			require.True(t, etagExists)
			assert.Equal(t, testCase.wantETag, cachedETag)
		})
	}
}

func TestDiscoveryCacheWriteErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		setup   func(t *testing.T) string
		name    string
		wantErr string
	}{
		{
			setup: func(t *testing.T) string {
				t.Helper()

				// Return a directory path where a file path is expected to force catalog writes to fail.
				return t.TempDir()
			},
			name:    "catalog write error",
			wantErr: discoveryCacheWriteErrorMsg,
		},
		{
			setup: func(t *testing.T) string {
				t.Helper()

				cachePath := filepath.Join(t.TempDir(), "models.json")
				makeNonEmptyDir(t, discoveryCacheETagPath(cachePath))

				return cachePath
			},
			name:    "etag write error",
			wantErr: discoveryCacheWriteErrorMsg,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("ETag", `"catalog-v2"`)
				_, err := writer.Write([]byte(discoveryCatalogFixture))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)

			models, err := DiscoverModelsCached(t.Context(), CachedDiscoveryOptions{
				Client:       server.Client(),
				CachePath:    testCase.setup(t),
				SourceURL:    server.URL,
				CacheTTL:     time.Hour,
				FetchTimeout: 0,
				Enabled:      true,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
			assert.Empty(t, models)
		})
	}
}

func TestDiscoveryCacheETagErrorPaths(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		run     func(t *testing.T) error
		name    string
		wantErr string
	}{
		{
			run: func(t *testing.T) error {
				t.Helper()

				cachePath := filepath.Join(t.TempDir(), "models.json")
				makeNonEmptyDir(t, discoveryCacheETagPath(cachePath))

				return writeDiscoveryCacheETag(cachePath, "")
			},
			name:    "remove stale etag fails",
			wantErr: "remove model discovery cache etag",
		},
		{
			run: func(t *testing.T) error {
				t.Helper()

				cachePath := filepath.Join(t.TempDir(), "models.json")
				require.NoError(t, writeDiscoveryCache(cachePath, []byte(discoveryCatalogFixture)))
				makeNonEmptyDir(t, discoveryCacheETagPath(cachePath))

				_, err := discoveredModelsFromNotModifiedCache(
					cachePath,
					[]byte(discoveryCatalogFixture),
					true,
					"catalog-v2",
				)

				return err
			},
			name:    "not modified etag write fails",
			wantErr: discoveryCacheWriteErrorMsg,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.run(t)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestDiscoveryCacheETagNotModifiedReusesStaleCatalog(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, writeDiscoveryCache(cachePath, []byte(discoveryCatalogFixture)))
	require.NoError(t, writeDiscoveryCacheETag(cachePath, "catalog-v1"))

	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(cachePath, oldTime, oldTime))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.True(t, xetag.Matches(request, "catalog-v1"))
		assert.Equal(t, "identity", request.Header.Get("Accept-Encoding"))
		writer.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(server.Close)

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

	stat, err := os.Stat(filepath.Clean(cachePath))
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), stat.ModTime(), time.Second)
}

func TestDiscoveryCacheETagHelpers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   string
		want string
	}{
		{name: "quoted", in: `"abc"`, want: "abc"},
		{name: "plain", in: "abc\n", want: "abc"},
		{name: "weak ignored", in: `W/"abc"`, want: ""},
		{name: "multi ignored", in: `"abc", "def"`, want: ""},
		{name: "empty", in: "  ", want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, normalizeDiscoveryETag(testCase.in))
		})
	}
}

func TestDiscoveryCacheHelpers(t *testing.T) {
	t.Parallel()

	require.NoError(t, writeDiscoveryCache("", []byte("{}")))
	require.NoError(t, writeDiscoveryCacheETag("", "etag"))

	content, cacheExists := readDiscoveryCache("")
	assert.False(t, cacheExists)
	assert.Nil(t, content)
	content, cacheExists = readFreshDiscoveryCache(filepath.Join(t.TempDir(), "missing.json"), time.Hour)
	assert.False(t, cacheExists)
	assert.Nil(t, content)
	etag, etagExists := readDiscoveryCacheETag(filepath.Join(t.TempDir(), "missing.json"))
	assert.False(t, etagExists)
	assert.Empty(t, etag)

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

func TestGPT56CapabilityMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{name: "sol", modelID: gpt56Sol, want: true},
		{name: "terra", modelID: gpt56Terra, want: true},
		{name: "luna", modelID: gpt56Luna, want: true},
		{name: "bare alias", modelID: gpt56, want: false},
		{name: "unknown variant", modelID: "gpt-5.6-custom", want: false},
		{name: "embedded name", modelID: "vendor/gpt-5.6-sol", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, openAISupportsMax(testCase.modelID))
		})
	}
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

	tests := []struct {
		assertFn func(string) bool
		name     string
		modelID  string
		want     bool
	}{
		{name: "gpt 5.2 supports xhigh", modelID: gpt52, assertFn: openAISupportsXHigh, want: true},
		{name: "gpt 5.3 codex supports xhigh", modelID: "gpt-5.3-codex", assertFn: openAISupportsXHigh, want: true},
		{name: "gpt 5.6 supports xhigh", modelID: gpt56Sol, assertFn: openAISupportsXHigh, want: true},
		{name: "gpt 5.6 supports max", modelID: gpt56Sol, assertFn: openAISupportsMax, want: true},
		{
			name:     "gpt 5.1 maps off to none",
			modelID:  "gpt-5.1",
			assertFn: openAIResponsesNoReasoningModel,
			want:     true,
		},
		{
			name:     "gpt 5.6 terra maps off to none",
			modelID:  gpt56Terra,
			assertFn: openAIResponsesNoReasoningModel,
			want:     true,
		},
		{
			name:     "gpt 4.1 does not map off to none",
			modelID:  "gpt-4.1",
			assertFn: openAIResponsesNoReasoningModel,
			want:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.assertFn(testCase.modelID))
		})
	}

	levels := map[ThinkingLevel]*string{}
	addAnthropicThinkingLevels(levels, providerAnthropic, "claude-basic")
	assert.Contains(t, levels, ThinkingOff)
	assert.NotContains(t, levels, ThinkingXHigh)
}

func TestCloseResponseBodyIgnoresCloseErrors(t *testing.T) {
	t.Parallel()

	closeResponseBody(errorReadCloser{})
	closeResponseBody(errorCloseReadCloser{})
}

func tempDiscoveryCachePath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "models.json")
}

func makeNonEmptyDir(t *testing.T, path string) {
	t.Helper()

	// Create a non-empty directory to force write/remove operations to fail.
	require.NoError(t, os.MkdirAll(filepath.Clean(path), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "entry"), []byte("x"), 0o600))
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

type errorCloseReadCloser struct{}

func (errorCloseReadCloser) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (errorCloseReadCloser) Close() error {
	return errors.New("close failed")
}

var (
	_ io.ReadCloser = errorReadCloser{}
	_ io.ReadCloser = errorCloseReadCloser{}
)
