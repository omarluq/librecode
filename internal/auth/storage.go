// Package auth stores provider credentials without exposing secret values.
package auth

import (
	"context"
	"sync"

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
//
// Key may either be the literal API key or the name of an environment variable
// containing the key. OAuth token fields are kept in flat and map shapes for
// provider-specific OAuth flows.
type Credential struct {
	OAuth     map[string]string `json:"oauth,omitempty"`
	Type      CredentialType    `json:"type"`
	Key       string            `json:"key,omitempty"`
	Access    string            `json:"access,omitempty"`
	Refresh   string            `json:"refresh,omitempty"`
	AccountID string            `json:"account_id,omitempty"`
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

// Locker serializes access to credential bytes.
type Locker interface {
	WithLock(ctx context.Context, callback func(current []byte) (LockResult, error)) error
}

// Storage provides librecode-style credential lookup with stored, runtime, env, and fallback sources.
//
// Locking contract: lock protects credentials, runtimeOverrides, errors,
// loadError, and fallbackResolver. Do not call fallbackResolver or OAuth
// refreshers while holding lock; take an authSnapshot first and operate on the
// copied values.
type Storage struct {
	backend          Locker
	loadError        error
	fallbackResolver func(provider string) (string, bool)
	credentials      map[string]Credential
	runtimeOverrides map[string]string
	errors           []error
	lock             sync.RWMutex
}

// NewStorage creates credential storage over a backend and loads existing credentials.
func NewStorage(ctx context.Context, backend Locker) (*Storage, error) {
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
