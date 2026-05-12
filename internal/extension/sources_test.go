package extension_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	testGitHubSubdirSource = "github:owner/repo//extensions/ext"
	testPathSource         = "path:.librecode/extensions"
	testVimModeSource      = "official:vim-mode"
)

func TestParseSourceRefValidatesSupportedSources(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		source  string
		version string
		key     string
	}{
		{name: "official", source: testVimModeSource, version: "", key: testVimModeSource},
		{name: "github", source: "github:owner/repo", version: "v1.0.0", key: "github:owner/repo"},
		{name: "github subdir", source: testGitHubSubdirSource, version: "", key: testGitHubSubdirSource},
		{name: "path", source: testPathSource, version: "", key: testPathSource},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ref, err := extension.ParseSourceRef(testCase.source, testCase.version)

			require.NoError(t, err)
			assert.Equal(t, testCase.key, ref.Key())
			assert.Equal(t, testCase.version, ref.Version)
		})
	}
}

func TestParseSourceRefRejectsInvalidSources(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"",
		"vim-mode",
		"npm:thing",
		"official:owner/name",
		"github:owner",
		"github:/repo",
		"github:owner/repo//../bad",
	}

	for _, source := range testCases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			_, err := extension.ParseSourceRef(source, "")

			assert.Error(t, err)
		})
	}
}

func TestResolveConfiguredSourcesResolvesPathAndOfficialSources(t *testing.T) {
	t.Parallel()

	installRoot := filepath.Join(t.TempDir(), "store")
	lockFile := extension.LockFile{Extensions: map[string]extension.LockedExtension{}}

	resolved, err := extension.ResolveConfiguredSources([]extension.ConfiguredSource{
		{Source: testPathSource, Version: ""},
		{Source: testVimModeSource, Version: ""},
	}, lockFile, installRoot)

	require.NoError(t, err)
	require.Len(t, resolved, 2)
	assert.Equal(t, ".librecode/extensions", resolved[0].LoadPath)
	assert.Equal(t, "path", resolved[0].Status)
	assert.Equal(t, filepath.Join(installRoot, "official", "vim-mode"), resolved[1].LoadPath)
	assert.Equal(t, "github:omarluq/librecode//extensions/vim-mode", resolved[1].Lock.Resolved)
	assert.Equal(t, "v0.1.0", resolved[1].Lock.Version)
}

func TestLockFileRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), extension.LockFileName)
	lockFile := extension.LockFile{Extensions: map[string]extension.LockedExtension{
		testVimModeSource: {
			Resolved: "github:omarluq/librecode//extensions/vim-mode",
			Version:  "v0.1.0",
		},
	}}

	require.NoError(t, extension.WriteLockFile(path, lockFile))
	loaded, err := extension.ReadLockFile(path)

	require.NoError(t, err)
	assert.Equal(t, lockFile, loaded)
}
