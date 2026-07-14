package agent_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func TestLoadIncludesBuiltInAgents(t *testing.T) {
	t.Parallel()

	const generalAgentName = "general"

	tests := []struct {
		name        string
		lookup      string
		wantName    string
		permissions agent.PermissionMode
		wantTools   []tool.Name
	}{
		{
			name:        "explore",
			lookup:      " EXPLORE ",
			wantName:    "explore",
			wantTools:   []tool.Name{tool.NameRead, tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST},
			permissions: agent.PermissionDeny,
		},
		{
			name:     generalAgentName,
			lookup:   generalAgentName,
			wantName: generalAgentName,
			wantTools: []tool.Name{
				tool.NameRead, tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST,
				tool.NameFetch, tool.NameBash, tool.NameEdit, tool.NameWrite,
			},
			permissions: agent.PermissionAllow,
		},
	}

	catalog := agent.Load(t.TempDir())
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			definition, found := catalog.Get(testCase.lookup)

			require.True(t, found)
			assert.Equal(t, testCase.wantName, definition.Name)
			assert.Equal(t, testCase.wantTools, definition.Tools)
			assert.Equal(t, testCase.permissions, definition.Permissions)
		})
	}

	assert.Empty(t, catalog.Diagnostics())
}

func TestLoadProjectDefinitionOverridesBuiltIn(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("LIBRECODE_HOME", t.TempDir())
	writeDefinition(t, filepath.Join(cwd, core.ConfigDirName, "agents", "explore.md"), `---
name: explore
description: Project explorer
tools: [read]
---
Project-specific instructions.
`)

	catalog := agent.Load(cwd)
	definition, found := catalog.Get("explore")

	require.True(t, found)
	assert.Equal(t, "Project explorer", definition.Description)
	assert.Equal(t, []tool.Name{tool.NameRead}, definition.Tools)
	require.Len(t, catalog.Diagnostics(), 1)
	assert.Contains(t, catalog.Diagnostics()[0].Message, "shadowed")
}

func TestLoadRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeDefinition(t, filepath.Join(cwd, core.ConfigDirName, "agents", "invalid.md"), `---
name: invalid
description: Invalid agent
tools: [missing]
---
Instructions.
`)

	catalog := agent.Load(cwd)
	_, found := catalog.Get("invalid")

	assert.False(t, found)
	require.Len(t, catalog.Diagnostics(), 1)
	assert.Contains(t, catalog.Diagnostics()[0].Message, "unknown tool")
}

func TestLoadParsesFullExecutionProfile(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeDefinition(t, filepath.Join(cwd, core.ConfigDirName, "agents", "general.md"), `---
name: general
description: General agent
tools: [read, write, bash]
provider: openai
model: gpt-test
thinking: high
timeout: 5m
permissions: deny
---
Complete the task.
`)

	definition, found := agent.Load(cwd).Get("general")

	require.True(t, found)
	assert.Equal(t, []tool.Name{tool.NameRead, tool.NameWrite, tool.NameBash}, definition.Tools)
	assert.Equal(t, "openai", definition.Model.Provider)
	assert.Equal(t, "gpt-test", definition.Model.Model)
	assert.Equal(t, model.ThinkingHigh, definition.Model.Thinking)
	assert.Equal(t, 5*time.Minute, definition.Limits.Timeout)
	assert.Equal(t, agent.PermissionDeny, definition.Permissions)
}

func writeDefinition(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
