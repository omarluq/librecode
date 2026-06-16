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

func TestOpenReturnsErrNoOpenerWhenCommandsAreMissing(t *testing.T) {
	t.Parallel()

	err := openWithCommands("https://example.com", []openerCommand{{name: "definitely-not-a-browser", args: nil}})
	require.ErrorIs(t, err, ErrNoOpener)
}

func TestRunOpenerReturnsErrorForMissingCommand(t *testing.T) {
	t.Parallel()

	err := runOpener("definitely-not-a-browser")
	require.Error(t, err)
}

func TestOpenerCommands(t *testing.T) {
	t.Setenv("BROWSER", "custom-browser")

	commands := openerCommands()
	require.NotEmpty(t, commands)
	assert.Equal(t, "custom-browser", commands[0].name)

	t.Setenv("BROWSER", "")
	assert.Equal(t, platformOpeners(), openerCommands())
}

func TestPlatformOpenersReturnsCommands(t *testing.T) {
	t.Parallel()

	commands := platformOpeners()
	require.NotEmpty(t, commands)

	for _, command := range commands {
		assert.NotEmpty(t, command.name)
	}
}
