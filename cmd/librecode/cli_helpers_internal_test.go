package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

const (
	extensionLoadPath = "/ext"
	testSessionID     = "session-1"
	testHelloCommand  = "hello"
)

func TestConfigFormattingHelpers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Path:            "data.db",
			ApplyMigrations: true,
			MaxOpenConns:    4,
			MaxIdleConns:    2,
			ConnMaxLifetime: time.Minute,
			BusyTimeout:     15 * time.Second,
		},
		Cache: config.CacheConfig{Enabled: true, Capacity: 10, TTL: time.Hour},
		Extensions: config.ExtensionsConfig{
			Use: []config.ExtensionUse{
				{Source: "github:user/ext", Version: "v1"},
				{Source: "local:./ext", Version: ""},
			},
			Enabled: true,
		},
		Assistant: config.AssistantConfig{
			Provider:      "openai",
			Model:         "gpt",
			ThinkingLevel: "low",
			Retry: config.RetryConfig{
				Enabled:     true,
				MaxAttempts: 2,
				BaseDelay:   time.Second,
				MaxDelay:    2 * time.Second,
			},
		},
		Context: config.ContextConfig{
			OutputReserveTokens:   1,
			ProviderReserveTokens: 2,
			SafetyMarginTokens:    3,
			PreflightEnabled:      true,
		},
		Models: config.ModelsConfig{Discovery: config.ModelDiscoveryConfig{
			SourceURL:    "https://models.example",
			CacheTTL:     time.Hour,
			FetchTimeout: time.Second,
			Enabled:      true,
		}},
		Logging: config.LoggingConfig{Level: "info", Format: "json"},
		App:     config.AppConfig{Name: "librecode", Env: "", WorkingLoader: config.LoaderUI{Text: "thinking"}},
	}

	entries := configEntries(cfg)
	assert.Contains(t, entries, configEntry{key: "database.busy_timeout", value: "15s"})
	assert.Contains(t, entries, configEntry{key: "extensions.use", value: "github:user/ext@v1,local:./ext"})
	assert.Equal(t, "development", resolveEnv("", "development"))
	assert.Equal(t, []string{"github:user/ext@v1", "local:./ext"}, extensionUseSources(cfg.Extensions.Use))
	assert.Contains(t, upperEnvKeys("LIBRECODE", entries), "LIBRECODE_APP_NAME")
}

func TestPrintLineWrapsWriterErrors(t *testing.T) {
	t.Parallel()

	err := printLine(failingWriter{}, "hello %s", "world")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write output")
}

func TestPrintSessionSummaryAndEntry(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	output := new(bytes.Buffer)
	cmd.SetOut(output)

	updatedAt := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)

	require.NoError(t, printSessionSummary(cmd, &database.SessionEntity{
		CreatedAt:     time.Time{},
		UpdatedAt:     updatedAt,
		ID:            testSessionID,
		CWD:           "/work",
		Name:          "",
		ParentSession: "",
	}))
	assert.Contains(t, output.String(), testSessionID+"\t2026-06-09 01:02:03\t(unnamed)")

	output.Reset()
	require.NoError(t, printSessionEntry(cmd, &database.EntryEntity{
		CreatedAt: time.Time{},
		ParentID:  nil,
		Message: database.MessageEntity{
			Timestamp: time.Time{},
			Role:      database.RoleUser,
			Content:   "message text",
			Provider:  "",
			Model:     "",
		},
		Summary:                    "",
		ToolStatus:                 "",
		Type:                       database.EntryTypeMessage,
		CustomType:                 "",
		DataJSON:                   "",
		ID:                         "entry-1",
		ToolName:                   "",
		SessionID:                  testSessionID,
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              0,
		Display:                    false,
		ModelFacing:                false,
	}))
	assert.Contains(t, output.String(), "entry-1\t"+"message\t"+"message text")
}

func TestExtensionFormattingHelpers(t *testing.T) {
	t.Parallel()

	loaded := []extension.LoadedExtension{{
		Name:          "ext",
		Path:          extensionLoadPath,
		Commands:      []string{testHelloCommand},
		Tools:         []string{"tool"},
		Keymaps:       []string{"ctrl+x"},
		Handlers:      []string{"event"},
		Timers:        2,
		TotalDuration: time.Second,
	}}
	assert.Equal(t, loaded[0], loadedExtensionsByPath(loaded)["/ext"])

	cmd := &cobra.Command{}
	output := new(bytes.Buffer)
	cmd.SetOut(output)
	require.NoError(t, printConfiguredExtension(cmd, &extension.ResolvedSource{
		Configured: extension.ConfiguredSource{Source: "local:/ext", Version: ""},
		Ref:        extension.SourceRef{Scheme: "local", Value: extensionLoadPath, Version: ""},
		Lock:       extension.LockedExtension{Resolved: extensionLoadPath, Version: "v1"},
		LoadPath:   extensionLoadPath,
		Name:       "ext",
		Status:     "configured",
	}, loadedExtensionsByPath(loaded)))
	assert.Contains(t, output.String(), "ext\tlocal:/ext\tloaded\tversion=v1")
	assert.Contains(t, output.String(), "commands="+testHelloCommand)
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, assert.AnError
}
