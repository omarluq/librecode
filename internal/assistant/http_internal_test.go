package assistant

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostJSONRejectsProviderResponseAboveLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(strings.Repeat("a", int(providerResponseLimitBytes)+1)))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	client := &HTTPCompletionClient{client: server.Client()}
	content, err := client.postJSON(t.Context(), server.URL, nil, map[string]any{"ok": true})
	require.Error(t, err)
	assert.Nil(t, content)
	assert.Contains(t, err.Error(), "provider response exceeds limit")
}

func TestPostJSONRejectsProviderErrorBodyAboveLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, err := writer.Write([]byte(strings.Repeat("a", int(providerResponseLimitBytes)+1)))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	client := &HTTPCompletionClient{client: server.Client()}
	content, err := client.postJSON(t.Context(), server.URL, nil, map[string]any{"ok": true})
	require.Error(t, err)
	assert.Nil(t, content)
	assert.Contains(t, err.Error(), "provider response exceeds limit")
}
