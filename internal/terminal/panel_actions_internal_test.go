package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/terminal/panel"
)

func TestApplyPanelSelectionUnknownKindReturnsError(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.selectedPanelKind = panel.Kind("mystery")

	err := app.applyPanelSelection(context.Background(), "value")
	if err == nil {
		t.Fatal("applyPanelSelection should return error for unknown panel kind")
	}

	if !strings.Contains(err.Error(), "unknown panel kind") {
		t.Fatalf("error = %q, want unknown panel kind", err.Error())
	}
}

func TestApplyPanelSelectionHotkeysAndChangelogAreNoops(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		kind panel.Kind
	}{
		{name: hotkeysCommandName, kind: panelHotkeys},
		{name: changelogCommandName, kind: panelChangelog},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.selectedPanelKind = testCase.kind

			if err := app.applyPanelSelection(context.Background(), "value"); err != nil {
				t.Fatalf("applyPanelSelection(%q) error = %v", testCase.kind, err)
			}
		})
	}
}
