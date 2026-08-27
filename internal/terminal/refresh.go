package terminal

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gdamore/tcell/v3"
)

const (
	terminalRefreshTimeout = 750 * time.Millisecond
	// terminalSlowOperationThreshold is the documented warning threshold; normal
	// timing samples remain debug-only.
	terminalSlowOperationThreshold = 100 * time.Millisecond
)

type terminalRefreshTiming struct {
	Total         time.Duration
	AgentTasks    time.Duration
	AgentPanel    time.Duration
	ToolTasks     time.Duration
	Workflows     time.Duration
	Details       time.Duration
	WorkflowPanel time.Duration
}

// terminalRefreshResult is immutable after publication. Generation identifies
// the one request whose completion may release the App's in-flight state.
type terminalRefreshResult struct {
	SessionID  string
	Snapshot   terminalRefreshSnapshot
	Timing     terminalRefreshTiming
	Generation uint64
	TimedOut   bool
	Canceled   bool
}

type terminalRefreshLoader func(context.Context, *terminalRefreshRequest) terminalRefreshSnapshot

// loadInitialTasksAsync starts the same immutable-snapshot refresh used by the
// running terminal. The worker only publishes an interrupt; App state remains
// owned by the UI goroutine.
func (app *App) loadInitialTasksAsync(ctx context.Context) <-chan struct{} {
	return app.requestTerminalRefresh(ctx)
}

func (app *App) requestTerminalRefresh(ctx context.Context) <-chan struct{} {
	// Keep the parent task/workflow summary stable while inspecting a child.
	if len(app.agentTaskSessionStack) > 0 {
		return nil
	}

	if app.refreshInFlight {
		app.refreshPending = true

		app.logTerminalRefresh("coalesce", 0, "pending")

		return nil
	}

	if app.runtime == nil || app.sessionID == "" {
		return nil
	}

	app.refreshGeneration++
	generation := app.refreshGeneration
	request := app.captureTerminalRefreshRequest()
	app.refreshInFlight = true
	app.refreshInFlightGeneration = generation
	loadCtx, loadCancel := context.WithCancel(ctx)
	app.refreshCancel = loadCancel

	loader := app.refreshLoader
	if loader == nil {
		loader = loadTerminalRefreshSnapshot
	}

	screen := app.screen
	diagnostics := app.refreshDiagnostics
	done := make(chan struct{})

	go func() {
		defer close(done)

		refreshCtx, cancel := context.WithTimeout(loadCtx, terminalRefreshTimeout)
		defer cancel()

		started := time.Now()
		snapshot := loader(refreshCtx, &request)
		timing := snapshot.Timing
		timing.Total = time.Since(started)
		result := &terminalRefreshResult{
			Snapshot: snapshot, Timing: timing,
			SessionID: request.SessionID, Generation: generation,
			TimedOut: errors.Is(refreshCtx.Err(), context.DeadlineExceeded),
			Canceled: errors.Is(refreshCtx.Err(), context.Canceled),
		}

		outcome := refreshOutcome(result)
		logTerminalRefreshTimings(diagnostics, result, outcome)
		postTerminalRefreshResult(ctx, screen, result)
	}()

	return done
}

func refreshOutcome(result *terminalRefreshResult) string {
	switch {
	case result.TimedOut:
		return refreshOutcomeTimeout
	case result.Canceled:
		return refreshOutcomeCanceled
	case result.Snapshot.hasErrors():
		return "partial_error"
	default:
		return "success"
	}
}

// invalidateTerminalRefresh is UI-thread-only. A canceled worker retains its
// single-flight slot until its completion event is handled.
func (app *App) invalidateTerminalRefresh() {
	app.refreshGeneration++

	app.refreshPending = false
	if app.refreshCancel != nil {
		app.refreshCancel()
		app.refreshCancel = nil
	}
}

func screenStop(screen terminalScreen) <-chan struct{} {
	if stoppable, ok := screen.(interface{ StopQ() <-chan struct{} }); ok {
		return stoppable.StopQ()
	}

	return nil
}

func postTerminalRefreshResult(ctx context.Context, screen terminalScreen, result *terminalRefreshResult) {
	if screen == nil || ctx.Err() != nil {
		return
	}

	stop := screenStop(screen)
	select {
	case <-stop:
		return
	default:
	}

	postTerminalRefreshEvent(ctx, screen.EventQ(), stop, tcell.NewEventInterrupt(result))
}

func postTerminalRefreshEvent(
	ctx context.Context,
	events chan<- tcell.Event,
	stop <-chan struct{},
	event tcell.Event,
) (posted bool) {
	// Test screens may close their event queue during shutdown. Treat that race
	// like a stopped screen instead of crashing the refresh worker.
	defer func() {
		if recover() != nil {
			posted = false
		}
	}()

	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	case <-stop:
		return false
	}
}

// applyTerminalRefresh runs only from handleInterrupt on the UI goroutine.
func (app *App) applyTerminalRefresh(ctx context.Context, result *terminalRefreshResult) {
	if result == nil {
		return
	}

	if !app.refreshInFlight || result.Generation != app.refreshInFlightGeneration {
		app.logTerminalRefresh("apply", result.Timing.Total, "stale")

		return
	}

	pending := app.refreshPending
	app.refreshInFlight = false
	app.refreshInFlightGeneration = 0

	app.refreshPending = false
	if app.refreshCancel != nil {
		app.refreshCancel()
		app.refreshCancel = nil
	}

	if result.Generation != app.refreshGeneration || result.SessionID != app.sessionID {
		// A session transition should invalidate explicitly, but still release the
		// matching worker here so an unanticipated transition cannot wedge refreshes.
		app.logTerminalRefresh("apply", result.Timing.Total, "stale")
	} else {
		app.applyTerminalRefreshSnapshot(ctx, &result.Snapshot)
	}

	if pending && ctx.Err() == nil {
		app.logTerminalRefresh("coalesce", 0, "started")
		app.requestTerminalRefresh(ctx)
	}
}

func logTerminalRefreshTimings(
	diagnostics *terminalRefreshDiagnostics,
	result *terminalRefreshResult,
	outcome string,
) {
	diagnostics.log("total", result.Timing.Total, outcome, 0)

	state := func(valid bool, err error) terminalRefreshSectionState {
		return terminalRefreshSectionState{valid: valid, err: err}
	}
	for _, section := range []struct {
		name     string
		outcome  string
		duration time.Duration
	}{
		{"agent_tasks", terminalRefreshSectionOutcome(result,
			state(result.Snapshot.AgentTasks.Valid, result.Snapshot.AgentTasks.Err),
			state(result.Snapshot.AgentTaskByID.Valid, result.Snapshot.AgentTaskByID.Err),
		), result.Timing.AgentTasks},
		{"agent_panel", terminalRefreshSectionOutcome(result,
			state(result.Snapshot.AgentPanel.Valid, result.Snapshot.AgentPanel.Err),
		), result.Timing.AgentPanel},
		{"tool_tasks", terminalRefreshSectionOutcome(result,
			state(result.Snapshot.ToolTasks.Valid, result.Snapshot.ToolTasks.Err),
		), result.Timing.ToolTasks},
		{"workflows", terminalRefreshSectionOutcome(result,
			state(result.Snapshot.ActiveWorkflow.Valid, result.Snapshot.ActiveWorkflow.Err),
			state(result.Snapshot.WorkflowByID.Valid, result.Snapshot.WorkflowByID.Err),
		), result.Timing.Workflows},
		{"details", terminalRefreshSectionOutcome(result,
			state(result.Snapshot.WorkflowDetail.Valid, result.Snapshot.WorkflowDetail.Err),
		), result.Timing.Details},
		{"workflow_panel", terminalRefreshSectionOutcome(result,
			state(result.Snapshot.WorkflowPanel.Valid, result.Snapshot.WorkflowPanel.Err),
		), result.Timing.WorkflowPanel},
	} {
		if section.duration > 0 {
			diagnostics.log(section.name, section.duration, section.outcome, 0)
		}
	}
}

type terminalRefreshSectionState struct {
	err   error
	valid bool
}

func terminalRefreshSectionOutcome(
	result *terminalRefreshResult,
	sections ...terminalRefreshSectionState,
) string {
	allValid := len(sections) > 0

	hasError := false
	for _, section := range sections {
		hasError = hasError || section.err != nil
		allValid = allValid && section.valid
	}

	outcome := "not_requested"
	if hasError {
		outcome = refreshOutcomeError
	}

	if allValid {
		outcome = "success"
	}

	if allValid || hasError {
		return outcome
	}

	if result.TimedOut {
		return refreshOutcomeTimeout
	}

	if result.Canceled {
		return refreshOutcomeCanceled
	}

	return outcome
}

const (
	terminalDiagnosticWarningWindow = time.Minute
	refreshOutcomeTimeout           = "timeout"
	refreshOutcomeCanceled          = "canceled"
	refreshOutcomeError             = "error"
)

type terminalRefreshDiagnostics struct {
	last       map[string]time.Time
	suppressed map[string]int
	now        func() time.Time
	sync.Mutex
}

func newTerminalRefreshDiagnostics() *terminalRefreshDiagnostics {
	return &terminalRefreshDiagnostics{
		last: map[string]time.Time{}, suppressed: map[string]int{},
		now: time.Now, Mutex: sync.Mutex{},
	}
}

func (app *App) logTerminalRefresh(operation string, duration time.Duration, outcome string) {
	app.refreshDiagnostics.log(operation, duration, outcome, 0)
}

func (diagnostics *terminalRefreshDiagnostics) log(
	operation string,
	duration time.Duration,
	outcome string,
	count int,
) {
	logger := slog.Default()
	if logger.Enabled(context.Background(), slog.LevelDebug) {
		logger.Debug("terminal refresh", terminalRefreshAttributes(operation, duration, outcome, count)...)
	}

	if duration < terminalSlowOperationThreshold {
		return
	}

	key := operation + ":" + outcome

	diagnostics.Lock()
	now := diagnostics.now()

	last := diagnostics.last[key]
	if !last.IsZero() && now.Sub(last) < terminalDiagnosticWarningWindow {
		diagnostics.suppressed[key]++
		diagnostics.Unlock()

		return
	}

	suppressed := diagnostics.suppressed[key]
	diagnostics.last[key] = now
	diagnostics.suppressed[key] = 0
	diagnostics.Unlock()

	attributes := terminalRefreshAttributes(operation, duration, outcome, count)
	attributes = append(attributes, slog.Int("suppressed_count", suppressed))
	logger.Warn("terminal refresh slow", attributes...)
}

func terminalRefreshAttributes(operation string, duration time.Duration, outcome string, count int) []any {
	attributes := []any{
		slog.String("operation", operation),
		slog.Duration("duration", duration),
		slog.String("outcome", outcome),
	}
	if count > 0 {
		attributes = append(attributes, slog.Int("count", count))
	}

	return attributes
}
