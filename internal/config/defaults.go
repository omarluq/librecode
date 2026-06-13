package config

import "time"

const (
	defaultDatabaseConnMaxLifetime = 30 * time.Minute
	defaultDatabaseBusyTimeout     = 15 * time.Second
	defaultRetryMaxAttempts        = 3
	defaultRetryBaseDelay          = 2 * time.Second
	defaultRetryMaxDelay           = 30 * time.Second
	defaultProviderReserveTokens   = 2_048
	defaultSafetyMarginTokens      = 8_192
	defaultDiscoveryCacheTTL       = 24 * time.Hour
	defaultDiscoveryFetchTimeout   = 10 * time.Second
	defaultCacheCapacity           = 512
	defaultCacheTTL                = 10 * time.Minute
)
