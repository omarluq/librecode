package model

import (
	"context"
	"sync"

	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/auth"
)

// ConfigReader reads model registry configuration from database-backed runtime documents.
type ConfigReader interface {
	Read() (content []byte, found bool, err error)
}

// RegistryOptions configures a model registry.
type RegistryOptions struct {
	ConfigReader ConfigReader     `json:"-"`
	Auth         *auth.Storage    `json:"-"`
	ModelsPath   string           `json:"models_path"`
	BuiltIns     []Model          `json:"built_ins"`
	Discovery    DiscoveryOptions `json:"discovery"`
}

// Registry loads built-in and custom models and resolves provider request auth.
type Registry struct {
	loadError       error
	configSource    ConfigReader
	auth            *auth.Storage
	providerConfigs map[string]providerRequestConfig
	modelsPath      string
	models          []Model
	builtIns        []Model
	discovery       DiscoveryOptions
	lock            sync.RWMutex
}

type providerRequestConfig struct {
	Headers    map[string]string
	APIKey     string
	AuthHeader bool
}

// NewRegistry creates and refreshes a registry.
func NewRegistry(options *RegistryOptions) *Registry {
	resolvedOptions := registryOptions(options)
	registry := &Registry{
		configSource:    resolvedOptions.ConfigReader,
		auth:            resolvedOptions.Auth,
		providerConfigs: map[string]providerRequestConfig{},
		modelsPath:      resolvedOptions.ModelsPath,
		models:          []Model{},
		builtIns:        cloneModels(resolvedOptions.BuiltIns),
		discovery:       resolvedOptions.Discovery,
		lock:            sync.RWMutex{},
		loadError:       nil,
	}
	registry.Refresh()

	return registry
}

// Refresh reloads models from disk and registered built-ins.
func (registry *Registry) Refresh() {
	registry.RefreshContext(context.Background())
}

// RefreshContext reloads models from custom config, discovery, and registered built-ins.
func (registry *Registry) RefreshContext(ctx context.Context) {
	customResult := registry.loadCustomModels()
	discoveredModels, discoveryErr := DiscoverModels(ctx, registry.discovery)
	builtIns := applyProviderPatches(registry.builtIns, customResult.ProviderPatches, customResult.ModelOverrides)
	models := mergeModelCatalogs(builtIns, discoveredModels, customResult.Models)

	registry.lock.Lock()
	registry.models = models
	registry.providerConfigs = customResult.ProviderConfigs
	registry.loadError = firstRegistryError(customResult.Err, discoveryErr)
	registry.lock.Unlock()
}

// Error returns the latest models.json load error.
func (registry *Registry) Error() error {
	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return registry.loadError
}

// All returns all known models.
func (registry *Registry) All() []Model {
	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return cloneModels(registry.models)
}

// DiscoveryOptions returns the registry's configured discovery settings.
func (registry *Registry) DiscoveryOptions() DiscoveryOptions {
	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return registry.discovery
}

// Available returns models whose provider has some configured auth.
func (registry *Registry) Available() []Model {
	models := registry.All()

	return lo.Filter(models, func(model Model, _ int) bool {
		return registry.HasAuth(model.Provider)
	})
}

// HasAuth reports whether provider auth can be resolved.
func (registry *Registry) HasAuth(provider string) bool {
	if registry.auth != nil && registry.auth.HasAuth(provider) {
		return true
	}

	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return registry.providerConfigs[provider].APIKey != ""
}

// RequestAuth returns auth and headers for a model request.
func (registry *Registry) RequestAuth(provider string) RequestAuth {
	registry.lock.RLock()
	config := registry.providerConfigs[provider]
	registry.lock.RUnlock()

	apiKey := config.APIKey
	if registry.auth != nil {
		if resolvedAPIKey, ok := registry.auth.APIKey(provider); ok {
			apiKey = resolvedAPIKey
		}
	}

	if apiKey == "" && config.AuthHeader {
		return RequestAuth{Headers: cloneStringMap(config.Headers), APIKey: "", Error: "missing API key", OK: false}
	}

	return RequestAuth{Headers: cloneStringMap(config.Headers), APIKey: apiKey, Error: "", OK: true}
}

// RequestAuthContext returns auth and headers, refreshing OAuth credentials when needed.
func (registry *Registry) RequestAuthContext(ctx context.Context, provider string) RequestAuth {
	registry.lock.RLock()
	config := registry.providerConfigs[provider]
	registry.lock.RUnlock()

	apiKey := config.APIKey
	headers := cloneStringMap(config.Headers)

	if registry.auth != nil {
		credential, hasCredential := registry.auth.Get(provider)

		resolvedAPIKey, ok, err := registry.auth.APIKeyContext(ctx, provider)
		if err != nil {
			return RequestAuth{Headers: headers, APIKey: "", Error: err.Error(), OK: false}
		}

		if ok {
			apiKey = resolvedAPIKey
		}

		if hasCredential && credential.AccountID != "" {
			if headers == nil {
				headers = map[string]string{}
			}

			headers["chatgpt-account-id"] = credential.AccountID
		}
	}

	if apiKey == "" {
		return RequestAuth{
			Headers: headers,
			APIKey:  "",
			Error:   "missing API key for provider " + provider,
			OK:      false,
		}
	}

	return RequestAuth{Headers: headers, APIKey: apiKey, Error: "", OK: true}
}

func registryOptions(options *RegistryOptions) *RegistryOptions {
	if options != nil {
		if len(options.BuiltIns) == 0 {
			copyOptions := *options
			copyOptions.BuiltIns = BuiltInModels()

			return &copyOptions
		}

		return options
	}

	return &RegistryOptions{
		ConfigReader: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns:     BuiltInModels(),
		Discovery: DiscoveryOptions{
			Client:       nil,
			CachePath:    "",
			SourceURL:    "",
			CacheTTL:     0,
			FetchTimeout: 0,
			Enabled:      false,
		},
	}
}

func firstRegistryError(left, right error) error {
	if left != nil {
		return left
	}

	return right
}
