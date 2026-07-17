package executeworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/omarluq/librecode/internal/mvmhost"
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

	caller := &rpcCaller{
		in: input, out: output, pending: make(map[uint64]chan Message), nextID: 0, terminalErr: nil,
		mu: sync.Mutex{}, writeMu: sync.Mutex{},
	}
	go caller.readResponses()

	bindings, err := workerBindings(&request, caller)
	if err != nil {
		return err
	}

	result, evalErr := mvmhost.New().Eval(context.Background(), mvmhost.Request{
		Bindings: bindings, Name: request.Name, Source: request.Source,
	})

	response := resultMessage(result, evalErr)

	return Write(output, &response)
}

func workerBindings(request *Message, caller *rpcCaller) (mvmhost.Bindings, error) {
	if request.Mode == "" || request.Mode == "execute" {
		return mvmhost.Bindings{"tools": {
			"Search":   func(query string) any { return caller.call("search", "", query, nil) },
			"Describe": func(name string) any { return caller.call("describe", name, "", nil) },
			"Call":     func(name string, input any) any { return caller.call("call", name, "", input) },
		}}, nil
	}

	if request.Mode != "workflow" {
		return nil, fmt.Errorf("unknown execute worker mode %q", request.Mode)
	}

	var arguments map[string]any
	if len(request.Arguments) > 0 && string(request.Arguments) != jsonNullValue {
		if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
			return nil, fmt.Errorf("decode workflow arguments: %w", err)
		}
	}

	return mvmhost.Bindings{"librecode/workflow": {
		"Arguments": arguments,
		"Agent": func(prompt string, options ...map[string]any) (any, error) {
			return caller.callResult("workflow_agent", "", "", map[string]any{"prompt": prompt, "options": options})
		},
		"Wait": func(taskID string) (any, error) {
			return caller.callResult("workflow_wait", taskID, "", nil)
		},
		"List": func() (any, error) { return caller.callResult("workflow_list", "", "", nil) },
		"Cancel": func(taskID string) (any, error) {
			return caller.callResult("workflow_cancel", taskID, "", nil)
		},
		"Pipeline": func(items []any, callback func(any) (any, error), concurrency int) (any, error) {
			results, err := workerPipeline(items, callback, concurrency)

			return pipelineValue(results), err
		},
	}}, nil
}

type pipelineValue []map[string]any

func workerPipeline(items []any, callback func(any) (any, error), concurrency int) ([]map[string]any, error) {
	if concurrency <= 0 {
		return nil, errors.New("pipeline concurrency must be positive")
	}

	if callback == nil {
		return nil, errors.New("pipeline callback is required")
	}

	results := make([]map[string]any, len(items))
	for index := range results {
		results[index] = map[string]any{
			"index": index, "value": nil, "error": "pipeline stopped before item was scheduled",
		}
	}

	var (
		state struct {
			sync.Mutex
			next    int
			stopped bool
		}
		workers sync.WaitGroup
	)
	workers.Add(min(concurrency, len(items)))

	for range min(concurrency, len(items)) {
		go runWorkerPipeline(items, callback, results, &state, &workers)
	}

	workers.Wait()

	return results, nil
}

func runWorkerPipeline(
	items []any,
	callback func(any) (any, error),
	results []map[string]any,
	state *struct {
		sync.Mutex
		next    int
		stopped bool
	},
	workers *sync.WaitGroup,
) {
	defer workers.Done()

	for {
		state.Lock()
		if state.stopped || state.next >= len(items) {
			state.Unlock()

			return
		}

		index := state.next
		state.next++
		state.Unlock()

		value, err := callback(items[index])

		message := ""
		if err != nil {
			message = err.Error()

			state.Lock()
			state.stopped = true
			state.Unlock()
		}

		results[index] = map[string]any{"index": index, "value": value, "error": message}
	}
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

func (caller *rpcCaller) readResponses() {
	for {
		response, err := Read(caller.in)
		if err != nil {
			caller.failPending(err)

			return
		}

		if response.Type != "rpc_result" {
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
	if err := json.Unmarshal(response.Value, &value); err != nil {
		return nil, fmt.Errorf("decode RPC result: %w", err)
	}

	return value, nil
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

		var normalized *mvmhost.EvalError
		if errors.As(evalErr, &normalized) {
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
