package executeworker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/omarluq/librecode/internal/mvmhost"
)

func TestVersion2ProgressBindingsMatchBothProfiles(t *testing.T) {
	t.Parallel()

	const source = `import "librecode/workflow"
workflow.Phase("review", "Review", "running")
workflow.Item("lint", "review", "Lint", "pending")
workflow.Event("files", map[string]any{"count": 2})
workflow.Log("info", "checking")
workflow.Item("lint", "review", "Lint", "succeeded")
workflow.Phase("review", "Review", "succeeded")`

	for _, profile := range []guestapi.Profile{guestapi.ProfileTurn, guestapi.ProfileDurable} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()

			bindings, err := executeworker.CompileBindings(profile, guestapi.Version2, nil)
			require.NoError(t, err)
			_, err = mvmhost.New().Eval(t.Context(), mvmhost.Request{
				Bindings: bindings, Name: "progress.go", Source: source,
			})
			require.NoError(t, err)

			for _, name := range []string{"Event", "Item", "Log", "Parallel", "Phase", "Pipeline"} {
				assert.Contains(t, bindings[guestapi.PackageWorkflow], name)
			}
		})
	}
}

func TestVersion2ProgressTerminalTransitionsAreImmutable(t *testing.T) {
	t.Parallel()

	bindings, err := executeworker.CompileBindings(guestapi.ProfileTurn, guestapi.Version2, nil)
	require.NoError(t, err)
	_, err = mvmhost.New().Eval(t.Context(), mvmhost.Request{
		Bindings: bindings,
		Name:     "terminal-progress.go",
		Source: `import "librecode/workflow"
workflow.Phase("review", "Review", "failed")
workflow.Phase("review", "Retry", "running")`,
	})
	require.ErrorContains(t, err, "is terminal")
}
