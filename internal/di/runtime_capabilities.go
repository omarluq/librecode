package di

import (
	"context"
	"sync/atomic"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

type runtimeCapabilitySet struct {
	agentTasks assistant.AgentTaskController
	workflows  assistant.WorkflowSubmitter
}

type runtimeCapabilities struct {
	set atomic.Pointer[runtimeCapabilitySet]
}

func newRuntimeCapabilities() *runtimeCapabilities {
	return new(runtimeCapabilities)
}

func (capabilities *runtimeCapabilities) publish(
	agentTasks assistant.AgentTaskController,
	workflows assistant.WorkflowSubmitter,
) error {
	if agentTasks == nil {
		return oops.In("di").Code("nil_agent_task_controller").Errorf("agent task controller is nil")
	}

	if workflows == nil {
		return oops.In("di").Code("nil_workflow_submitter").Errorf("workflow submitter is nil")
	}

	published := &runtimeCapabilitySet{agentTasks: agentTasks, workflows: workflows}
	if !capabilities.set.CompareAndSwap(nil, published) {
		return oops.In("di").Code("runtime_capabilities_published").Errorf("runtime capabilities are already published")
	}

	return nil
}

func (capabilities *runtimeCapabilities) revoke() {
	capabilities.set.Store(new(runtimeCapabilitySet))
}

func (capabilities *runtimeCapabilities) load() (*runtimeCapabilitySet, error) {
	set := capabilities.set.Load()
	if set == nil || set.agentTasks == nil || set.workflows == nil {
		return nil, oops.In("assistant").Code("runtime_capabilities_unavailable").
			Errorf("runtime capabilities are unavailable")
	}

	return set, nil
}

func unavailableAgentTasks(err error) error {
	return oops.In("assistant").Code("agent_task_service_unavailable").
		Wrapf(err, "agent task service is unavailable")
}

func unavailableWorkflows(err error) error {
	return oops.In("assistant").Code("workflow_service_unavailable").
		Wrapf(err, "workflow service is unavailable")
}

func (capabilities *runtimeCapabilities) SubmitAgentTask(
	ctx context.Context,
	request *assistant.AgentTaskRequest,
) (*database.AgentTaskEntity, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, unavailableAgentTasks(err)
	}

	result, err := set.agentTasks.SubmitAgentTask(ctx, request)
	if err != nil {
		return nil, oops.In("di").Code("submit_agent_task").Wrapf(err, "submit agent task")
	}

	return result, nil
}

func (capabilities *runtimeCapabilities) Get(
	ctx context.Context,
	taskID string,
) (*database.AgentTaskEntity, bool, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, false, unavailableAgentTasks(err)
	}

	result, found, err := set.agentTasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("di").Code("get_agent_task").Wrapf(err, "get agent task")
	}

	return result, found, nil
}

func (capabilities *runtimeCapabilities) List(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]database.AgentTaskEntity, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, unavailableAgentTasks(err)
	}

	result, err := set.agentTasks.List(ctx, ownerSessionID, limit)
	if err != nil {
		return nil, oops.In("di").Code("list_agent_tasks").Wrapf(err, "list agent tasks")
	}

	return result, nil
}

func (capabilities *runtimeCapabilities) Cancel(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
	source string,
) (*database.TaskEntity, bool, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, false, unavailableAgentTasks(err)
	}

	result, found, err := set.agentTasks.Cancel(ctx, ownerSessionID, taskID, source)
	if err != nil {
		return nil, false, oops.In("di").Code("cancel_agent_task").Wrapf(err, "cancel agent task")
	}

	return result, found, nil
}

func (capabilities *runtimeCapabilities) Await(
	ctx context.Context,
	taskID string,
) (*database.AgentTaskEntity, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, unavailableAgentTasks(err)
	}

	result, err := set.agentTasks.Await(ctx, taskID)
	if err != nil {
		return nil, oops.In("di").Code("await_agent_task").Wrapf(err, "await agent task")
	}

	return result, nil
}

func (capabilities *runtimeCapabilities) AwaitAll(
	ctx context.Context,
	ownerSessionID string,
) ([]database.AgentTaskEntity, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, unavailableAgentTasks(err)
	}

	result, err := set.agentTasks.AwaitAll(ctx, ownerSessionID)
	if err != nil {
		return nil, oops.In("di").Code("await_all_agent_tasks").Wrapf(err, "await all agent tasks")
	}

	return result, nil
}

func (capabilities *runtimeCapabilities) SubscribeAgentTask(
	taskID string,
) (events <-chan database.TaskEventEntity, cancel func(), err error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, nil, unavailableAgentTasks(err)
	}

	events, cancel, err = set.agentTasks.SubscribeAgentTask(taskID)
	if err != nil {
		return nil, nil, oops.In("di").Code("subscribe_agent_task").Wrapf(err, "subscribe agent task")
	}

	return events, cancel, nil
}

func (capabilities *runtimeCapabilities) Events(
	ctx context.Context,
	taskID string,
	after int64,
	limit int,
) ([]database.TaskEventEntity, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, unavailableAgentTasks(err)
	}

	reader, ok := set.agentTasks.(interface {
		Events(context.Context, string, int64, int) ([]database.TaskEventEntity, error)
	})
	if !ok {
		return nil, oops.In("assistant").Code("agent_task_events_unavailable").
			Errorf("agent task events are unavailable")
	}

	result, err := reader.Events(ctx, taskID, after, limit)
	if err != nil {
		return nil, oops.In("di").Code("list_agent_task_events").Wrapf(err, "list agent task events")
	}

	return result, nil
}

func (capabilities *runtimeCapabilities) Submit(
	ctx context.Context,
	request *workflow.ServiceRequest,
) (*database.WorkflowRunEntity, error) {
	set, err := capabilities.load()
	if err != nil {
		return nil, unavailableWorkflows(err)
	}

	result, err := set.workflows.Submit(ctx, request)
	if err != nil {
		return nil, oops.In("di").Code("submit_workflow").Wrapf(err, "submit workflow")
	}

	return result, nil
}
