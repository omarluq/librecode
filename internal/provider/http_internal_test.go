package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadProviderBodyRejectsBodiesAboveLimit(t *testing.T) {
	t.Parallel()

	content, err := readProviderBody(strings.NewReader(strings.Repeat("a", int(providerResponseLimitBytes)+1)))

	require.Error(t, err)
	assert.Nil(t, content)
	assert.Contains(t, err.Error(), "provider response exceeds limit")
}
