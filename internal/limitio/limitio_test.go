package limitio_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/limitio"
)

func TestReadAllBelowLimit(t *testing.T) {
	t.Parallel()

	content, err := limitio.ReadAll(strings.NewReader("hello"), 5, "test input")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), content)
}

func TestReadAllAboveLimit(t *testing.T) {
	t.Parallel()

	content, err := limitio.ReadAll(strings.NewReader("hello!"), 5, "test input")
	require.Error(t, err)
	assert.Nil(t, content)
	assert.Contains(t, err.Error(), "test input exceeds limit of 5 bytes")
}
