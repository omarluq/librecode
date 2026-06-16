package di

import (
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/require"
)

func TestNewSkillsService(t *testing.T) {
	t.Parallel()

	service, err := NewSkillsService(do.New())
	require.NoError(t, err)
	require.NotNil(t, service.Cache)

	service.Shutdown()
}
