package assistant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClientCompleteWrapsProviderErrors(t *testing.T) {
	t.Parallel()

	client := NewHTTPClient()

	result, err := client.Complete(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "complete provider request")
}
