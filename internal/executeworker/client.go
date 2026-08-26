package executeworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/workflowprogress"
)

// RPCHandler handles a callback request received from an execute worker.
type RPCHandler func(context.Context, *Message) (any, error)

// Client evaluates source in a separate worker process.
type Client struct {
	Handler    RPCHandler
	Progress   workflowprogress.Sink
	Executable string
}

const (
	maxWorkerStderrSize       = 64 << 10
	maxConcurrentRPCCallbacks = 4
	maxTotalRPCCallbacks      = 256
	stderrTruncatedMarker     = "\n[execute worker stderr truncated]"
)

type cappedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - len(buffer.data)
	if remaining > 0 {
		buffer.data = append(buffer.data, data[:min(len(data), remaining)]...)
	}

	if len(data) > remaining {
		buffer.truncated = true
	}

	return len(data), nil
}

func (buffer *cappedBuffer) String() string {
	if buffer == nil {
		return ""
	}

	if buffer.truncated {
		return string(buffer.data) + stderrTruncatedMarker
	}

	return string(buffer.data)
}

type workerProcess struct {
	killErr  error
	cmd      *exec.Cmd
	stderr   *cappedBuffer
	killOnce sync.Once
}

// Request describes one isolated MVM evaluation.
type Request struct {
	Arguments       any
	Profile         guestapi.Profile
	GuestAPIVersion guestapi.Version
	Name            string
	Source          string
}

// EvalRequest evaluates source in the requested worker mode.
func (client Client) EvalRequest(ctx context.Context, eval *Request) (mvmhost.Result, error) {
	request, err := evaluationMessage(eval)
	if err != nil {
		return mvmhost.Result{}, err
	}

	worker, stdin, stdout, err := client.startWorker()
	if err != nil {
		return mvmhost.Result{}, err
	}

	stopCancellation := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			worker.kill()
		case <-stopCancellation:
		}
	}()

	defer close(stopCancellation)

	if request.Arguments, err = json.Marshal(eval.Arguments); err != nil {
		return mvmhost.Result{}, worker.abort(fmt.Errorf("encode worker arguments: %w", err))
	}

	if err = Write(stdin, request); err != nil {
		return mvmhost.Result{}, worker.abort(err)
	}

	return client.readMessages(ctx, worker, stdin, stdout)
}

func evaluationMessage(eval *Request) (*Message, error) {
	if eval == nil {
		return nil, errors.New("execute worker request is required")
	}

	request := newMessage("eval")
	request.Profile, request.GuestAPI = eval.Profile, eval.GuestAPIVersion
	request.Name, request.Source = eval.Name, eval.Source

	if request.Profile == "" || request.GuestAPI == "" {
		return nil, errors.New(
			"execute worker profile and guest API version must be provided together",
		)
	}

	if err := guestapi.ValidateWorkerContract(request.Profile, request.GuestAPI); err != nil {
		return nil, fmt.Errorf("validate execute worker contract: %w", err)
	}

	return &request, nil
}

func (client Client) startWorker() (*workerProcess, io.WriteCloser, io.ReadCloser, error) {
	executable, err := client.executablePath()
	if err != nil {
		return nil, nil, nil, err
	}

	// Constructing Cmd directly avoids shell interpretation. executablePath only
	// permits an absolute path to a regular file.
	cmd := &exec.Cmd{Path: executable, Args: []string{executable, "__execute-worker"}, Env: os.Environ()}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open execute worker stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, errors.Join(
			fmt.Errorf("open execute worker stdout: %w", err),
			stdin.Close(),
		)
	}

	stderr := &cappedBuffer{data: nil, limit: maxWorkerStderrSize, truncated: false}
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return nil, nil, nil, errors.Join(
			fmt.Errorf("start execute worker: %w", err),
			stdin.Close(),
			stdout.Close(),
		)
	}

	return &workerProcess{cmd: cmd, stderr: stderr, killOnce: sync.Once{}, killErr: nil}, stdin, stdout, nil
}

func (client Client) executablePath() (string, error) {
	executable := client.Executable
	if executable == "" {
		var err error

		executable, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve execute worker executable: %w", err)
		}
	}

	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return "", fmt.Errorf("execute worker path must be an absolute clean path: %q", executable)
	}

	info, err := os.Stat(executable)
	if err != nil {
		return "", fmt.Errorf("inspect execute worker executable: %w", err)
	}

	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("execute worker path is not a regular file: %q", executable)
	}

	return executable, nil
}

type rpcCallbacks struct {
	cancel context.CancelFunc
	slots  chan struct{}
	done   []<-chan struct{}
}

func (client Client) readMessages(
	ctx context.Context,
	worker *workerProcess,
	stdin io.WriteCloser,
	stdout io.Reader,
) (mvmhost.Result, error) {
	callbackCtx, cancelCallbacks := context.WithCancel(ctx)

	callbacks := rpcCallbacks{
		cancel: cancelCallbacks,
		slots:  make(chan struct{}, maxConcurrentRPCCallbacks), done: nil,
	}
	defer cancelCallbacks()

	var (
		writes           sync.Mutex
		progressSequence uint64
	)

	for {
		message, err := Read(stdout)
		if err != nil {
			client.stopRPCCallbacks(worker, &callbacks)

			return mvmhost.Result{}, worker.readError(ctx, err)
		}

		switch message.Type {
		case "progress":
			if err := client.handleProgress(callbackCtx, stdin, &message, &writes, &progressSequence); err != nil {
				client.stopRPCCallbacks(worker, &callbacks)

				return mvmhost.Result{}, worker.abort(err)
			}
		case "rpc":
			if err := client.startRPCCallback(callbackCtx, worker, stdin, &message, &callbacks, &writes); err != nil {
				client.stopRPCCallbacks(worker, &callbacks)

				return mvmhost.Result{}, worker.abort(err)
			}
		case "result":
			if err := waitForCallbacks(ctx, callbacks.done); err != nil {
				client.stopRPCCallbacks(worker, &callbacks)

				return mvmhost.Result{}, worker.abort(err)
			}

			return finishResult(ctx, worker, stdin, &message)
		default:
			client.stopRPCCallbacks(worker, &callbacks)

			return mvmhost.Result{}, worker.abort(
				fmt.Errorf("unexpected execute worker message %q", message.Type),
			)
		}
	}
}

func (client Client) handleProgress(
	ctx context.Context,
	stdin io.Writer,
	message *Message,
	writes *sync.Mutex,
	previous *uint64,
) error {
	if message.Progress.Sequence != *previous+1 {
		return fmt.Errorf(
			"execute worker progress sequence %d follows %d",
			message.Progress.Sequence,
			*previous,
		)
	}

	response := newMessage("progress_result")
	response.ID = message.ID

	if client.Progress != nil {
		if progressErr := client.Progress(ctx, *message.Progress); progressErr != nil {
			response.Error = progressErr.Error()
		}
	}

	if ctx.Err() != nil {
		return canceledError(ctx.Err())
	}

	writes.Lock()
	defer writes.Unlock()

	if err := Write(stdin, &response); err != nil {
		return err
	}

	*previous = message.Progress.Sequence

	return nil
}

func (client Client) startRPCCallback(
	ctx context.Context,
	worker *workerProcess,
	stdin io.Writer,
	message *Message,
	callbacks *rpcCallbacks,
	writes *sync.Mutex,
) error {
	if len(callbacks.done) >= maxTotalRPCCallbacks {
		return fmt.Errorf(
			"execute worker RPC callback limit exceeded (maximum %d)",
			maxTotalRPCCallbacks,
		)
	}

	select {
	case callbacks.slots <- struct{}{}:
	case <-ctx.Done():
		return canceledError(ctx.Err())
	}

	callbackDone := make(chan struct{})
	callbacks.done = append(callbacks.done, callbackDone)
	rpc := *message

	go func() {
		defer close(callbackDone)
		defer func() { <-callbacks.slots }()

		response := client.rpcResponse(ctx, &rpc)
		if ctx.Err() != nil {
			return
		}

		writes.Lock()
		defer writes.Unlock()

		if ctx.Err() != nil {
			return
		}

		if Write(stdin, &response) != nil {
			worker.kill()
		}
	}()

	return nil
}

func (client Client) stopRPCCallbacks(worker *workerProcess, callbacks *rpcCallbacks) {
	callbacks.cancel()
	worker.kill()

	for _, callbackDone := range callbacks.done {
		<-callbackDone
	}
}

func waitForCallbacks(ctx context.Context, callbacks []<-chan struct{}) error {
	for _, callbackDone := range callbacks {
		select {
		case <-callbackDone:
		case <-ctx.Done():
			return canceledError(ctx.Err())
		}
	}

	return nil
}

func (client Client) rpcResponse(ctx context.Context, message *Message) Message {
	var (
		value  any
		rpcErr error
	)
	if client.Handler == nil {
		rpcErr = errors.New("execute worker RPC handler is not configured")
	} else {
		value, rpcErr = client.Handler(ctx, message)
	}

	response := newMessage("rpc_result")

	response.ID = message.ID
	if rpcErr != nil {
		response.Error = rpcErr.Error()
	}

	if _, ok := value.(ToolCallResult); ok {
		response.ValueKind = toolCallResultKind
	}

	if response.Value, rpcErr = json.Marshal(value); rpcErr != nil && response.Error == "" {
		response.Error = rpcErr.Error()
	}

	return response
}

func finishResult(ctx context.Context,
	worker *workerProcess, stdin io.Closer, message *Message) (mvmhost.Result, error) {
	if err := stdin.Close(); err != nil {
		return mvmhost.Result{}, worker.abort(fmt.Errorf("close execute worker stdin: %w", err))
	}

	if err := waitForWorker(ctx, worker); err != nil {
		return mvmhost.Result{}, err
	}

	result := mvmhost.Result{
		Value: nil, ValueKind: message.ValueKind, Stdout: message.Stdout, Stderr: message.Stderr,
	}
	if len(message.Value) > 0 {
		switch {
		case string(message.Value) == jsonNullValue:
			result.Value = json.RawMessage(jsonNullValue)
		case message.ValueKind == toolCallResultKind:
			var nested ToolCallResult
			if err := json.Unmarshal(message.Value, &nested); err != nil {
				return result, fmt.Errorf("decode execute worker tool result: %w", err)
			}

			result.Value = nested
		default:
			if err := json.Unmarshal(message.Value, &result.Value); err != nil {
				return result, fmt.Errorf("decode execute worker result: %w", err)
			}
		}
	}

	if message.Error == "" {
		return result, nil
	}

	return result, &mvmhost.EvalError{
		Err: errors.New(message.Error), Kind: mvmhost.ErrorKind(message.ErrorKind), ExitCode: message.ExitCode,
	}
}

func waitForWorker(ctx context.Context, worker *workerProcess) error {
	if err := worker.cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return canceledError(ctx.Err())
		}

		return worker.commandError("wait for execute worker", err)
	}

	return nil
}

func (worker *workerProcess) kill() {
	worker.killOnce.Do(func() {
		worker.killErr = worker.cmd.Process.Kill()
	})
}

func (worker *workerProcess) abort(cause error) error {
	worker.kill()

	waitErr := worker.cmd.Wait()
	errs := []error{cause}

	if worker.killErr != nil && !errors.Is(worker.killErr, os.ErrProcessDone) {
		errs = append(errs, fmt.Errorf("kill execute worker: %w", worker.killErr))
	}

	if waitErr != nil {
		errs = append(errs, worker.commandError("wait for execute worker", waitErr))
	}

	return errors.Join(errs...)
}

func (worker *workerProcess) readError(ctx context.Context, readErr error) error {
	if !errors.Is(readErr, io.EOF) || ctx.Err() != nil {
		worker.kill()
	}

	waitErr := worker.cmd.Wait()

	if ctx.Err() != nil {
		return canceledError(ctx.Err())
	}

	if !errors.Is(readErr, io.EOF) {
		if waitErr != nil {
			return errors.Join(
				fmt.Errorf("read execute worker: %w", readErr),
				worker.commandError("wait for execute worker", waitErr),
			)
		}

		return fmt.Errorf("read execute worker: %w", readErr)
	}

	if waitErr != nil {
		return worker.commandError("execute worker exited without result", waitErr)
	}

	return worker.commandError("execute worker exited without result", nil)
}

func (worker *workerProcess) commandError(message string, err error) error {
	stderr := ""
	if worker.stderr != nil {
		stderr = worker.stderr.String()
	}

	if stderr == "" {
		if err == nil {
			return errors.New(message)
		}

		return fmt.Errorf("%s: %w", message, err)
	}

	if err == nil {
		return fmt.Errorf("%s: stderr: %s", message, stderr)
	}

	return fmt.Errorf("%s: %w: stderr: %s", message, err, stderr)
}

func canceledError(err error) error {
	return &mvmhost.EvalError{Err: err, Kind: mvmhost.ErrorKindCanceled, ExitCode: 0}
}
