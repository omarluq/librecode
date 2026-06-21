//go:build !linux && !windows && !darwin && !openbsd && !freebsd && !netbsd

package browser_test

import (
	"testing"

	"github.com/omarluq/librecode/internal/browser"
	"github.com/stretchr/testify/require"
)

func TestOpenWrapsUnsupportedPlatformError(t *testing.T) {
	t.Parallel()

	err := browser.Open("https://example.com")
	require.Error(t, err)
	require.ErrorContains(t, err, "open browser")
}
