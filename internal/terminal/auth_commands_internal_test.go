package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tui"
)

func TestAuthPanelItemsReflectProviderStatus(t *testing.T) {
	t.Parallel()

	app := newAuthPanelTestApp(t)

	app.openLoginPanel()

	require.Equal(t, modePanel, app.mode)
	require.NotNil(t, app.panel)
	assert.Equal(t, panelAuthLogin, app.selectedPanelKind)
	assert.Contains(t, panelItemValues(app.panel.Items()), openAICodexProviderID)
	assert.Contains(t, panelItemValues(app.panel.Items()), promptSendTestProvider)
	assert.Contains(t, app.authStatusLabel(promptSendTestProvider), string(auth.SourceStored))
	assert.Equal(t, "API key provider", authDescription(promptSendTestProvider))
	assert.Equal(t, promptSendTestProvider, providerDisplayName(promptSendTestProvider))
}

func TestLogoutPanelAndSelectionRemoveCredential(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		promptSendTestProvider: testPanelAuthCredential(),
	})
	app := newRenderTestApp(t)
	app.auth = storage

	app.openLogoutPanel()
	require.Equal(t, modePanel, app.mode)
	require.NotNil(t, app.panel)
	assert.Equal(t, panelAuthLogout, app.selectedPanelKind)
	assert.Contains(t, panelItemValues(app.panel.Items()), promptSendTestProvider)

	err := app.applyAuthSelection(context.Background(), promptSendTestProvider)

	require.NoError(t, err)
	assert.Equal(t, modeChat, app.mode)

	_, ok, err := storage.APIKeyContext(context.Background(), promptSendTestProvider)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestOpenLogoutPanelWithoutCredentialsReportsStatus(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})
	app := newRenderTestApp(t)
	app.auth = storage

	app.openLogoutPanel()

	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, "no stored credentials to remove", app.transcript.History[len(app.transcript.History)-1].Content)
	assert.Equal(t, modeChat, app.mode)
}

func TestAuthInfoTextIncludesProviderURLAndInstructions(t *testing.T) {
	t.Parallel()

	text := authInfoText("Codex", auth.OAuthAuthInfo{
		URL:          "",
		Instructions: "Paste the code",
	})

	assert.Contains(t, text, "Codex browser login:")
	assert.Contains(t, text, "Paste the code")
}

func TestShowAuthInfoAndReloadRuntime(t *testing.T) {
	t.Parallel()

	app := newAuthPanelTestApp(t)

	app.showAuthInfo()
	err := app.reloadRuntime(context.Background())

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(app.transcript.History), 2)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-2].Content, "auth status:")
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "reloaded auth and models")
}

func TestRunOAuthLoginPostsDoneAndErrorEvents(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})
	app := newRenderTestApp(t)
	app.auth = storage
	successConfig := oauthLoginConfig{
		LoginFunc: func(context.Context, func(auth.OAuthAuthInfo)) (*auth.Credential, error) {
			return &auth.Credential{
				OAuth:     nil,
				Type:      auth.CredentialTypeAPIKey,
				Key:       "oauth-key",
				Access:    "",
				Refresh:   "",
				AccountID: "",
				Expires:   0,
				ExpiresAt: 0,
			}, nil
		},
		Provider:       "oauth-provider",
		DisplayName:    "OAuth",
		AlreadyMessage: "already",
		LoginFailed:    "failed: ",
	}

	app.screen = newClipboardScreen()
	app.runOAuthLogin(context.Background(), successConfig)
	event := readAuthAsyncEvent(t, app)
	require.Equal(t, asyncEventAuthDone, event.Kind)

	_, ok, err := storage.APIKeyContext(context.Background(), "oauth-provider")
	require.NoError(t, err)
	assert.True(t, ok)

	failureConfig := successConfig
	failureConfig.LoginFunc = func(context.Context, func(auth.OAuthAuthInfo)) (*auth.Credential, error) {
		return nil, assert.AnError
	}
	failureConfig.Provider = "failing-provider"
	app.runOAuthLogin(context.Background(), failureConfig)
	event = readAuthAsyncEvent(t, app)
	require.Equal(t, asyncEventAuthError, event.Kind)
	assert.Equal(t, "failed: "+assert.AnError.Error(), event.Text)
}

func TestParentIDFromEntry(t *testing.T) {
	t.Parallel()

	assert.Nil(t, parentIDFromEntry(nil))
	parentID := parentIDFromEntry(&database.EntryEntity{
		ParentID: nil,
		Message: database.MessageEntity{
			Timestamp: time.Time{},
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		Summary:                    "",
		ToolStatus:                 "",
		CreatedAt:                  time.Time{},
		ID:                         "entry-1",
		Type:                       "",
		CustomType:                 "",
		DataJSON:                   "",
		ToolName:                   "",
		SessionID:                  "",
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              0,
		Display:                    false,
		ModelFacing:                false,
	})
	require.NotNil(t, parentID)
	assert.Equal(t, "entry-1", *parentID)
}

func newAuthPanelTestApp(t *testing.T) *App {
	t.Helper()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		promptSendTestProvider: testPanelAuthCredential(),
	})
	app := newRenderTestApp(t)
	app.auth = storage
	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     []model.Model{newPanelTestModel(promptSendTestModel, "Current")},
		Discovery:    disabledModelDiscovery(),
	})

	return app
}

func readAuthAsyncEvent(t *testing.T, app *App) *asyncEvent {
	t.Helper()

	select {
	case raw := <-app.screen.EventQ():
		interrupt, ok := raw.(*tcell.EventInterrupt)
		require.True(t, ok, "event = %T, want *tcell.EventInterrupt", raw)
		event, ok := interrupt.Data().(*asyncEvent)
		require.True(t, ok, "interrupt data = %T, want *asyncEvent", interrupt.Data())

		return event
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for async event")

		return nil
	}
}

func panelItemValues(items []tui.ListItem) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
	}

	return values
}

func TestAuthStorageUnavailablePathsReturnSharedError(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.auth = nil

	tests := []struct {
		call func() error
		name string
	}{
		{name: "logout", call: func() error { return app.logoutCommand(context.Background(), "anthropic") }},
		{name: "claude login", call: func() error { return app.startAnthropicClaudeLogin(context.Background()) }},
		{name: "oauth login", call: func() error { return app.loginOpenAICodex(context.Background()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()

			require.Error(t, err)
			assert.EqualError(t, err, authStorageUnavailableMessage)
		})
	}
}

func TestLoginCommandSavesAPIKeyCredential(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})
	app := newRenderTestApp(t)
	app.auth = storage
	app.cfg = promptSendTestConfig()

	err := app.loginCommand(context.Background(), "anthropic test-key")

	require.NoError(t, err)
	credential, ok, err := storage.APIKeyContext(context.Background(), anthropicAPIProviderID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "test-key", credential)
}
