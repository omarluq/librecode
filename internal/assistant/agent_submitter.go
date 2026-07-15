package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/database"
)

const defaultWorkflowAgentName = "general"

// AgentSubmitRequest describes one high-level durable agent launch.
type AgentSubmitRequest struct {
	ParentTaskID    string
	OwnerSessionID  string
	CWD             string
	AgentName       string
	Prompt          string
	Model           string
	Provider        string
	ConcurrencyKey  string
	NodeKey         string
	InvocationIndex int
	Depth           int
}

// AgentSubmitter creates child sessions and snapshots agent policy before
// submitting durable work.
type AgentSubmitter struct {
	controller AgentTaskController
	sessions   *database.SessionRepository
	catalog    *agent.Catalog
}

// NewAgentSubmitter creates the shared high-level agent submission boundary.
func NewAgentSubmitter(
	controller AgentTaskController,
	sessions *database.SessionRepository,
	catalog *agent.Catalog,
) (*AgentSubmitter, error) {
	if controller == nil || sessions == nil || catalog == nil {
		return nil, oops.In("assistant").Code("invalid_agent_submitter_dependencies").
			Errorf("agent task controller, sessions, and catalog are required")
	}

	return &AgentSubmitter{controller: controller, sessions: sessions, catalog: catalog}, nil
}

// SubmitAgent resolves policy, creates a child session, and durably submits work.
func (submitter *AgentSubmitter) SubmitAgent(
	ctx context.Context,
	request *AgentSubmitRequest,
) (*database.AgentTaskEntity, error) {
	if request == nil {
		return nil, oops.In("assistant").Code("nil_agent_submit_request").Errorf("agent submit request is nil")
	}

	definition, err := submitter.resolveDefinition(request)
	if err != nil {
		return nil, err
	}

	policy, err := json.Marshal(&definition)
	if err != nil {
		return nil, oops.In("assistant").Code("encode_agent_profile").Wrapf(err, "encode agent profile")
	}

	child, err := submitter.sessions.CreateSession(
		ctx,
		request.CWD,
		childSessionName(definition.Name, request.Prompt),
		request.OwnerSessionID,
	)
	if err != nil {
		return nil, oops.In("assistant").Code("create_agent_session").Wrapf(err, "create child agent session")
	}

	concurrencyKey := request.ConcurrencyKey
	if concurrencyKey == "" {
		concurrencyKey = request.OwnerSessionID
	}

	depth := request.Depth
	if depth <= 0 {
		depth = 1
	}

	task, err := submitter.controller.SubmitAgentTask(ctx, &AgentTaskRequest{
		ParentTaskID: request.ParentTaskID, OwnerSessionID: request.OwnerSessionID,
		ChildSessionID: child.ID, AgentName: definition.Name,
		Prompt: request.Prompt, Model: definition.Model.Model, Provider: definition.Model.Provider,
		PolicyJSON: string(policy), ConcurrencyKey: concurrencyKey,
		NodeKey: request.NodeKey, InvocationIndex: request.InvocationIndex, Depth: depth,
	})
	if err == nil {
		return task, nil
	}

	if task != nil {
		return task, oops.In("assistant").Code("submit_agent_task").Wrapf(err, "submit agent task")
	}

	if deleteErr := submitter.sessions.DeleteSession(context.WithoutCancel(ctx), child.ID); deleteErr != nil {
		return nil, errors.Join(err, deleteErr)
	}

	return nil, oops.In("assistant").Code("submit_agent_task").Wrapf(err, "submit agent task")
}

func (submitter *AgentSubmitter) resolveDefinition(request *AgentSubmitRequest) (agent.Definition, error) {
	agentName := strings.ToLower(strings.TrimSpace(request.AgentName))
	if agentName == "" {
		agentName = defaultWorkflowAgentName
	}

	definition, found := submitter.catalog.Get(agentName)
	if !found {
		return agent.Definition{}, oops.In("assistant").Code("unknown_agent").Errorf("unknown agent %q", agentName)
	}

	if request.Model != "" {
		definition.Model.Model = request.Model
	}

	if request.Provider != "" {
		definition.Model.Provider = request.Provider
	}

	return definition, nil
}
