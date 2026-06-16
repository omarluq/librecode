package limitio

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitError(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")
	err := limitError(cause, "read input")

	require.Error(t, err)
	require.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "read input")
}

func TestLimitErrorIgnoresNil(t *testing.T) {
	t.Parallel()

	assert.NoError(t, limitError(nil, "read input"))
}
