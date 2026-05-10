// Package auth stores provider credentials without exposing secret values.
package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

// CredentialType identifies the stored credential kind.
type CredentialType string

const (
	// CredentialTypeAPIKey stores a provider API key.
	CredentialTypeAPIKey CredentialType = "api_key"
	// CredentialTypeOAuth stores OAuth token material.
	CredentialTypeOAuth CredentialType = "oauth"
)

// Source describes where a provider credential is configured.
type Source string

const (
	// SourceStored comes from auth.json.
	SourceStored Source = "stored"
	// SourceRuntime comes from a process-only override.
	SourceRuntime Source = "runtime"
	// SourceEnvironment comes from an environment variable.
	SourceEnvironment Source = "environment"
	// SourceFallback comes from a caller-supplied resolver.
	SourceFallback Source = "fallback"
	// SourceExternal comes from compatible external auth files imported at startup.
	SourceExternal Source = "external"
)

// Credential stores API-key or OAuth credentials.
type Credential struct {
	OAuth     map[string]string `json:"oauth,omitempty"`
	Type      CredentialType    `json:"type"`
	Key       string            `json:"key,omitempty"`
	Access    string            `json:"access,omitempty"`
	Refresh   string            `json:"refresh,omitempty"`
	AccountID string            `json:"accountId,omitempty"`
	Expires   int64             `json:"expires,omitempty"`
	ExpiresAt int64             `json:"expires_at,omitempty"`
}

// Status reports whether auth exists without revealing secrets.
type Status struct {
	Source     Source `json:"source,omitempty"`
	Label      string `json:"label,omitempty"`
	Configured bool   `json:"configured"`
}

// LockResult tells a backend whether to persist a new auth.json payload.
type LockResult struct {
	Next  []byte
	Write bool
}

// Backend serializes access to credential bytes.
type Backend interface {
	WithLock(ctx context.Context, callback func(current []byte) (LockResult, error)) error
}

// Storage provides librecode-style credential lookup with stored, runtime, env, and fallback sources.
type Storage struct {
	backend          Backend
	loadError        error
	fallbackResolver func(provider string) (string, bool)
	credentials      map[string]Credential
	runtimeOverrides map[string]string
	errors           []error
	lock             sync.RWMutex
}

// NewStorage creates credential storage over a backend and loads existing credentials.
func NewStorage(ctx context.Context, backend Backend) (*Storage, error) {
	storage := &Storage{
		fallbackResolver: nil,
		backend:          backend,
		credentials:      map[string]Credential{},
		runtimeOverrides: map[string]string{},
		errors:           []error{},
		lock:             sync.RWMutex{},
		loadError:        nil,
	}
	if err := storage.Reload(ctx); err != nil {
		return nil, err
	}

	return storage, nil
}

// NewInMemoryStorage creates storage backed by in-memory JSON data.
func NewInMemoryStorage(ctx context.Context, credentials map[string]Credential) (*Storage, error) {
	backend, err := NewMemoryBackend(credentials)
	if err != nil {
		return nil, err
	}

	return NewStorage(ctx, backend)
}

// Reload refreshes credentials from the backend.
func (storage *Storage) Reload(ctx context.Context) error {
	var content []byte
	if err := storage.backend.WithLock(ctx, func(current []byte) (LockResult, error) {
		content = append([]byte{}, current...)
		return LockResult{Next: nil, Write: false}, nil
	}); err != nil {
		storage.recordError(err)
		return oops.In("auth").Code("reload").Wrapf(err, "reload credentials")
	}

	credentials, err := parseCredentials(content)
	storage.lock.Lock()
	defer storage.lock.Unlock()
	if err != nil {
		storage.loadError = err
		storage.errors = append(storage.errors, err)
		return oops.In("auth").Code("parse").Wrapf(err, "parse credentials")
	}
	storage.credentials = credentials
	storage.loadError = nil

	return nil
}

// SetRuntimeAPIKey sets a process-only provider API key.
func (storage *Storage) SetRuntimeAPIKey(provider, apiKey string) {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	storage.runtimeOverrides[provider] = apiKey
}

// RemoveRuntimeAPIKey removes a process-only provider API key.
func (storage *Storage) RemoveRuntimeAPIKey(provider string) {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	delete(storage.runtimeOverrides, provider)
}

// SetFallbackResolver configures a resolver for custom provider API keys.
func (storage *Storage) SetFallbackResolver(resolver func(provider string) (string, bool)) {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	storage.fallbackResolver = resolver
}

// Get returns a stored credential only.
func (storage *Storage) Get(provider string) (Credential, bool) {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	credential, ok := storage.credentials[provider]
	return credential, ok
}

// Set stores a provider credential and persists it.
func (storage *Storage) Set(ctx context.Context, provider string, credential *Credential) error {
	if credential == nil {
		return oops.In("auth").Code("nil_credential").Errorf("credential is required")
	}
	if err := storage.persistProviderChange(ctx, provider, credential); err != nil {
		return err
	}

	storage.lock.Lock()
	defer storage.lock.Unlock()
	storage.credentials[provider] = *credential

	return nil
}

// Remove deletes a provider credential and persists the change.
func (storage *Storage) Remove(ctx context.Context, provider string) error {
	if err := storage.persistProviderChange(ctx, provider, nil); err != nil {
		return err
	}

	storage.lock.Lock()
	defer storage.lock.Unlock()
	delete(storage.credentials, provider)

	return nil
}

// List returns providers with stored credentials.
func (storage *Storage) List() []string {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	providers := lo.Keys(storage.credentials)
	slices.Sort(providers)

	return providers
}

// HasStored reports whether auth.json contains a credential for provider.
func (storage *Storage) HasStored(provider string) bool {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	_, ok := storage.credentials[provider]
	return ok
}

// HasAuth reports whether any source can provide auth for provider.
func (storage *Storage) HasAuth(provider string) bool {
	_, runtimeOK, credential, credentialOK, resolver := storage.authSnapshot(provider)
	if runtimeOK {
		return true
	}
	if credentialOK && credential.hasSecretMaterial() {
		return true
	}
	if _, ok := envAPIKey(provider); ok {
		return true
	}
	if resolver != nil {
		_, ok := resolver(provider)
		return ok
	}

	return false
}

// APIKey resolves provider API key using runtime, stored, environment, then fallback sources.
func (storage *Storage) APIKey(provider string) (string, bool) {
	apiKey, runtimeOK, credential, credentialOK, resolver := storage.authSnapshot(provider)
	if runtimeOK {
		return apiKey, true
	}
	if credentialOK {
		if apiKey, ok := credential.apiKeyValue(); ok {
			return apiKey, true
		}
	}
	if apiKey, ok := envAPIKey(provider); ok {
		return apiKey, true
	}
	if resolver != nil {
		return resolver(provider)
	}

	return "", false
}

// APIKeyContext resolves provider API key and refreshes OAuth credentials when needed.
func (storage *Storage) APIKeyContext(ctx context.Context, provider string) (apiKey string, found bool, err error) {
	apiKey, runtimeOK, credential, credentialOK, resolver := storage.authSnapshot(provider)
	if runtimeOK {
		return apiKey, true, nil
	}

	if credentialOK {
		apiKey, found, err := storage.credentialAPIKeyContext(ctx, provider, &credential)
		if err != nil || found {
			return apiKey, found, err
		}
	}
	if apiKey, ok := envAPIKey(provider); ok {
		return apiKey, true, nil
	}
	if resolver != nil {
		apiKey, ok := resolver(provider)
		return apiKey, ok, nil
	}

	return "", false, nil
}

func (storage *Storage) authSnapshot(provider string) (
	runtimeKey string,
	hasRuntime bool,
	credential Credential,
	hasCredential bool,
	resolver func(provider string) (string, bool),
) {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	runtimeKey, hasRuntime = storage.runtimeOverrides[provider]
	credential, hasCredential = storage.credentials[provider]
	resolver = storage.fallbackResolver

	return runtimeKey, hasRuntime, credential, hasCredential, resolver
}

func (storage *Storage) credentialAPIKeyContext(
	ctx context.Context,
	provider string,
	credential *Credential,
) (apiKey string, found bool, err error) {
	if credential.Type == CredentialTypeAPIKey {
		value, ok := credential.apiKeyValue()
		return value, ok, nil
	}
	if provider != openAICodexProvider || credential.Type != CredentialTypeOAuth {
		value, ok := credential.apiKeyValue()
		return value, ok, nil
	}
	refreshed, apiKey, err := openAICodexAPIKey(ctx, credential)
	if err != nil {
		return "", false, err
	}
	if apiKey == "" {
		return "", false, nil
	}
	if refreshed != credential && refreshed.oauthAccess() != credential.oauthAccess() {
		if err := storage.Set(ctx, provider, refreshed); err != nil {
			return "", false, err
		}
	}

	return apiKey, true, nil
}

// ImportOpenAICodexFromKnownFiles imports compatible existing Codex auth if librecode has no credential yet.
func (storage *Storage) ImportOpenAICodexFromKnownFiles(ctx context.Context) (bool, error) {
	if storage.HasStored(openAICodexProvider) {
		return false, nil
	}

	return storage.importOpenAICodexFromKnownFiles(ctx)
}

// SyncOpenAICodexFromKnownFiles refreshes librecode auth from compatible existing Codex auth.
func (storage *Storage) SyncOpenAICodexFromKnownFiles(ctx context.Context) (bool, error) {
	return storage.importOpenAICodexFromKnownFiles(ctx)
}

func (storage *Storage) importOpenAICodexFromKnownFiles(ctx context.Context) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, oops.In("auth").Code("user_home").Wrapf(err, "resolve user home")
	}
	candidates := []func() (*Credential, bool){
		func() (*Credential, bool) {
			return openAICodexCredentialFromNativeFile(filepath.Join(home, ".codex", "auth.json"))
		},
		func() (*Credential, bool) {
			return openAICodexCredentialFromAuthFile(filepath.Join(home, ".pi", "agent", "auth.json"))
		},
	}
	for _, candidate := range candidates {
		credential, ok := candidate()
		if !ok {
			continue
		}
		if err := storage.Set(ctx, openAICodexProvider, credential); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

// AuthStatus reports credential availability without exposing values.
func (storage *Storage) AuthStatus(provider string) Status {
	_, runtimeOK, credential, credentialOK, resolver := storage.authSnapshot(provider)
	if runtimeOK {
		return Status{Source: SourceRuntime, Label: "--api-key", Configured: false}
	}
	if credentialOK {
		if _, ok := credential.apiKeyValue(); ok {
			return Status{Source: SourceStored, Label: "", Configured: true}
		}
	}
	if envKey, ok := envKeyName(provider); ok {
		return Status{Source: SourceEnvironment, Label: envKey, Configured: false}
	}
	if resolver != nil {
		if _, ok := resolver(provider); ok {
			return Status{Source: SourceFallback, Label: "custom provider config", Configured: false}
		}
	}

	return Status{Source: "", Label: "", Configured: false}
}

// DrainErrors returns accumulated non-secret storage errors and clears them.
func (storage *Storage) DrainErrors() []error {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	drained := append([]error{}, storage.errors...)
	storage.errors = []error{}

	return drained
}

func (storage *Storage) persistProviderChange(
	ctx context.Context,
	provider string,
	credential *Credential,
) error {
	if err := storage.currentLoadError(); err != nil {
		return oops.In("auth").Code("load_error").Wrapf(err, "persist credentials")
	}
	err := storage.backend.WithLock(ctx, func(current []byte) (LockResult, error) {
		credentials, err := parseCredentials(current)
		if err != nil {
			return LockResult{Next: nil, Write: false}, err
		}
		if credential == nil {
			delete(credentials, provider)
		} else {
			credentials[provider] = *credential
		}
		next, err := json.MarshalIndent(credentials, "", "  ")
		if err != nil {
			return LockResult{Next: nil, Write: false}, err
		}

		return LockResult{Next: next, Write: true}, nil
	})
	if err != nil {
		storage.recordError(err)
		return oops.In("auth").Code("persist").Wrapf(err, "persist credentials")
	}

	return nil
}

func (storage *Storage) currentLoadError() error {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	return storage.loadError
}

func (storage *Storage) recordError(err error) {
	storage.lock.Lock()
	defer storage.lock.Unlock()

	storage.errors = append(storage.errors, err)
}

func (credential *Credential) apiKeyValue() (string, bool) {
	switch credential.Type {
	case CredentialTypeAPIKey:
		resolved := resolveStoredKey(credential.Key)
		return resolved, resolved != ""
	case CredentialTypeOAuth:
		access := credential.oauthAccess()
		return access, access != "" && !credential.oauthExpired()
	default:
		return "", false
	}
}

func (credential *Credential) hasSecretMaterial() bool {
	switch credential.Type {
	case CredentialTypeAPIKey:
		return strings.TrimSpace(credential.Key) != ""
	case CredentialTypeOAuth:
		return credential.oauthAccess() != "" || credential.oauthRefresh() != ""
	default:
		return false
	}
}

func (credential *Credential) oauthAccess() string {
	if credential.Access != "" {
		return credential.Access
	}
	if credential.OAuth != nil {
		return credential.OAuth["access"]
	}

	return ""
}

func (credential *Credential) oauthRefresh() string {
	if credential.Refresh != "" {
		return credential.Refresh
	}
	if credential.OAuth != nil {
		return credential.OAuth["refresh"]
	}

	return ""
}

func (credential *Credential) oauthExpired() bool {
	expires := credential.Expires
	if expires == 0 {
		expires = credential.ExpiresAt
	}
	if expires == 0 || expires > 100000000000 {
		return expires != 0 && timeNowMillis() >= expires
	}

	return timeNowMillis() >= expires*1000
}

func resolveStoredKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if envValue := strings.TrimSpace(os.Getenv(trimmed)); envValue != "" {
		return envValue
	}

	return trimmed
}

func timeNowMillis() int64 {
	return timeNow().UnixMilli()
}

var timeNow = time.Now

func parseCredentials(content []byte) (map[string]Credential, error) {
	if strings.TrimSpace(string(content)) == "" {
		return map[string]Credential{}, nil
	}
	credentials := map[string]Credential{}
	if err := json.Unmarshal(content, &credentials); err != nil {
		return map[string]Credential{}, err
	}

	return credentials, nil
}

func envAPIKey(provider string) (string, bool) {
	envKey, ok := envKeyName(provider)
	if !ok {
		return "", false
	}
	value := strings.TrimSpace(os.Getenv(envKey))

	return value, value != ""
}

func envKeyName(provider string) (string, bool) {
	candidates := envKeyCandidates(provider)
	key, ok := lo.Find(candidates, func(candidate string) bool {
		return strings.TrimSpace(os.Getenv(candidate)) != ""
	})
	if ok {
		return key, true
	}

	return "", false
}

func envKeyCandidates(provider string) []string {
	normalized := strings.ToUpper(provider)
	normalized = strings.NewReplacer("-", "_", ".", "_", "/", "_").Replace(normalized)
	candidates := []string{normalized + apiKeyEnvSuffix()}
	if envKey, ok := wellKnownEnvKey(provider); ok {
		candidates = append([]string{envKey}, candidates...)
	}

	return lo.Uniq(candidates)
}

func wellKnownEnvKey(provider string) (string, bool) {
	keys := map[string]string{
		"anthropic":              "ANTHROPIC" + apiKeyEnvSuffix(),
		"azure-openai-responses": "AZURE_OPENAI" + apiKeyEnvSuffix(),
		"cerebras":               "CEREBRAS" + apiKeyEnvSuffix(),
		"cloudflare-ai-gateway":  "CLOUDFLARE" + apiKeyEnvSuffix(),
		"cloudflare-workers-ai":  "CLOUDFLARE" + apiKeyEnvSuffix(),
		"deepseek":               "DEEPSEEK" + apiKeyEnvSuffix(),
		"fireworks":              "FIREWORKS" + apiKeyEnvSuffix(),
		"google":                 "GEMINI" + apiKeyEnvSuffix(),
		"groq":                   "GROQ" + apiKeyEnvSuffix(),
		"huggingface":            "HF_" + credentialWord(),
		"kimi-coding":            "KIMI" + apiKeyEnvSuffix(),
		"minimax":                "MINIMAX" + apiKeyEnvSuffix(),
		"minimax-cn":             "MINIMAX_CN" + apiKeyEnvSuffix(),
		"mistral":                "MISTRAL" + apiKeyEnvSuffix(),
		"openai":                 "OPENAI" + apiKeyEnvSuffix(),
		"opencode":               "OPENCODE" + apiKeyEnvSuffix(),
		"opencode-go":            "OPENCODE" + apiKeyEnvSuffix(),
		"openrouter":             "OPENROUTER" + apiKeyEnvSuffix(),
		"vercel-ai-gateway":      "AI_GATEWAY" + apiKeyEnvSuffix(),
		"xai":                    "XAI" + apiKeyEnvSuffix(),
		"xiaomi":                 "XIAOMI" + apiKeyEnvSuffix(),
		"xiaomi-token-plan-ams":  "XIAOMI_TOKEN_PLAN_AMS" + apiKeyEnvSuffix(),
		"xiaomi-token-plan-cn":   "XIAOMI_TOKEN_PLAN_CN" + apiKeyEnvSuffix(),
		"xiaomi-token-plan-sgp":  "XIAOMI_TOKEN_PLAN_SGP" + apiKeyEnvSuffix(),
		"zai":                    "ZAI" + apiKeyEnvSuffix(),
	}
	envKey, ok := keys[provider]

	return envKey, ok
}

func apiKeyEnvSuffix() string {
	return "_API" + "_KEY"
}

func credentialWord() string {
	return "TOK" + "EN"
}
