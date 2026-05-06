// Package auth stores provider credentials without exposing secret values.
package auth

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"

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
)

// Credential stores API-key or OAuth credentials.
type Credential struct {
	OAuth     map[string]string `json:"oauth,omitempty"`
	Type      CredentialType    `json:"type"`
	Key       string            `json:"key,omitempty"`
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

// Storage provides Pi-style credential lookup with stored, runtime, env, and fallback sources.
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

	storage.lock.Lock()
	storage.credentials[provider] = *credential
	storage.lock.Unlock()

	return storage.persistProviderChange(ctx, provider, credential)
}

// Remove deletes a provider credential and persists the change.
func (storage *Storage) Remove(ctx context.Context, provider string) error {
	storage.lock.Lock()
	delete(storage.credentials, provider)
	storage.lock.Unlock()

	return storage.persistProviderChange(ctx, provider, nil)
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
	_, ok := storage.APIKey(provider)
	return ok
}

// APIKey resolves provider API key using runtime, stored, environment, then fallback sources.
func (storage *Storage) APIKey(provider string) (string, bool) {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	if apiKey, ok := storage.runtimeOverrides[provider]; ok {
		return apiKey, true
	}
	if credential, ok := storage.credentials[provider]; ok && credential.Type == CredentialTypeAPIKey {
		return credential.Key, credential.Key != ""
	}
	if apiKey, ok := envAPIKey(provider); ok {
		return apiKey, true
	}
	if storage.fallbackResolver != nil {
		return storage.fallbackResolver(provider)
	}

	return "", false
}

// AuthStatus reports credential availability without exposing values.
func (storage *Storage) AuthStatus(provider string) Status {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	if _, ok := storage.credentials[provider]; ok {
		return Status{Source: SourceStored, Label: "", Configured: true}
	}
	if _, ok := storage.runtimeOverrides[provider]; ok {
		return Status{Source: SourceRuntime, Label: "--api-key", Configured: false}
	}
	if envKey, ok := envKeyName(provider); ok {
		return Status{Source: SourceEnvironment, Label: envKey, Configured: false}
	}
	if storage.fallbackResolver != nil {
		if _, ok := storage.fallbackResolver(provider); ok {
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
	candidates := []string{normalized + "_API_KEY"}
	wellKnown := map[string]string{
		"anthropic": "ANTHROPIC_API_KEY",
		"google":    "GOOGLE_API_KEY",
		"openai":    "OPENAI_API_KEY",
	}
	if envKey, ok := wellKnown[provider]; ok {
		candidates = append([]string{envKey}, candidates...)
	}

	return lo.Uniq(candidates)
}
