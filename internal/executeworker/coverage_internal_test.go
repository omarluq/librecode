package executeworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorWriter struct{ calls int }

func (w *errorWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return len(p), nil
	}

	return 0, errors.New("write failed")
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

type closeError struct{ err error }

func (c closeError) Close() error { return c.err }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestProtocolErrorBranches(t *testing.T) {
	t.Parallel()

	_, err := Read(strings.NewReader("x"))
	require.ErrorContains(t, err, "frame size")

	for _, size := range []uint32{0, MaxFrameSize + 1} {
		var input bytes.Buffer
		require.NoError(t, binary.Write(&input, binary.BigEndian, size))
		_, err = Read(&input)
		require.ErrorContains(t, err, "exceeds limit")
	}

	var truncated bytes.Buffer
	require.NoError(t, binary.Write(&truncated, binary.BigEndian, uint32(2)))
	truncated.WriteByte('{')
	_, err = Read(&truncated)
	require.ErrorContains(t, err, "read execute worker frame")

	for _, payload := range []struct {
		value string
		size  uint32
	}{
		{value: "{", size: 1},
		{value: `{`, size: 1},
	} {
		var input bytes.Buffer
		require.NoError(t, binary.Write(&input, binary.BigEndian, payload.size))
		input.WriteString(payload.value)
		_, err = Read(&input)
		require.Error(t, err)
	}

	require.Error(t, Write(io.Discard, nil))
	require.Error(t, Write(io.Discard, ptrMessage("")))
	require.ErrorContains(t, Write(shortWriter{}, ptrMessage("rpc")), "frame size")
	require.ErrorContains(t, Write(&errorWriter{calls: 0}, ptrMessage("rpc")), "frame")

	var partial bytes.Buffer

	writer := struct{ io.Writer }{Writer: &partial}
	require.NoError(t, Write(writer, ptrMessage("rpc")))

	_, err = Read(&partial)
	require.NoError(t, err)
}

func ptrMessage(kind string) *Message {
	message := newMessage(kind)

	return &message
}

func TestWorkerPipelineBranches(t *testing.T) {
	t.Parallel()

	_, err := workerPipeline(nil, func(any) (any, error) { return 0, nil }, 0)
	require.EqualError(t, err, "pipeline concurrency must be positive")
	_, err = workerPipeline(nil, nil, 1)
	require.EqualError(t, err, "pipeline callback is required")

	results, err := workerPipeline([]any{1, 2, 3}, func(value any) (any, error) {
		if value == 2 {
			return nil, errors.New("stop")
		}

		integer, integerOK := value.(int)
		require.True(t, integerOK)

		return integer * 2, nil
	}, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, results[0]["value"])
	assert.Equal(t, "stop", results[1]["error"])
	assert.Contains(t, results[2]["error"], "stopped")

	empty, err := workerPipeline(nil, func(any) (any, error) { return 0, nil }, 2)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestWorkerBindingsModes(t *testing.T) {
	t.Parallel()

	caller := newRPCCaller()
	bindings, err := workerBindings(ptrMessage("eval"), caller)
	require.NoError(t, err)
	assert.Contains(t, bindings, "tools")

	request := ptrMessage("eval")
	request.Mode = "bad"
	_, err = workerBindings(request, caller)
	require.ErrorContains(t, err, "unknown")

	request.Mode = "workflow"
	request.Arguments = json.RawMessage(`{`)
	_, err = workerBindings(request, caller)
	require.ErrorContains(t, err, "decode workflow arguments")

	for _, arguments := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(`{"x":1}`)} {
		request.Arguments = arguments
		bindings, err = workerBindings(request, caller)
		require.NoError(t, err)
		assert.Contains(t, bindings, "librecode/workflow")
	}
}

func TestRPCCallerExchangeAndFailures(t *testing.T) {
	t.Parallel()

	parentRead, workerWrite := io.Pipe()
	workerRead, parentWrite := io.Pipe()
	caller := newRPCCaller()

	caller.in, caller.out = workerRead, workerWrite
	go caller.readResponses()

	writeErrors := make(chan error, 1)

	go func() {
		request, readErr := Read(parentRead)
		if readErr != nil {
			return
		}

		ignored := newMessage("noise")
		if writeErr := Write(parentWrite, &ignored); writeErr != nil {
			writeErrors <- writeErr

			return
		}

		response := newMessage("rpc_result")
		response.ID = request.ID
		response.Value = json.RawMessage(`{"ok":true}`)

		writeErrors <- Write(parentWrite, &response)
	}()

	value, err := caller.callResult("call", "name", "query", map[string]any{"x": 1})
	require.NoError(t, err)
	require.NoError(t, <-writeErrors)
	assert.Equal(t, map[string]any{"ok": true}, value)
	require.NoError(t, parentWrite.Close())
	require.NoError(t, parentRead.Close())
	require.NoError(t, workerWrite.Close())

	badInput := newRPCCaller()
	_, err = badInput.callResult("call", "", "", func() {})
	require.ErrorContains(t, err, "encode worker RPC input")

	terminal := errors.New("terminal")
	failed := newRPCCaller()
	failed.terminalErr = terminal
	_, err = failed.exchange("call", "", "", nil)
	require.ErrorIs(t, err, terminal)

	writeFailed := newRPCCaller()
	writeFailed.out = shortWriter{}
	_, err = writeFailed.exchange("call", "", "", nil)
	require.Error(t, err)
	assert.Empty(t, writeFailed.pending)

	responseCh := make(chan Message, 1)
	pending := newRPCCaller()
	pending.pending[1] = responseCh
	pending.failPending(errors.New("first"))
	pending.failPending(errors.New("second"))
	require.EqualError(t, pending.terminalErr, "first")
	assert.Equal(t, "first", (<-responseCh).Error)
}

func TestRPCValuesAndResultMessages(t *testing.T) {
	t.Parallel()

	value, err := decodeRPCValue(messageWithValue("", json.RawMessage("null")))
	require.NoError(t, err)

	rawValue, rawValueOK := value.(json.RawMessage)
	require.True(t, rawValueOK)
	assert.JSONEq(t, "null", string(rawValue))

	toolValue := ToolCallResult{Details: nil, Error: "nested", Content: nil, IsError: true}
	raw, err := json.Marshal(toolValue)
	require.NoError(t, err)
	value, err = decodeRPCValue(messageWithValue(toolCallResultKind, raw))
	require.NoError(t, err)
	assert.Equal(t, toolValue, value)

	_, err = decodeRPCValue(messageWithValue(toolCallResultKind, json.RawMessage("{")))
	require.ErrorContains(t, err, "decode tool call result")
	_, err = decodeRPCValue(messageWithValue("", json.RawMessage("{")))
	require.ErrorContains(t, err, "decode RPC result")

	isError, isErrorOK := rpcError("bad")["is_error"].(bool)
	require.True(t, isErrorOK)
	assert.True(t, isError)

	message := resultMessage(mvmhost.Result{
		Value: pipelineValue{{"x": 1}}, ValueKind: "", Stdout: "out", Stderr: "err",
	}, nil)
	assert.Equal(t, pipelineResultKind, message.ValueKind)
	assert.NotEmpty(t, message.Value)
	message = resultMessage(mvmResult(toolValue), nil)
	assert.Equal(t, toolCallResultKind, message.ValueKind)
	message = resultMessage(mvmResult(func() {}), nil)
	assert.Equal(t, string(mvmhost.ErrorKindRuntime), message.ErrorKind)

	evalErr := &mvmhost.EvalError{Err: errors.New("boom"), Kind: mvmhost.ErrorKindCanceled, ExitCode: 7}
	message = resultMessage(mvmResult(nil), evalErr)
	assert.Equal(t, 7, message.ExitCode)
	message = resultMessage(mvmResult(nil), errors.New("plain"))
	assert.Equal(t, "plain", message.Error)
}

func TestServeBranchesInProcess(t *testing.T) {
	t.Parallel()

	require.Error(t, Serve(strings.NewReader("bad"), io.Discard))

	wrongMode := newMessage("eval")
	wrongMode.Mode = "wrong"

	for _, request := range []Message{newMessage("wrong"), wrongMode} {
		var input bytes.Buffer
		require.NoError(t, Write(&input, &request))
		require.Error(t, Serve(&input, io.Discard))
	}

	request := newMessage("eval")
	request.Source = "1"

	var input, output bytes.Buffer
	require.NoError(t, Write(&input, &request))
	require.NoError(t, Serve(&input, &output))
	response, err := Read(&output)
	require.NoError(t, err)
	assert.Equal(t, "result", response.Type)
}

func startedProcess(t *testing.T, script string) *workerProcess {
	t.Helper()

	var cmd *exec.Cmd

	switch script {
	case "exit 0":
		cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 0")
	case "exit 2":
		cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 2")
	case "exit 4":
		cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 4")
	case "sleep 10":
		cmd = exec.CommandContext(t.Context(), "sh", "-c", "sleep 10")
	default:
		require.FailNow(t, "unsupported test process", script)
	}

	require.NoError(t, cmd.Start())

	return &workerProcess{killErr: nil, cmd: cmd, killOnce: sync.Once{}}
}

func TestEvalRequestValidationInProcess(t *testing.T) {
	t.Setenv("LIBRECODE_EXECUTEWORKER_TEST_HELPER", "1")

	client := newClient(nil, os.Args[0])

	_, err := client.EvalRequest(t.Context(), nil)
	require.ErrorContains(t, err, "request is required")
	_, err = client.EvalRequest(t.Context(), requestWithArguments(func() {}))
	require.ErrorContains(t, err, "encode worker arguments")
}

func TestClientPrivateBranches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	relative := newClient(nil, "relative")
	_, err := relative.executablePath()
	require.ErrorContains(t, err, "absolute clean path")
	_, err = (newClient(nil, filepath.Join(dir, "missing"))).executablePath()
	require.ErrorContains(t, err, "inspect")
	_, err = (newClient(nil, dir)).executablePath()
	require.ErrorContains(t, err, "not a regular file")

	file := filepath.Join(dir, "worker")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	path, err := (newClient(nil, file)).executablePath()
	require.NoError(t, err)
	assert.Equal(t, file, path)
	path, err = newClient(nil, "").executablePath()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(path))

	response := newClient(nil, "").rpcResponse(t.Context(), messageWithID(2))
	assert.Contains(t, response.Error, "not configured")

	client := newClient(func(context.Context, *Message) (any, error) { return func() {}, nil }, "")
	response = client.rpcResponse(t.Context(), ptrMessage(""))
	assert.NotEmpty(t, response.Error)

	client.Handler = func(context.Context, *Message) (any, error) { return nil, errors.New("handler") }
	response = client.rpcResponse(t.Context(), ptrMessage(""))
	assert.Equal(t, "handler", response.Error)

	unexpected := newMessage("unexpected")

	var output bytes.Buffer
	require.NoError(t, Write(&output, &unexpected))
	worker := startedProcess(t, "sleep 10")
	_, err = newClient(nil, "").readMessages(t.Context(), worker, nopWriteCloser{io.Discard}, &output)
	require.ErrorContains(t, err, "unexpected execute worker message")
}

func TestReadMessagesCallbackWaitHonorsCancellation(t *testing.T) {
	t.Parallel()

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	client := newClient(func(context.Context, *Message) (any, error) {
		close(callbackStarted)
		<-releaseCallback

		return "released", nil
	}, "")

	var output bytes.Buffer

	rpc := newMessage("rpc")
	rpc.ID = 1
	require.NoError(t, Write(&output, &rpc))

	result := newMessage("result")
	require.NoError(t, Write(&output, &result))

	ctx, cancel := context.WithCancel(t.Context())
	worker := startedProcess(t, "sleep 10")
	resultCh := make(chan error, 1)

	go func() {
		_, err := client.readMessages(ctx, worker, nopWriteCloser{io.Discard}, &output)
		resultCh <- err
	}()

	<-callbackStarted
	cancel()

	select {
	case err := <-resultCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-t.Context().Done():
		require.FailNow(t, "callback wait did not honor cancellation")
	}

	close(releaseCallback)
}

func TestFinishResultBranches(t *testing.T) {
	t.Parallel()

	worker := startedProcess(t, "sleep 10")
	_, err := finishResult(t.Context(), worker, closeError{err: errors.New("close")}, ptrMessage("result"))
	require.ErrorContains(t, err, "close execute worker stdin")

	cases := []struct {
		check   func(mvmhost.Result, error)
		message Message
	}{
		{message: *messageWithValue("", json.RawMessage("null")), check: func(result mvmhost.Result, err error) {
			require.NoError(t, err)

			rawResult, rawResultOK := result.Value.(json.RawMessage)
			require.True(t, rawResultOK)
			assert.JSONEq(t, "null", string(rawResult))
		}},
		{message: *messageWithValue("", json.RawMessage(`{"x":1}`)), check: func(result mvmhost.Result, err error) {
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"x": float64(1)}, result.Value)
		}},
		{
			message: *messageWithValue(
				toolCallResultKind,
				json.RawMessage(`{"details":{},"content":[],"is_error":false}`),
			),
			check: func(result mvmhost.Result, err error) {
				require.NoError(t, err)
				assert.IsType(t, emptyToolCallResult(), result.Value)
			},
		},
		{message: *messageWithValue("", json.RawMessage("{")), check: func(_ mvmhost.Result, err error) {
			require.ErrorContains(t, err, "decode execute worker result")
		}},
		{
			message: *messageWithValue(toolCallResultKind, json.RawMessage("{")),
			check: func(_ mvmhost.Result, err error) {
				require.ErrorContains(t, err, "decode execute worker tool result")
			},
		},
		{message: *errorMessage("boom", "runtime", 3), check: func(_ mvmhost.Result, err error) {
			var evalErr *mvmhost.EvalError
			require.ErrorAs(t, err, &evalErr)
			assert.Equal(t, 3, evalErr.ExitCode)
		}},
	}
	for _, test := range cases {
		worker = startedProcess(t, "exit 0")
		test.check(finishResult(t.Context(), worker, io.NopCloser(strings.NewReader("")), &test.message))
	}
}

func TestWorkerProcessErrors(t *testing.T) {
	t.Parallel()

	worker := startedProcess(t, "exit 4")
	require.ErrorContains(t, waitForWorker(t.Context(), worker), "wait for execute worker")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	worker = startedProcess(t, "exit 4")
	require.ErrorIs(t, waitForWorker(ctx, worker), context.Canceled)

	worker = startedProcess(t, "sleep 10")
	require.Error(t, worker.abort(errors.New("cause")))
	worker.kill()

	worker = startedProcess(t, "sleep 10")
	require.ErrorContains(t, worker.readError(t.Context(), errors.New("read")), "read execute worker")
	worker = startedProcess(t, "exit 2")
	require.ErrorContains(t, worker.readError(t.Context(), io.EOF), "exited without result")
	worker = startedProcess(t, "exit 0")
	require.EqualError(t, worker.readError(t.Context(), io.EOF), "execute worker exited without result")

	worker = startedProcess(t, "sleep 10")
	assert.ErrorIs(t, worker.readError(ctx, io.EOF), context.Canceled)
}

func newRPCCaller() *rpcCaller {
	return &rpcCaller{
		in: nil, out: nil, terminalErr: nil, pending: make(map[uint64]chan Message),
		nextID: 0, mu: sync.Mutex{}, writeMu: sync.Mutex{},
	}
}

func newClient(handler RPCHandler, executable string) Client {
	return Client{Handler: handler, Executable: executable}
}

func requestWithArguments(arguments any) *Request {
	return &Request{
		Arguments: arguments, Mode: "", Name: "", Source: "",
	}
}

func messageWithID(id uint64) *Message {
	message := newMessage("")
	message.ID = id

	return &message
}

func messageWithValue(kind string, value json.RawMessage) *Message {
	message := newMessage("")
	message.ValueKind = kind
	message.Value = value

	return &message
}

func errorMessage(message, kind string, exitCode int) *Message {
	result := newMessage("")
	result.Error = message
	result.ErrorKind = kind
	result.ExitCode = exitCode

	return &result
}

func mvmResult(value any) mvmhost.Result {
	return mvmhost.Result{Value: value, ValueKind: "", Stdout: "", Stderr: ""}
}

func emptyToolCallResult() ToolCallResult {
	return ToolCallResult{Details: nil, Error: "", Content: nil, IsError: false}
}
