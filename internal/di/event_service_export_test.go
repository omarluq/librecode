package di

import "github.com/omarluq/librecode/internal/event"

// EventDiagnosticsForTest exposes the diagnostic observer to external tests.
func EventDiagnosticsForTest(service *EventService) *event.DiagnosticObserver {
	return service.diagnostics
}
