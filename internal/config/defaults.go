package config

import "time"

const (
	defaultDatabaseConnMaxLifetime     = 30 * time.Minute
	defaultDatabaseBusyTimeout         = 15 * time.Second
	defaultRetryMaxAttempts            = 3
	defaultRetryBaseDelay              = 2 * time.Second
	defaultRetryMaxDelay               = 30 * time.Second
	defaultProviderReserveTokens       = 2_048
	defaultSafetyMarginTokens          = 8_192
	defaultExtensionContributionTokens = 8_192
	defaultAutoCompactionThreshold     = 80
	defaultRetainedTailMaxTokens       = 64_000
	defaultDiscoveryCacheTTL           = 24 * time.Hour
	defaultDiscoveryFetchTimeout       = 10 * time.Second
	defaultCacheCapacity               = 512
	defaultCacheTTL                    = 10 * time.Minute
	defaultTaskWorkers                 = 4
	defaultTaskPollInterval            = 250 * time.Millisecond
	defaultTaskLeaseDuration           = 30 * time.Second
	defaultTaskHeartbeatInterval       = 10 * time.Second
	defaultTaskRecoveryInterval        = 30 * time.Second
	defaultTaskTimeout                 = 30 * time.Minute
	defaultTaskMaxTimeout              = 2 * time.Hour
	defaultTaskMaxOutcomeBytes         = 256 * 1024
	minimumTaskOutcomeBytes            = 256
)
