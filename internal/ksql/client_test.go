package ksql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/ksql"
)

func TestClient_Info(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "/info", request.URL.Path)
		_, err := writer.Write([]byte(`{"ok":true}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	client := ksql.NewClient(server.URL, time.Second)
	body, err := client.Info(context.Background())
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(body))
}

func TestClient_Execute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/ksql", request.URL.Path)

		var payload ksql.Request
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		assert.Equal(t, "SHOW STREAMS;", payload.KSQL)
		assert.Empty(t, payload.StreamsProperties)

		_, err := writer.Write([]byte(`[]`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	client := ksql.NewClient(server.URL, time.Second)
	body, err := client.Execute(context.Background(), "SHOW STREAMS;")
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(body))
}
