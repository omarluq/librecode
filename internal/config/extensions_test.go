package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestLoadParsesExtensionUseDecodeForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected config.ExtensionUse
	}{
		{
			name: "string form",
			input: `extensions:
  use:
    - " path:.librecode/extensions "
`,
			expected: config.ExtensionUse{Source: "path:.librecode/extensions", Version: ""},
		},
		{
			name: "object form",
			input: `extensions:
  use:
    - source: " official:vim-mode "
      version: " v0.1.0 "
`,
			expected: config.ExtensionUse{Source: "official:vim-mode", Version: "v0.1.0"},
		},
		{
			name: "inline object form",
			input: `extensions:
  use:
    - {source: "github:owner/repo", version: "v1.0.0"}
`,
			expected: config.ExtensionUse{Source: "github:owner/repo", Version: "v1.0.0"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := loadExtensionConfigFromContent(t, testCase.input)

			require.Len(t, cfg.Extensions.Use, 1)
			assert.Equal(t, testCase.expected, cfg.Extensions.Use[0])
		})
	}
}

func loadExtensionConfigFromContent(t *testing.T, content string) *config.Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	loaded := config.Load(path)
	require.True(t, loaded.IsOk(), "config load failed: %v", loaded.Error())

	return loaded.MustGet()
}
