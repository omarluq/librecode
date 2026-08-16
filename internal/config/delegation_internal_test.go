package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDelegationDefaults(t *testing.T) {
	t.Parallel()

	cfg := Load("").MustGet()

	assert.Empty(t, cfg.Delegation.Provider)
	assert.Empty(t, cfg.Delegation.Model)
	assert.Empty(t, cfg.Delegation.ThinkingLevel)
}

func TestDelegationValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*DelegationConfig)
	}{
		{"provider only", func(delegationConfig *DelegationConfig) { delegationConfig.Provider = "openai" }},
		{"model only", func(delegationConfig *DelegationConfig) { delegationConfig.Model = "small" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := Load("").MustGet()
			test.mutate(&cfg.Delegation)
			err := cfg.validateDelegation()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be configured together")
		})
	}
}
