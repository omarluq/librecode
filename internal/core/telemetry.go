package core

import "strings"

// InstallTelemetryEnabler exposes the persisted install telemetry preference.
type InstallTelemetryEnabler interface {
	InstallTelemetryEnabled() bool
}

// TruthyEnvFlag reports whether value is an enabled environment flag.
func TruthyEnvFlag(value string) bool {
	if value == "" {
		return false
	}
	normalized := strings.ToLower(value)

	return normalized == "1" || normalized == "true" || normalized == "yes"
}

// InstallTelemetryEnabled resolves env override before settings.
func InstallTelemetryEnabled(settings InstallTelemetryEnabler, telemetryEnv string, envProvided bool) bool {
	if envProvided {
		return TruthyEnvFlag(telemetryEnv)
	}
	if settings == nil {
		return true
	}

	return settings.InstallTelemetryEnabled()
}
