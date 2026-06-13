package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenIgnoresEmptyURL(t *testing.T) {
	t.Parallel()

	require.NoError(t, Open(""))
}

func TestRunOpenerReturnsErrorForMissingCommand(t *testing.T) {
	t.Parallel()

	err := runOpener("definitely-not-a-browser")
	require.Error(t, err)
}

func TestOpenerCommandsPrefersConfiguredBrowser(t *testing.T) {
	t.Setenv("BROWSER", "custom-browser")

	commands := openerCommands()
	require.NotEmpty(t, commands)
	assert.Equal(t, "custom-browser", commands[0].name)
}

func TestPlatformOpenersReturnsCommands(t *testing.T) {
	t.Parallel()

	commands := platformOpeners()
	require.NotEmpty(t, commands)

	for _, command := range commands {
		assert.NotEmpty(t, command.name)
	}
}
