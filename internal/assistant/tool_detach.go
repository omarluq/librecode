package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
)

const foregroundCleanupTimeout = 2 * time.Second

type attachmentResolution uint8

const (
	attachmentUnresolved attachmentResolution = iota
	attachmentCompleted
	attachmentDetached
	attachmentCanceled
)

type foregroundAttachment struct {
	err      error
	resolved chan struct{}
	entity   *database.ToolTaskEntity
	taskID   string
	state    attachmentResolution
}

type foregroundAttachments struct {
	items map[string]*foregroundAttachment
	mu    sync.Mutex
}

func newForegroundAttachments() *foregroundAttachments {
	return &foregroundAttachments{items: make(map[string]*foregroundAttachment), mu: sync.Mutex{}}
}

// DetachForegroundTool detaches the eligible foreground call identified by its
// provider call ID. It returns the stable durable task ID.
func (runtime *Runtime) DetachForegroundTool(callID string) (string, bool) {
	runtime.attachments.mu.Lock()
	defer runtime.attachments.mu.Unlock()

	attachment := runtime.attachments.items[callID]
	if attachment == nil {
		return "", false
	}

	if attachment.state != attachmentUnresolved {
		return "", false
	}

	attachment.state = attachmentDetached
	close(attachment.resolved)

	return attachment.taskID, true
}

func (runtime *Runtime) executeManagedForegroundTool(
	ctx context.Context,
	prepared *preparedToolCall,
	owner, cwd string,
) bool {
	if !runtime.canManageForegroundTool(prepared, owner, cwd) {
		return false
	}

	entity, err := runtime.toolTasks.Start(ctx, &tooltask.StartRequest{
		Invocation: tooltask.Invocation{
			ID: prepared.call.ID, WrapperCallID: prepared.call.ID, OwnerSessionID: owner, CWD: cwd,
			ParentCallID: prepared.call.ParentCallID, InitiatingEntryID: "", SourceSequence: prepared.call.Sequence,
		},
		Admit: nil, Target: prepared.call.Name, Arguments: prepared.call.Arguments, Timeout: 0,
	})
	if err != nil {
		runtime.executePreparedToolCall(ctx, prepared, nil)

		return true
	}

	prepared.invocation.Release()
	prepared.ready = false

	attachment := &foregroundAttachment{
		err: nil, resolved: make(chan struct{}), entity: nil, taskID: entity.Task.ID, state: attachmentUnresolved,
	}

	runtime.attachments.mu.Lock()
	runtime.attachments.items[prepared.call.ID] = attachment
	runtime.attachments.mu.Unlock()

	defer func() {
		runtime.attachments.mu.Lock()
		delete(runtime.attachments.items, prepared.call.ID)
		runtime.attachments.mu.Unlock()
	}()

	resolution, completed, waitErr := runtime.resolveForegroundAttachment(ctx, owner, entity, attachment)

	switch resolution {
	case attachmentUnresolved:
		prepared.err = errors.New("foreground attachment was not resolved")
	case attachmentCompleted:
		prepared.result, prepared.err = foregroundOutcome(completed, waitErr)
		prepared.lifecycleCompleted = true
	case attachmentDetached:
		prepared.result = taskEntityResult(entity, false)
		prepared.lifecycleCompleted = true
	case attachmentCanceled:
		cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), foregroundCleanupTimeout)
		_, _, cancelErr := runtime.toolTasks.Cancel(cancelCtx, owner, entity.Task.ID)

		cancel()

		prepared.err = errors.Join(ctx.Err(), cancelErr)
	}

	return true
}

func (runtime *Runtime) resolveForegroundAttachment(
	ctx context.Context,
	owner string,
	entity *database.ToolTaskEntity,
	attachment *foregroundAttachment,
) (attachmentResolution, *database.ToolTaskEntity, error) {
	waitCtx, stopWaiting := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWaiting()

	waiterDone := make(chan struct{})

	go func() {
		completed, waitErr := runtime.toolTasks.Wait(waitCtx, owner, entity.Task.ID)

		runtime.attachments.mu.Lock()
		if attachment.state == attachmentUnresolved {
			attachment.state = attachmentCompleted
			attachment.entity = completed
			attachment.err = waitErr
			close(attachment.resolved)
		}
		runtime.attachments.mu.Unlock()
		close(waiterDone)
	}()

	select {
	case <-attachment.resolved:
	case <-ctx.Done():
		runtime.attachments.mu.Lock()
		if attachment.state == attachmentUnresolved {
			attachment.state = attachmentCanceled
			close(attachment.resolved)
		}
		runtime.attachments.mu.Unlock()
	}

	runtime.attachments.mu.Lock()
	resolution, completed, waitErr := attachment.state, attachment.entity, attachment.err
	runtime.attachments.mu.Unlock()

	if resolution != attachmentCompleted {
		stopWaiting()
		<-waiterDone
	}

	return resolution, completed, waitErr
}

func (runtime *Runtime) canManageForegroundTool(prepared *preparedToolCall, owner, cwd string) bool {
	return runtime.toolTasks != nil && prepared != nil && prepared.ready && !prepared.background &&
		owner != "" && cwd != "" && tooltask.Eligible(tool.Name(prepared.call.Name))
}

func foregroundOutcome(entity *database.ToolTaskEntity, waitErr error) (tool.Result, error) {
	if waitErr != nil {
		return tool.Result{}, waitErr
	}

	if entity == nil || entity.OutcomeJSON == nil {
		return tool.Result{}, errors.New("background task completed without an outcome")
	}

	var outcome struct {
		Error   string      `json:"error"`
		Result  tool.Result `json:"result"`
		IsError bool        `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(*entity.OutcomeJSON), &outcome); err != nil {
		return tool.Result{}, oops.In("assistant").Code("decode_foreground_outcome").Wrapf(
			err, "decode foreground outcome",
		)
	}

	if outcome.IsError {
		return outcome.Result, errors.New(outcome.Error)
	}

	return outcome.Result, nil
}
