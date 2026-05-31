package auth

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

// Get returns a stored credential only.
func (storage *Storage) Get(provider string) (Credential, bool) {
	storage.lock.RLock()
	defer storage.lock.RUnlock()

	credential, ok := storage.credentials[provider]
	return credential, ok
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
	if credential.Type != CredentialTypeOAuth {
		value, ok := credential.apiKeyValue()
		return value, ok, nil
	}
	refreshed, apiKey, err := refreshOAuthCredential(ctx, provider, credential)
	if err != nil {
		return "", false, err
	}
	if apiKey == "" {
		return "", false, nil
	}
	if refreshed.oauthAccess() != credential.oauthAccess() {
		if err := storage.Set(ctx, provider, refreshed); err != nil {
			return "", false, err
		}
	}

	return apiKey, true, nil
}

func refreshOAuthCredential(ctx context.Context, provider string, credential *Credential) (*Credential, string, error) {
	switch provider {
	case openAICodexProvider:
		return openAICodexAPIKey(ctx, credential)
	case anthropicClaudeProvider:
		return anthropicAPIKey(ctx, credential)
	default:
		value, _ := credential.apiKeyValue()
		return credential, value, nil
	}
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
