//go:build linux

package browser_test

import (
	"testing"

	"github.com/omarluq/librecode/internal/browser"
	"github.com/stretchr/testify/require"
)

func TestOpenWrapsPkgBrowserError(t *testing.T) {
	t.Parallel()

	err := browser.Open("")
	require.Error(t, err)
	require.ErrorContains(t, err, "open browser")
}
