package executeworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"
	"sync"

	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/workflowkernel"
	"github.com/omarluq/librecode/internal/workflowprogress"
	"github.com/samber/oops"
)

const errorKey = "error"

type rpcCaller struct {
	in          io.Reader
	out         io.Writer
	terminalErr error
	pending     map[uint64]chan Message
	nextID      uint64
	mu          sync.Mutex
	writeMu     sync.Mutex
}

// Serve runs one evaluation. Tool bindings are synchronous callback RPCs to the
// parent; only JSON values cross the process boundary.
func Serve(input io.Reader, output io.Writer) error {
	request, err := Read(input)
	if err != nil {
		return err
	}

	if request.Type != "eval" {
		return fmt.Errorf("unexpected execute worker message %q", request.Type)
	}

	evalCtx, cancelEval := context.WithCancel(context.Background())
	defer cancelEval()

	caller := &rpcCaller{
		in: input, out: output, pending: make(map[uint64]chan Message), nextID: 0, terminalErr: nil,
		mu: sync.Mutex{}, writeMu: sync.Mutex{},
	}
	go caller.readResponses(cancelEval)

	bindings, err := workerBindings(evalCtx, &request, caller)
	if err != nil {
		return err
	}

	result, evalErr := mvmhost.New().Eval(evalCtx, mvmhost.Request{
		Bindings: bindings, Name: request.Name, Source: request.Source,
	})

	response := resultMessage(result, evalErr)

	return Write(output, &response)
}

func workerBindings(ctx context.Context, request *Message, caller *rpcCaller) (mvmhost.Bindings, error) {
	profile, version, err := workerContract(request)
	if err != nil {
		return nil, err
	}

	var arguments map[string]any
	if profile == guestapi.ProfileDurable && len(request.Arguments) > 0 && string(request.Arguments) != jsonNullValue {
		if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
			return nil, fmt.Errorf("decode workflow arguments: %w", err)
		}
	}

	return profileBindings(ctx, profile, version, arguments, workflowCallBridge{caller: caller}), nil
}

func workerContract(request *Message) (guestapi.Profile, guestapi.Version, error) {
	profile, version := request.Profile, request.GuestAPI
	if profile == "" && version == "" { // pre-manifest version-1 protocol
		switch request.Mode {
		case "", "execute":
			profile = guestapi.ProfileTurn
		case "workflow":
			profile = guestapi.ProfileDurable
		default:
			return "", "", fmt.Errorf("unknown execute worker mode %q", request.Mode)
		}

		version = guestapi.Version1
	} else if profile == "" || version == "" {
		return "", "", errors.New("execute worker profile and guest API version must be provided together")
	}

	if err := guestapi.ValidateWorkerContract(profile, version); err != nil {
		return "", "", fmt.Errorf("validate worker contract: %w", err)
	}

	return profile, version, nil
}

func toolsBindings(packageName string, caller *rpcCaller) mvmhost.Bindings {
	return mvmhost.Bindings{packageName: {
		"Search":   func(query string) any { return caller.call("search", "", query, nil) },
		"Describe": func(name string) any { return caller.call("describe", name, "", nil) },
		"Call":     func(name string, input any) any { return caller.call("call", name, "", input) },
	}}
}

func profileBindings(
	ctx context.Context,
	profile guestapi.Profile,
	version guestapi.Version,
	arguments map[string]any,
	bridge callBridge,
) mvmhost.Bindings {
	if version == guestapi.Version1 {
		if profile == guestapi.ProfileDurable {
			return workflowModeBindings(arguments, bridge)
		}

		if workflowBridge, ok := bridge.(workflowCallBridge); ok {
			return toolsBindings(guestapi.LegacyPackageTools, workflowBridge.caller)
		}

		return inertToolsBindings(guestapi.LegacyPackageTools)
	}

	bindings := version2Bindings(profile)
	if bindings[guestapi.PackageWorkflow] == nil {
		bindings[guestapi.PackageWorkflow] = make(map[string]any)
	}

	maps.Copy(bindings[guestapi.PackageWorkflow], combinatorBindings(ctx)[guestapi.PackageWorkflow])
	maps.Copy(bindings[guestapi.PackageWorkflow], progressBindings(ctx, bridge)[guestapi.PackageWorkflow])

	if profile == guestapi.ProfileTurn {
		if workflowBridge, ok := bridge.(workflowCallBridge); ok {
			bindings[guestapi.PackageTools] = toolsBindings(
				guestapi.PackageTools,
				workflowBridge.caller,
			)[guestapi.PackageTools]
		} else {
			bindings[guestapi.PackageTools] = inertToolsBindings(guestapi.PackageTools)[guestapi.PackageTools]
		}
	}

	if profile == guestapi.ProfileDurable {
		bindings[guestapi.PackageAgents] = agentsBindings(bridge)[guestapi.PackageAgents]
	}

	return bindings
}

func version2Bindings(profile guestapi.Profile) mvmhost.Bindings {
	bindings := make(mvmhost.Bindings)

	for _, availability := range guestapi.AvailabilityManifest() {
		if !availability.Available(profile) || availability.Implemented {
			continue
		}

		packageBindings := bindings[availability.Package]
		if packageBindings == nil {
			packageBindings = make(map[string]any)
			bindings[availability.Package] = packageBindings
		}

		packageName, functionName := availability.Package, availability.Function
		packageBindings[functionName] = func(...any) error {
			return fmt.Errorf("%s: %s.%s is not implemented", guestapi.ErrorUnsupported, packageName, functionName)
		}
	}

	return bindings
}

func combinatorBindings(ctx context.Context) mvmhost.Bindings {
	return mvmhost.Bindings{guestapi.PackageWorkflow: {
		"Parallel": func(items []any, callback func(any) (any, error), concurrency int) (any, error) {
			return workflowkernel.Parallel(ctx, items, callback, concurrency)
		},
		"Pipeline": func(
			items []any,
			stages []func(any) (any, error),
			concurrency int,
		) (any, error) {
			callbacks := make([]workflowkernel.Callback, len(stages))
			for index := range stages {
				callbacks[index] = stages[index]
			}

			return workflowkernel.Pipeline(ctx, items, callbacks, concurrency)
		},
	}}
}

func progressBindings(ctx context.Context, bridge callBridge) mvmhost.Bindings {
	emitter := workflowprogress.New(func(_ context.Context, event workflowprogress.Event) error {
		workflowBridge, ok := bridge.(workflowCallBridge)
		if !ok {
			return nil
		}

		if err := workflowBridge.caller.progress(event); err != nil {
			panic(err)
		}

		return nil
	})

	return mvmhost.Bindings{guestapi.PackageWorkflow: {
		"Phase": func(id, title, state string) error {
			return emitter.Phase(ctx, id, title, state)
		},
		"Item": func(id, phaseID, title, state string) error {
			return emitter.Item(ctx, id, phaseID, title, state)
		},
		"Event": func(name string, data map[string]any) error {
			return emitter.Event(ctx, name, data)
		},
		"Log": func(level, message string) error {
			return emitter.Log(ctx, level, message)
		},
	}}
}

func inertToolsBindings(packageName string) mvmhost.Bindings {
	fail := func() { panic("executeworker: tools binding must not be called during compile") }

	return mvmhost.Bindings{packageName: {
		"Search": func(string) any {
			fail()

			return nil
		},
		"Describe": func(string) any {
			fail()

			return nil
		},
		"Call": func(string, any) any {
			fail()

			return nil
		},
	}}
}

// callBridge forwards workflow binding calls somewhere. The production worker
// bridges them over RPC to the parent process; compile validation uses an inert
// bridge because nothing is executed.
type callBridge interface {
	call(method string, input any) any
	callResult(method, taskID string, input any) (any, error)
}

type workflowCallBridge struct {
	caller *rpcCaller
}

func (bridge workflowCallBridge) call(method string, input any) any {
	return bridge.caller.call(method, "", "", input)
}

func (bridge workflowCallBridge) callResult(method, taskID string, input any) (any, error) {
	return bridge.caller.callResult(method, taskID, "", input)
}

// workflowModeBindings builds the "librecode/workflow" package exposed to
// workflow source. It is the single source of truth for binding signatures:
// both worker evaluation and compile-time validation derive from it, so a
// signature change cannot make validation accept what execution rejects (or
// vice versa).
func workflowModeBindings(arguments map[string]any, bridge callBridge) mvmhost.Bindings {
	agents := agentsBindings(bridge)[guestapi.PackageAgents]

	return mvmhost.Bindings{guestapi.PackageWorkflow: {
		"Arguments": arguments,
		"Agent":     agents["Spawn"],
		"Wait":      agents["Wait"],
		"List":      agents["List"],
		"Cancel":    agents["Cancel"],
		"Pipeline": func(items []any, callback func(any) (any, error), concurrency int) (any, error) {
			results, err := workerPipeline(items, callback, concurrency)

			return pipelineValue(results), err
		},
	}}
}

func agentsBindings(bridge callBridge) mvmhost.Bindings {
	spawn := func(prompt string, options ...map[string]any) (any, error) {
		return bridge.callResult("workflow_agent", "", map[string]any{"prompt": prompt, "options": options})
	}
	wait := func(taskID string) (any, error) {
		return bridge.callResult("workflow_wait", taskID, nil)
	}

	return mvmhost.Bindings{guestapi.PackageAgents: {
		"Run": func(prompt string, options ...map[string]any) (any, error) {
			taskID, err := spawn(prompt, options...)
			if err != nil {
				return nil, err
			}

			id, ok := taskID.(string)
			if !ok {
				return nil, fmt.Errorf("agent spawn returned task ID of type %T", taskID)
			}

			return wait(id)
		},
		"Spawn": spawn,
		"Wait":  wait,
		"List": func() (any, error) {
			return bridge.callResult("workflow_list", "", nil)
		},
		"Cancel": func(taskID string) (any, error) {
			return bridge.callResult("workflow_cancel", taskID, nil)
		},
	}}
}

// CompileBindings returns the same profile/version manifest used by runtime
// evaluation, with inert callbacks suitable for compile-only validation.
func CompileBindings(
	profile guestapi.Profile,
	version guestapi.Version,
	arguments map[string]any,
) (mvmhost.Bindings, error) {
	if err := guestapi.ValidateWorkerContract(profile, version); err != nil {
		return nil, fmt.Errorf("validate compile worker contract: %w", err)
	}

	// CompileBindings is intentionally context-free: bindings are reflected for
	// type checking but never invoked. Runtime bindings capture the worker's
	// evaluation context in workerBindings.
	return profileBindings(context.Background(), profile, version, arguments, inertCallBridge{}), nil
}

// WorkflowModeBindings preserves the version-1 compile API for persisted
// workflow source.
func WorkflowModeBindings(arguments map[string]any) mvmhost.Bindings {
	bindings, err := CompileBindings(guestapi.ProfileDurable, guestapi.Version1, arguments)
	if err != nil {
		panic(err)
	}

	return bindings
}

// inertCallBridge satisfies callBridge without doing anything. Compilation
// never invokes bindings, but an unreachable path must fail loudly rather than
// silently return nil results.
type inertCallBridge struct{}

func (inertCallBridge) call(method string, _ any) any {
	panic("executeworker: workflow binding " + method + " must not be called during compile")
}

func (inertCallBridge) callResult(method, _ string, _ any) (any, error) {
	panic("executeworker: workflow binding " + method + " must not be called during compile")
}

type pipelineValue []map[string]any

func workerPipeline(items []any, callback func(any) (any, error), concurrency int) ([]map[string]any, error) {
	// Keep the version-1 validation messages and result shape while delegating
	// scheduling to the same kernel used by the canonical version-2 bindings.
	if concurrency <= 0 {
		return nil, errors.New("pipeline concurrency must be positive")
	}

	if callback == nil {
		return nil, errors.New("pipeline callback is required")
	}

	// The v1 API historically let callback panics escape. The canonical kernel
	// recovers them into item failures, so remember and re-panic after its
	// workers finish rather than changing the persisted v1 contract.
	var (
		panicOnce  sync.Once
		panicValue any
		panicked   bool
	)

	legacyCallback := func(value any) (result any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicOnce.Do(func() {
					panicValue = recovered
					panicked = true
				})

				result = nil
				err = errors.New("worker pipeline callback panicked")
			}
		}()

		return callback(value)
	}

	outcome, err := workflowkernel.Pipeline(
		context.Background(), items, []workflowkernel.Callback{legacyCallback}, concurrency,
	)

	if panicked {
		panic(panicValue)
	}

	if err != nil {
		return nil, oops.In("executeworker").Code("worker_pipeline").Wrapf(err, "run worker pipeline")
	}

	results := make([]map[string]any, len(outcome.Items))
	for index, item := range outcome.Items {
		message := item.Error
		if item.State == workflowkernel.StateNotStarted {
			message = "pipeline stopped before item was scheduled"
		}

		results[index] = map[string]any{"index": item.Index, "value": item.Value, "error": message}
	}

	return results, nil
}

func (caller *rpcCaller) call(method, name, query string, input any) any {
	value, err := caller.callResult(method, name, query, input)
	if err != nil {
		return rpcError(err.Error())
	}

	return value
}

func (caller *rpcCaller) callResult(method, name, query string, input any) (any, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode worker RPC input: %w", err)
	}

	response, err := caller.exchange(method, name, query, raw)
	if err != nil {
		return nil, err
	}

	value, err := decodeRPCValue(response)
	if err != nil {
		return nil, err
	}

	return value, nil
}

func (caller *rpcCaller) progress(event workflowprogress.Event) error {
	caller.mu.Lock()
	if caller.terminalErr != nil {
		err := caller.terminalErr
		caller.mu.Unlock()

		return err
	}

	caller.nextID++
	requestID := caller.nextID
	responseCh := make(chan Message, 1)
	caller.pending[requestID] = responseCh
	caller.mu.Unlock()

	request := newMessage("progress")
	request.ID, request.Progress = requestID, &event

	caller.writeMu.Lock()
	err := Write(caller.out, &request)
	caller.writeMu.Unlock()

	if err != nil {
		caller.mu.Lock()
		delete(caller.pending, requestID)
		caller.mu.Unlock()

		return oops.In("executeworker").Code("write_progress").Wrapf(err, "write progress request")
	}

	response := <-responseCh
	if response.Error != "" {
		return oops.In("executeworker").Code("progress_rejected").
			Wrapf(errors.New(response.Error), "progress request rejected")
	}

	return nil
}

func (caller *rpcCaller) exchange(method, name, query string, input json.RawMessage) (*Message, error) {
	caller.mu.Lock()
	if caller.terminalErr != nil {
		err := caller.terminalErr
		caller.mu.Unlock()

		return nil, err
	}

	caller.nextID++
	requestID := caller.nextID
	responseCh := make(chan Message, 1)
	caller.pending[requestID] = responseCh
	caller.mu.Unlock()

	request := newMessage("rpc")
	request.ID, request.Method, request.Name = requestID, method, name
	request.Query, request.Input = query, input

	caller.writeMu.Lock()
	err := Write(caller.out, &request)
	caller.writeMu.Unlock()

	if err != nil {
		caller.mu.Lock()
		delete(caller.pending, requestID)
		caller.mu.Unlock()

		return nil, err
	}

	response := <-responseCh
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}

	return &response, nil
}

func (caller *rpcCaller) readResponses(cancelEval context.CancelFunc) {
	defer cancelEval()

	for {
		response, err := Read(caller.in)
		if err != nil {
			caller.failPending(err)

			return
		}

		if response.Type != "rpc_result" && response.Type != "progress_result" {
			continue
		}

		caller.mu.Lock()
		responseCh := caller.pending[response.ID]
		delete(caller.pending, response.ID)
		caller.mu.Unlock()

		if responseCh != nil {
			responseCh <- response
		}
	}
}

func (caller *rpcCaller) failPending(err error) {
	caller.mu.Lock()
	if caller.terminalErr == nil {
		caller.terminalErr = err
	}

	pending := caller.pending
	caller.pending = make(map[uint64]chan Message)
	caller.mu.Unlock()

	for _, responseCh := range pending {
		response := newMessage("rpc_result")

		response.Error = err.Error()
		responseCh <- response
	}
}

func decodeRPCValue(response *Message) (any, error) {
	if string(response.Value) == jsonNullValue {
		return json.RawMessage(jsonNullValue), nil
	}

	if response.ValueKind == toolCallResultKind {
		var value ToolCallResult
		if err := json.Unmarshal(response.Value, &value); err != nil {
			return nil, fmt.Errorf("decode tool call result: %w", err)
		}

		return value, nil
	}

	var value any

	decoder := json.NewDecoder(bytes.NewReader(response.Value))
	decoder.UseNumber()

	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode RPC result: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode RPC result: trailing data")
	}

	value, err := normalizeRPCNumbers(value)
	if err != nil {
		return nil, fmt.Errorf("normalize RPC result: %w", err)
	}

	return value, nil
}

// normalizeRPCNumbers keeps UseNumber's exact integer decoding without exposing
// json.Number (a named string type) to guest code that expects native numbers.
func normalizeRPCNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := strconv.Atoi(typed.String()); err == nil {
			return integer, nil
		}

		decimal, err := typed.Float64()
		if err != nil {
			return nil, fmt.Errorf("convert JSON number %q: %w", typed, err)
		}

		return decimal, nil
	case []any:
		return normalizeRPCSlice(typed)
	case map[string]any:
		return normalizeRPCMap(typed)
	}

	return value, nil
}

func normalizeRPCSlice(values []any) ([]any, error) {
	for index := range values {
		normalized, err := normalizeRPCNumbers(values[index])
		if err != nil {
			return nil, err
		}

		values[index] = normalized
	}

	return values, nil
}

func normalizeRPCMap(values map[string]any) (map[string]any, error) {
	for key := range values {
		normalized, err := normalizeRPCNumbers(values[key])
		if err != nil {
			return nil, err
		}

		values[key] = normalized
	}

	return values, nil
}

func rpcError(message string) map[string]any {
	return map[string]any{errorKey: message, "is_error": true}
}

func resultMessage(result mvmhost.Result, evalErr error) Message {
	response := newMessage("result")

	response.Stdout, response.Stderr = result.Stdout, result.Stderr
	if pipeline, ok := result.Value.(pipelineValue); ok {
		result.Value = []map[string]any(pipeline)
		response.ValueKind = pipelineResultKind
	}

	if evalErr != nil {
		response.Error = evalErr.Error()

		if normalized, ok := errors.AsType[*mvmhost.EvalError](evalErr); ok {
			response.ErrorKind = string(normalized.Kind)
			response.ExitCode = normalized.ExitCode
		}

		return response
	}

	value, err := json.Marshal(result.Value)
	if err != nil {
		response.Error = fmt.Sprintf("encode execute result: %v", err)
		response.ErrorKind = string(mvmhost.ErrorKindRuntime)
	} else {
		if _, ok := result.Value.(ToolCallResult); ok {
			response.ValueKind = toolCallResultKind
		}

		response.Value = value
	}

	return response
}
