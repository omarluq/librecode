package di

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewEventServiceExposesBus(t *testing.T) {
	t.Parallel()

	container, err := NewContainer("", ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		report := container.ShutdownWithContext(ctx)
		require.True(t, report.Succeed, report.Error())
	})

	service := container.EventService()

	require.True(t, service != nil && service.Bus != nil)
}
