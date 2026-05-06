package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const openAICodexProviderID = "openai-codex"

func (app *App) openLoginPanel() {
	app.openPanel(newSelectionPanel(
		panelAuthLogin,
		"Login",
		"Select provider; Codex imports ~/.codex auth, API-key providers fill /login",
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
	if provider == openAICodexProviderID {
		return "ChatGPT Plus/Pro subscription OAuth"
	}

	return "API key provider"
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
	if provider == openAICodexProviderID && len(fields) == 1 {
		return app.loginOpenAICodex(ctx)
	}
	if len(fields) < 2 {
		app.editor.setText("/login " + provider + " ")
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

func (app *App) loginOpenAICodex(ctx context.Context) error {
	if app.auth == nil {
		return fmt.Errorf("auth storage is unavailable")
	}
	if imported, err := app.auth.SyncOpenAICodexFromKnownFiles(ctx); err != nil {
		return err
	} else if imported {
		app.refreshModels()
		app.setModel(openAICodexProviderID, model.DefaultModelPerProvider[openAICodexProviderID])
		app.addSystemMessage("imported Codex auth from ~/.codex/auth.json")
		return nil
	}
	if _, ok, err := app.auth.APIKeyContext(ctx, openAICodexProviderID); err != nil {
		return err
	} else if ok {
		app.refreshModels()
		app.setModel(openAICodexProviderID, model.DefaultModelPerProvider[openAICodexProviderID])
		app.addSystemMessage("Codex auth is already configured")
		return nil
	}
	app.addSystemMessage(strings.Join([]string{
		"No compatible Codex credentials found.",
		"Run Codex's login once to create ~/.codex/auth.json, then run /login openai-codex again.",
		"Direct embedded OpenAI OAuth currently returns Authentication Error for this client.",
	}, "\n"))

	return nil
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
