package terminal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/browser"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	anthropicAPIProviderID    = "anthropic"
	anthropicClaudeProviderID = "anthropic-claude"
	openAICodexProviderID     = "openai-codex"
)

func (app *App) openLoginPanel() {
	app.openPanel(newSelectionPanel(
		panelAuthLogin,
		"Login",
		"Select provider; subscription providers open browser login, API-key providers fill /login",
		app.loginProviderItems(),
		true,
	))
}

func (app *App) openLogoutPanel() {
	items := app.logoutProviderItems()
	if len(items) == 0 {
		app.addSystemMessage("no stored credentials to remove")
		return
	}
	app.openPanel(newSelectionPanel(panelAuthLogout, "Logout", "Select stored credential to remove", items, true))
}

func (app *App) loginProviderItems() []panelItem {
	providers := app.authProviderIDs()
	items := make([]panelItem, 0, len(providers))
	for _, provider := range providers {
		items = append(items, panelItem{
			Value:       provider,
			Title:       providerDisplayName(provider),
			Description: authDescription(provider),
			Meta:        app.authStatusLabel(provider),
		})
	}

	return items
}

func (app *App) logoutProviderItems() []panelItem {
	providers := []string{}
	if app.auth != nil {
		providers = app.auth.List()
	}
	items := make([]panelItem, 0, len(providers))
	for _, provider := range providers {
		items = append(items, panelItem{
			Value:       provider,
			Title:       providerDisplayName(provider),
			Description: authDescription(provider),
			Meta:        "stored",
		})
	}

	return items
}

func (app *App) authProviderIDs() []string {
	seen := map[string]bool{}
	providers := []string{openAICodexProviderID}
	seen[openAICodexProviderID] = true
	if app.models != nil {
		models := app.models.All()
		for index := range models {
			knownModel := &models[index]
			if !seen[knownModel.Provider] {
				providers = append(providers, knownModel.Provider)
				seen[knownModel.Provider] = true
			}
		}
	}

	return providers
}

func (app *App) authStatusLabel(provider string) string {
	if app.auth == nil {
		return "unavailable"
	}
	status := app.auth.AuthStatus(provider)
	if status.Configured {
		return "✓ " + string(status.Source)
	}
	if status.Source != "" {
		return string(status.Source) + " " + status.Label
	}

	return "not configured"
}

func authDescription(provider string) string {
	switch provider {
	case anthropicAPIProviderID:
		return "Anthropic API key"
	case anthropicClaudeProviderID:
		return "Claude Pro/Max subscription OAuth"
	case openAICodexProviderID:
		return "ChatGPT Plus/Pro subscription OAuth"
	default:
		return "API key provider"
	}
}

func providerDisplayName(provider string) string {
	if displayName, ok := model.ProviderDisplayNames[provider]; ok {
		return displayName
	}

	return provider
}

func (app *App) loginCommand(ctx context.Context, args string) error {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		app.openLoginPanel()
		return nil
	}
	provider := fields[0]
	if len(fields) == 1 {
		switch provider {
		case anthropicClaudeProviderID:
			return app.startAnthropicClaudeLogin(ctx)
		case openAICodexProviderID:
			return app.loginOpenAICodex(ctx)
		}
	}
	if provider == anthropicClaudeProviderID {
		return app.completeAnthropicClaudeLogin(ctx, strings.TrimSpace(strings.TrimPrefix(args, provider)))
	}
	if len(fields) < 2 {
		app.resetPromptHistoryNavigation()
		app.setComposerText("/login " + provider + " ")
		app.setStatus("paste API key after provider and press Enter")
		return nil
	}
	apiKey := strings.TrimSpace(strings.TrimPrefix(args, provider))
	if apiKey == "" {
		return fmt.Errorf("api key is required")
	}
	credential := auth.Credential{
		OAuth:     nil,
		Type:      auth.CredentialTypeAPIKey,
		Key:       apiKey,
		Access:    "",
		Refresh:   "",
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
	if err := app.auth.Set(ctx, provider, &credential); err != nil {
		return err
	}
	app.refreshModels()
	app.selectProviderDefault(provider)
	app.addSystemMessage("saved API key for " + providerDisplayName(provider))

	return nil
}

func (app *App) logoutCommand(ctx context.Context, args string) error {
	provider := strings.TrimSpace(args)
	if provider == "" {
		app.openLogoutPanel()
		return nil
	}
	if app.auth == nil {
		return fmt.Errorf("auth storage is unavailable")
	}
	if err := app.auth.Remove(ctx, provider); err != nil {
		return err
	}
	app.refreshModels()
	app.addSystemMessage("removed credentials for " + providerDisplayName(provider))

	return nil
}

func (app *App) applyAuthSelection(ctx context.Context, value string) error {
	switch app.selectedPanelKind {
	case panelAuthLogin:
		app.closePanel()
		return app.loginCommand(ctx, value)
	case panelAuthLogout:
		app.closePanel()
		return app.logoutCommand(ctx, value)
	case panelModel, panelScopedModels, panelSettings, panelSessions, panelTree:
		return nil
	}

	return nil
}

type oauthLoginConfig struct {
	LoginFunc      func(context.Context, func(auth.OAuthAuthInfo)) (*auth.Credential, error)
	Provider       string
	DisplayName    string
	AlreadyMessage string
	LoginFailed    string
}

func (app *App) startAnthropicClaudeLogin(_ context.Context) error {
	if app.auth == nil {
		return fmt.Errorf("auth storage is unavailable")
	}
	flowURL, err := auth.AnthropicLoginURL()
	if err != nil {
		return err
	}
	text := authInfoText("Claude", auth.OAuthAuthInfo{
		URL: flowURL,
		Instructions: "Complete login in your browser, then paste the authorization code with: /login " +
			anthropicClaudeProviderID + " <code#state>",
	})
	app.addMessage(database.RoleCustom, text)
	app.resetPromptHistoryNavigation()
	app.setComposerText("/login " + anthropicClaudeProviderID + " ")

	return nil
}

func (app *App) completeAnthropicClaudeLogin(ctx context.Context, code string) error {
	if app.auth == nil {
		return fmt.Errorf("auth storage is unavailable")
	}
	credential, err := auth.LoginAnthropicWithCode(ctx, code)
	if err != nil {
		return err
	}
	if err := app.auth.Set(ctx, anthropicClaudeProviderID, credential); err != nil {
		return err
	}
	app.refreshModels()
	app.selectProviderDefault(anthropicClaudeProviderID)
	app.addSystemMessage("logged in to " + providerDisplayName(anthropicClaudeProviderID))

	return nil
}

func (app *App) loginOpenAICodex(ctx context.Context) error {
	return app.loginOAuthProvider(ctx, oauthLoginConfig{
		Provider:       openAICodexProviderID,
		DisplayName:    "Codex",
		AlreadyMessage: "Codex auth is already configured",
		LoginFailed:    "Codex login failed: ",
		LoginFunc:      auth.LoginOpenAICodex,
	})
}

func (app *App) loginOAuthProvider(ctx context.Context, config oauthLoginConfig) error {
	if app.auth == nil {
		return fmt.Errorf("auth storage is unavailable")
	}
	if _, ok, err := app.auth.APIKeyContext(ctx, config.Provider); err != nil {
		return err
	} else if ok {
		app.refreshModels()
		app.setModel(config.Provider, model.DefaultModelPerProvider[config.Provider])
		app.addSystemMessage(config.AlreadyMessage)
		return nil
	}

	app.authWorking = true
	app.workStartedAt = time.Now()
	app.workFrame = 0
	go app.runOAuthLogin(ctx, config)

	return nil
}

func (app *App) runOAuthLogin(ctx context.Context, config oauthLoginConfig) {
	credential, err := config.LoginFunc(ctx, func(info auth.OAuthAuthInfo) {
		app.postAsyncEvent(ctx, asyncEvent{
			Response:  nil,
			ToolEvent: nil,
			Kind:      asyncEventAuthURL,
			Provider:  config.Provider,
			Text:      authInfoText(config.DisplayName, info),
			PromptID:  0,
		})
	})
	if err != nil {
		app.postOAuthLoginError(ctx, config, err)
		return
	}
	if err := app.auth.Set(ctx, config.Provider, credential); err != nil {
		app.postOAuthLoginError(ctx, config, err)
		return
	}
	app.postAsyncEvent(ctx, asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Kind:      asyncEventAuthDone,
		Provider:  config.Provider,
		Text:      "",
		PromptID:  0,
	})
}

func (app *App) postOAuthLoginError(ctx context.Context, config oauthLoginConfig, err error) {
	app.postAsyncEvent(ctx, asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Kind:      asyncEventAuthError,
		Provider:  config.Provider,
		Text:      config.LoginFailed + err.Error(),
		PromptID:  0,
	})
}

func authInfoText(provider string, info auth.OAuthAuthInfo) string {
	lines := []string{
		provider + " browser login:",
		info.URL,
	}
	if err := browser.Open(info.URL); err == nil {
		lines = append(lines, "Opened your browser. Complete login there to continue.")
	} else {
		lines = append(lines, "Open the URL above in your browser to continue.")
	}
	if strings.TrimSpace(info.Instructions) != "" {
		lines = append(lines, info.Instructions)
	}

	return strings.Join(lines, "\n")
}

func (app *App) refreshModels() {
	if app.models != nil {
		app.models.Refresh()
	}
}

func (app *App) selectProviderDefault(provider string) {
	modelID := model.DefaultModelPerProvider[provider]
	if modelID == "" {
		return
	}
	app.setModel(provider, modelID)
}

func (app *App) showAuthInfo() {
	providers := app.authProviderIDs()
	lines := make([]string, 0, len(providers)+1)
	lines = append(lines, "auth status:")
	for _, provider := range providers {
		lines = append(lines, provider+": "+app.authStatusLabel(provider))
	}
	app.addMessage(database.RoleCustom, strings.Join(lines, "\n"))
}

func (app *App) reloadRuntime(ctx context.Context) error {
	if app.auth != nil {
		if err := app.auth.Reload(ctx); err != nil {
			return err
		}
	}
	app.refreshModels()
	app.addSystemMessage("reloaded auth and models")

	return nil
}
