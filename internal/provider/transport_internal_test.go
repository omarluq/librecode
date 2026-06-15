package provider

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPCompletionClientTunedTransport(t *testing.T) {
	t.Parallel()

	client := NewHTTPCompletionClient()
	require.NotNil(t, client.client.Transport)

	transport, ok := client.client.Transport.(*http.Transport)
	require.True(t, ok)

	assert.Equal(t, providerMaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, providerMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	assert.Equal(t, providerIdleConnTimeout, transport.IdleConnTimeout)
	assert.Equal(t, providerTLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	assert.Equal(t, providerResponseHeaderTimeout, transport.ResponseHeaderTimeout)
	assert.True(t, transport.ForceAttemptHTTP2)
}
