package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestPromptTemplateArgumentSubstitution(t *testing.T) {
	t.Parallel()

	const (
		firstArg  = "one"
		secondArg = "two words"
		thirdArg  = "three words"
	)

	args := core.ParseCommandArgs(`one "two words" 'three words'`)
	assert.Equal(t, []string{firstArg, secondArg, thirdArg}, args)

	expanded := core.SubstituteArgs("$1|$2|$3|${@:2}|${@:2:1}|$ARGUMENTS|$@", args)
	expectedExpanded := strings.Join([]string{
		firstArg,
		secondArg,
		thirdArg,
		secondArg + " " + thirdArg,
		secondArg,
		firstArg + " " + secondArg + " " + thirdArg,
		firstArg + " " + secondArg + " " + thirdArg,
	}, "|")
	assert.Equal(t, expectedExpanded, expanded)
}

func TestLoadPromptTemplatesLoadsMarkdownAndReportsCollisions(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	agentDir := t.TempDir()
	globalPrompts := filepath.Join(agentDir, "prompts")
	projectPrompts := filepath.Join(cwd, core.ConfigDirName, "prompts")
	writeTestFile(t, filepath.Join(globalPrompts, "fix.md"), strings.Join([]string{
		frontmatterDelimiter,
		"description: Global fix",
		"argument-hint: BUG",
		frontmatterDelimiter,
		"fix $1",
	}, "\n"))
	writeTestFile(t, filepath.Join(projectPrompts, "fix.md"), "project fix")
	writeTestFile(t, filepath.Join(projectPrompts, "review.md"), "Review this code")

	loaded := core.LoadPromptTemplates(&core.LoadPromptTemplatesOptions{
		CWD:             cwd,
		AgentDir:        agentDir,
		PromptPaths:     nil,
		IncludeDefaults: true,
	})
	require.Empty(t, loaded.Diagnostics)
	require.Len(t, loaded.Prompts, 3)

	deduped := core.DedupePromptTemplates(loaded.Prompts)
	require.Len(t, deduped.Prompts, 2)
	require.Len(t, deduped.Diagnostics, 1)
	assert.Equal(t, "/fix", deduped.Diagnostics[0].Collision.Name)
	assert.Equal(t, "Global fix", deduped.Prompts[0].Description)
	assert.Equal(t, "BUG", deduped.Prompts[0].ArgumentHint)
	assert.Equal(t, "Review this code", deduped.Prompts[1].Description)

	assert.Equal(t, "fix bug-123", core.ExpandPromptTemplate("/fix bug-123", deduped.Prompts))
	assert.Equal(t, "/unknown value", core.ExpandPromptTemplate("/unknown value", deduped.Prompts))
}
