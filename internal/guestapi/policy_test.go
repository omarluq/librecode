package guestapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/guestapi"
)

func TestVersionIsIndependentFromPersistedSourceVersion(t *testing.T) {
	t.Parallel()

	assert.Equal(t, guestapi.CurrentVersion, guestapi.Version("2"))
	assert.NotEqual(t, guestapi.CurrentVersion, guestapi.Version("v1"))
}

func TestCanonicalPackageNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"librecode/tools",
		"librecode/agents",
		"librecode/workflow",
		"librecode/artifacts",
		"librecode/state",
	}, []string{
		guestapi.PackageTools,
		guestapi.PackageAgents,
		guestapi.PackageWorkflow,
		guestapi.PackageArtifacts,
		guestapi.PackageState,
	})
	assert.Equal(t, "tools", guestapi.LegacyPackageTools)
}

func TestAvailabilityManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		packageName string
		function    string
		turn        bool
		durable     bool
		implemented bool
	}{
		{guestapi.PackageTools, "Call", true, false, true},
		{guestapi.PackageAgents, "Run", false, true, false},
		{guestapi.PackageAgents, "Spawn", false, true, false},
		{guestapi.PackageWorkflow, "Pipeline", true, true, false},
		{guestapi.PackageArtifacts, "Put", true, true, false},
		{guestapi.PackageState, "Get", false, true, false},
	}

	manifest := guestapi.AvailabilityManifest()
	manifest[0].Function = "changed"
	assert.NotEqual(t, "changed", guestapi.AvailabilityManifest()[0].Function)

	for _, test := range tests {
		t.Run(test.packageName+"/"+test.function, func(t *testing.T) {
			t.Parallel()

			var found *guestapi.Availability

			functionPolicy := guestapi.AvailabilityManifest()
			for index := range functionPolicy {
				candidate := &functionPolicy[index]
				if candidate.Package == test.packageName && candidate.Function == test.function {
					found = candidate

					break
				}
			}

			require.NotNil(t, found)
			assert.Equal(t, test.turn, found.Available(guestapi.ProfileTurn))
			assert.Equal(t, test.durable, found.Available(guestapi.ProfileDurable))
			assert.False(t, found.Available(guestapi.Profile("unknown")))
			assert.Equal(t, test.implemented, found.Implemented)
		})
	}
}

func TestValidateWorkerContract(t *testing.T) {
	t.Parallel()

	for _, profile := range []guestapi.Profile{guestapi.ProfileTurn, guestapi.ProfileDurable} {
		for _, version := range []guestapi.Version{guestapi.Version1, guestapi.Version2} {
			require.NoError(t, guestapi.ValidateWorkerContract(profile, version))
		}
	}

	require.ErrorContains(t, guestapi.ValidateWorkerContract("other", guestapi.Version2), "unknown")
	require.ErrorContains(t, guestapi.ValidateWorkerContract(guestapi.ProfileTurn, "3"), "incompatible")
}

func TestStableCapabilityErrorCodes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []guestapi.CapabilityErrorCode{
		"guest_capability_unavailable",
		"guest_capability_denied",
		"guest_capability_unsupported",
		"guest_capability_recursive",
	}, []guestapi.CapabilityErrorCode{
		guestapi.ErrorUnavailable,
		guestapi.ErrorDenied,
		guestapi.ErrorUnsupported,
		guestapi.ErrorRecursive,
	})
}
