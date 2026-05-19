package di_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
)

func TestNewEventServiceExposesBus(t *testing.T) {
	t.Parallel()

	container, err := di.NewContainer("", di.ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		require.True(t, report.Succeed, report.Error())
	})

	service := di.MustInvoke[*di.EventService](container)

	require.True(t, di.EventBusAvailableForTest(service))
}
