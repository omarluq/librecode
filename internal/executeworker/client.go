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

	"github.com/omarluq/librecode/internal/mvmhost"
)

// RPCHandler handles a callback request received from an execute worker.
type RPCHandler func(context.Context, *Message) (any, error)

// Client evaluates source in a separate worker process.
type Client struct {
	Handler    RPCHandler
	Executable string
}

type workerProcess struct {
	killErr  error
	cmd      *exec.Cmd
	killOnce sync.Once
}

// Request describes one isolated MVM evaluation.
type Request struct {
	Arguments   any
	Mode        string
	Name        string
	Source      string
	SourceLimit int
	OutputLimit int
}

// Eval evaluates provider-facing source, forwarding callback requests to Handler.
func (client Client) Eval(ctx context.Context, source string) (mvmhost.Result, error) {
	return client.EvalRequest(ctx, &Request{
		Arguments: nil, Mode: "execute", Name: "execute.go", Source: source, SourceLimit: 0, OutputLimit: 0,
	})
}

// EvalRequest evaluates source in the requested worker mode.
func (client Client) EvalRequest(ctx context.Context, eval *Request) (mvmhost.Result, error) {
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

	if eval == nil {
		return mvmhost.Result{}, worker.abort(errors.New("execute worker request is required"))
	}

	request := newMessage("eval")
	request.Mode, request.Name, request.Source = eval.Mode, eval.Name, eval.Source

	request.SourceLimit, request.OutputLimit = eval.SourceLimit, eval.OutputLimit
	if request.Arguments, err = json.Marshal(eval.Arguments); err != nil {
		return mvmhost.Result{}, worker.abort(fmt.Errorf("encode worker arguments: %w", err))
	}

	if err = Write(stdin, &request); err != nil {
		return mvmhost.Result{}, worker.abort(err)
	}

	return client.readMessages(ctx, worker, stdin, stdout)
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
		return nil, nil, nil, fmt.Errorf("open execute worker stdout: %w", err)
	}

	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("start execute worker: %w", err)
	}

	return &workerProcess{cmd: cmd, killOnce: sync.Once{}, killErr: nil}, stdin, stdout, nil
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

func (client Client) readMessages(
	ctx context.Context,
	worker *workerProcess,
	stdin io.WriteCloser,
	stdout io.Reader,
) (mvmhost.Result, error) {
	var (
		writes    sync.Mutex
		callbacks sync.WaitGroup
	)

	for {
		message, err := Read(stdout)
		if err != nil {
			return mvmhost.Result{}, worker.readError(ctx, err)
		}

		switch message.Type {
		case "rpc":
			callbacks.Add(1)
			go func(rpc Message) {
				defer callbacks.Done()

				response := client.rpcResponse(ctx, &rpc)

				writes.Lock()
				defer writes.Unlock()

				if replyErr := Write(stdin, &response); replyErr != nil {
					worker.kill()
				}
			}(message)
		case "result":
			callbacks.Wait()

			return finishResult(worker, stdin, &message)
		default:
			return mvmhost.Result{}, worker.abort(
				fmt.Errorf("unexpected execute worker message %q", message.Type),
			)
		}
	}
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

func finishResult(worker *workerProcess, stdin io.Closer, message *Message) (mvmhost.Result, error) {
	if err := stdin.Close(); err != nil {
		return mvmhost.Result{}, worker.abort(fmt.Errorf("close execute worker stdin: %w", err))
	}

	if err := worker.cmd.Wait(); err != nil {
		return mvmhost.Result{}, fmt.Errorf("wait for execute worker: %w", err)
	}

	result := mvmhost.Result{Stdout: message.Stdout, Stderr: message.Stderr, Value: nil}
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

func (worker *workerProcess) kill() {
	worker.killOnce.Do(func() {
		worker.killErr = worker.cmd.Process.Kill()
	})
}

func (worker *workerProcess) abort(cause error) error {
	worker.kill()

	waitErr := worker.cmd.Wait()
	if worker.killErr != nil && !errors.Is(worker.killErr, os.ErrProcessDone) {
		return errors.Join(cause, fmt.Errorf("kill execute worker: %w", worker.killErr), waitErr)
	}

	return errors.Join(cause, waitErr)
}

func (worker *workerProcess) readError(ctx context.Context, readErr error) error {
	worker.kill()
	waitErr := worker.cmd.Wait()

	if ctx.Err() != nil {
		return canceledError(ctx.Err())
	}

	if waitErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("read execute worker: %w", readErr)
	}

	return fmt.Errorf("execute worker exited without result: %w", waitErr)
}

func canceledError(err error) error {
	return &mvmhost.EvalError{Err: err, Kind: mvmhost.ErrorKindCanceled, ExitCode: 0}
}
