package extension_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	testResolverGitHubSource  = "github:owner/repo"
	testResolverVimModeSource = "official:vim-mode"
)

func TestResolveConfiguredSourcesAppliesConfiguredVersionOverLock(t *testing.T) {
	t.Parallel()

	lockFile := extension.LockFile{Extensions: map[string]extension.LockedExtension{
		testResolverGitHubSource: {Resolved: "", Version: "v1.0.0"},
	}}

	resolved, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: testResolverGitHubSource, Version: "v2.0.0"},
	}, lockFile, t.TempDir())

	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "v2.0.0", resolved[0].Lock.Version)
}

func TestResolveConfiguredSourcesUsesGitHubStorePath(t *testing.T) {
	t.Parallel()

	installRoot := filepath.Join(t.TempDir(), "store")

	resolved, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: "github:owner/repo//extensions/demo", Version: ""},
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, installRoot)

	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, filepath.Join(installRoot, "github", "owner", "repo", "extensions", "demo"), resolved[0].LoadPath)
	assert.Equal(t, "missing", resolved[0].Status)
	assert.Equal(t, "owner/repo//extensions/demo", resolved[0].Name)
}

func TestResolveConfiguredSourcesRejectsUnknownOfficialExtension(t *testing.T) {
	t.Parallel()

	_, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: "official:not-real", Version: ""},
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, t.TempDir())

	assert.ErrorContains(t, err, "unknown official extension")
}

func TestResolveConfiguredSourcesUsesOfficialDefaults(t *testing.T) {
	t.Parallel()

	installRoot := filepath.Join(t.TempDir(), "store")

	resolved, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: testResolverVimModeSource, Version: ""},
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, installRoot)

	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "vim-mode", resolved[0].Name)
	assert.Equal(t, "github:omarluq/librecode//extensions/vim-mode", resolved[0].Lock.Resolved)
	assert.Equal(t, "v0.1.0", resolved[0].Lock.Version)
	assert.Equal(t, filepath.Join(installRoot, "official", "vim-mode"), resolved[0].LoadPath)
	assert.Equal(t, "missing", resolved[0].Status)
}

func TestResolveConfiguredSourcesUsesLockedOfficialValues(t *testing.T) {
	t.Parallel()

	lockFile := extension.LockFile{Extensions: map[string]extension.LockedExtension{
		testResolverVimModeSource: {Resolved: "github:fork/librecode//extensions/vim-mode", Version: "v9.9.9"},
	}}

	resolved, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: testResolverVimModeSource, Version: ""},
	}, lockFile, t.TempDir())

	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "github:fork/librecode//extensions/vim-mode", resolved[0].Lock.Resolved)
	assert.Equal(t, "v9.9.9", resolved[0].Lock.Version)
}

func TestResolveConfiguredSourcesUsesPathSource(t *testing.T) {
	t.Parallel()

	resolved, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: "path:.librecode/extensions", Version: ""},
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, t.TempDir())

	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, ".librecode/extensions", resolved[0].LoadPath)
	assert.Equal(t, "path", resolved[0].Status)
}

func TestResolveConfiguredSourcesRejectsInvalidConfiguredSource(t *testing.T) {
	t.Parallel()

	_, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: "github:owner", Version: ""},
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, t.TempDir())

	assert.ErrorContains(t, err, "github source")
}
