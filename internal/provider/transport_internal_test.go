package provider

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPCompletionClientTunedTransport(t *testing.T) {
	t.Parallel()

	client := NewHTTPCompletionClient()
	require.NotNil(t, client.client.Transport)

	h2Transport, ok := client.client.Transport.(h2OnlyTransport)
	require.True(t, ok)
	transport, ok := h2Transport.base.(*http.Transport)
	require.True(t, ok)

	assert.Equal(t, providerMaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, providerMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	assert.Equal(t, providerIdleConnTimeout, transport.IdleConnTimeout)
	assert.Equal(t, providerTLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	assert.Zero(t, transport.ResponseHeaderTimeout)
	assert.True(t, transport.ForceAttemptHTTP2)
	require.NotNil(t, transport.TLSClientConfig)
	assert.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MinVersion)
	assert.Equal(t, []string{"h2"}, transport.TLSClientConfig.NextProtos)
}

func TestH2OnlyTransportRejectsHTTP1Responses(t *testing.T) {
	t.Parallel()

	transport := h2OnlyTransport{base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test", http.NoBody)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	if response != nil {
		defer closeBody(response.Body)
	}

	require.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "HTTP/2 is required")
}

func TestH2OnlyTransportAcceptsHTTP2Responses(t *testing.T) {
	t.Parallel()

	transport := h2OnlyTransport{base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Proto:      "HTTP/2.0",
			ProtoMajor: http2ProtoMajor,
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test", http.NoBody)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	if response != nil {
		defer closeBody(response.Body)
	}

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, http2ProtoMajor, response.ProtoMajor)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
