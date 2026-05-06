package core

import "strings"

// InstallTelemetrySettings exposes the persisted install telemetry preference.
type InstallTelemetrySettings interface {
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
func InstallTelemetryEnabled(settings InstallTelemetrySettings, telemetryEnv string, envProvided bool) bool {
	if envProvided {
		return TruthyEnvFlag(telemetryEnv)
	}
	if settings == nil {
		return true
	}

	return settings.InstallTelemetryEnabled()
}
