package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/tooltask"
)

// admitBackgroundToolTarget applies the target lifecycle exactly once before
// durable acceptance. Workers reconstruct the persisted post-hook arguments.
func (runtime *Runtime) admitBackgroundToolTarget(ctx context.Context, request *tooltask.StartRequest) error {
	call := &ToolCallEvent{
		ID: request.Invocation.ID, ParentCallID: request.Invocation.ParentCallID,
		Name: request.Target, Arguments: request.Arguments, ArgumentsJSON: request.Arguments.String(),
		Sequence: request.Invocation.SourceSequence,
	}
	if err := runtime.dispatchToolCallLifecycle(ctx, call); err != nil {
		return err
	}

	request.Target = call.Name
	request.Arguments = call.Arguments

	return nil
}

// BackgroundToolCompletion applies result/error lifecycle hooks in the worker
// that owns the durable execution. Admission has already applied tool_call.
func (runtime *Runtime) BackgroundToolCompletion(
	ctx context.Context,
	completion *tooltask.Completion,
) error {
	event := BackgroundToolCompletionEvent(completion)

	if err := runtime.dispatchToolResultLifecycle(ctx, &event); err != nil {
		if runtime.logger != nil {
			runtime.logger.Debug("tool result lifecycle failed", "error", err)
		}

		return err
	}

	completion.Result = canonicalToolResult(completion.Result, &event)
	if event.IsError {
		completion.Err = toolEventError(event.Error)
	} else {
		completion.Err = nil
	}

	return nil
}

// BackgroundToolCompletionEvent converts a canonical worker completion into the
// same result event used by foreground tools and provider streams.
func BackgroundToolCompletionEvent(completion *tooltask.Completion) ToolEvent {
	if completion == nil {
		return ToolEvent{
			CallID: "", ParentCallID: "", Name: "", ArgumentsJSON: "", DetailsJSON: "",
			Result: "", Error: "", Sequence: 0, IsError: false,
		}
	}

	return toolEventFromResult(&ToolCallEvent{
		ArgumentsJSON: completion.ArgumentsJSON,
		ID:            completion.InvocationID,
		ParentCallID:  completion.ParentCallID,
		Name:          completion.Target,
		Arguments:     completion.Arguments,
		Sequence:      completion.SourceSequence,
	}, completion.Result, completion.Err)
}

type toolEventError string

func (err toolEventError) Error() string { return string(err) }
