package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/tool"
)

func TestCatalogRejectsInvalidProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		meta    string
		message string
	}{
		{name: "missing fields", meta: "name: broken", message: "required"},
		{
			name: "provider without model", meta: "name: broken\ndescription: bad\nprovider: openai",
			message: "set together",
		},
		{
			name: "invalid thinking", meta: "name: broken\ndescription: bad\nthinking: enormous",
			message: "invalid thinking",
		},
		{
			name: "invalid timeout", meta: "name: broken\ndescription: bad\ntimeout: -1s",
			message: "positive duration",
		},
		{
			name: "invalid permissions", meta: "name: broken\ndescription: bad\npermissions: maybe",
			message: "invalid permissions",
		},
		{name: "malformed frontmatter", meta: "name: [", message: "parse frontmatter"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			cwd := t.TempDir()
			path := filepath.Join(cwd, core.ConfigDirName, "agents", "broken.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte("---\n"+testCase.meta+"\n---\nprompt"), 0o600))

			catalog := agent.Load(cwd)
			_, found := catalog.Get("broken")
			assert.False(t, found)
			require.NotEmpty(t, catalog.Diagnostics())
			assert.Contains(t, catalog.Diagnostics()[0].Message, testCase.message)
		})
	}
}

func TestCatalogResultsAreSortedAndDefensive(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	for _, name := range []string{"zeta", "alpha"} {
		writeDefinition(
			t,
			filepath.Join(cwd, core.ConfigDirName, "agents", name+".md"),
			"---\nname: "+name+"\ndescription: test\ntools: [read, read, ' ']\n---\nprompt",
		)
	}

	writeDefinition(
		t,
		filepath.Join(cwd, core.ConfigDirName, "agents", "broken.md"),
		"---\nname: broken\n---\nprompt",
	)

	catalog := agent.Load(cwd)
	definitions := catalog.Definitions()
	require.GreaterOrEqual(t, len(definitions), 2)
	assert.Equal(t, "alpha", definitions[0].Name)

	alpha, found := catalog.Get(" ALPHA ")
	require.True(t, found)
	assert.Equal(t, []tool.Name{tool.NameRead}, alpha.Tools)
	alpha.Tools[0] = tool.NameWrite
	again, _ := catalog.Get("alpha")
	assert.Equal(t, tool.NameRead, again.Tools[0])

	diagnostics := catalog.Diagnostics()
	require.NotEmpty(t, diagnostics)
	originalMessage := diagnostics[0].Message
	diagnostics[0].Message = "mutation"
	assert.Equal(t, originalMessage, catalog.Diagnostics()[0].Message)
}

func TestCatalogReportsDiscoveryErrors(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("LIBRECODE_HOME", t.TempDir())

	agentsPath := filepath.Join(cwd, core.ConfigDirName, "agents")
	require.NoError(t, os.MkdirAll(filepath.Dir(agentsPath), 0o700))
	require.NoError(t, os.WriteFile(agentsPath, []byte("not a directory"), 0o600))

	diagnostics := agent.Load(cwd).Diagnostics()
	require.Len(t, diagnostics, 1)
	assert.Equal(t, ".", diagnostics[0].Path)
	assert.NotEmpty(t, diagnostics[0].Message)
}

func TestNilCatalogAccessors(t *testing.T) {
	t.Parallel()

	var catalog *agent.Catalog

	definition, found := catalog.Get("anything")
	assert.False(t, found)
	assert.Equal(t, agent.PermissionInherit, definition.Permissions)
	assert.Nil(t, catalog.Definitions())
	assert.Nil(t, catalog.Diagnostics())
}
