package executeworker_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/workflowprogress"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

const (
	workerNameKey   = "name"
	helperEnv       = "LIBRECODE_EXECUTEWORKER_TEST_HELPER"
	helperStderrEnv = "LIBRECODE_EXECUTEWORKER_TEST_STDERR"
	echoQuery       = "echo"
	waitErrorText   = "wait for execute worker"
	progressName    = "progress.go"
)

func TestMain(testMain *testing.M) {
	if mode := os.Getenv(helperStderrEnv); mode != "" {
		runStderrHelper(mode)
	}

	if os.Getenv(helperEnv) == "1" {
		if err := executeworker.Serve(os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}

		os.Exit(0)
	}

	goleak.VerifyTestMain(testMain)
}

func runStderrHelper(mode string) {
	if mode == "request-write" {
		writeHelperStderr("worker diagnostic")
		os.Exit(2)
	}

	if _, err := executeworker.Read(os.Stdin); err != nil {
		os.Exit(3)
	}

	if mode == "abort" {
		writeHelperStderr("worker diagnostic")
		writeHelperMessage(helperMessage("unexpected"))
		time.Sleep(time.Minute)
		os.Exit(3)
	}

	switch mode {
	case "large":
		writeHelperStderr("stderr prefix " + strings.Repeat("x", 70<<10) + " tail sentinel")
	case "stdout-read":
		writeHelperStderr("worker diagnostic")
		writeMalformedFrame()
	case "wait-after-result":
		writeHelperStderr("worker diagnostic")
		writeHelperMessage(helperMessage("result"))
	case "premature-exit":
		writeHelperStderr("worker diagnostic")
	default:
		writeHelperStderr("worker diagnostic")
	}

	os.Exit(2)
}

func writeMalformedFrame() {
	if err := binary.Write(os.Stdout, binary.BigEndian, uint32(2)); err != nil {
		os.Exit(3)
	}

	if _, err := os.Stdout.WriteString("{"); err != nil {
		os.Exit(3)
	}
}

func writeHelperMessage(message *executeworker.Message) {
	if err := executeworker.Write(os.Stdout, message); err != nil {
		os.Exit(3)
	}
}

func helperMessage(messageType string) *executeworker.Message {
	return &executeworker.Message{
		Stderr: "", Source: "", Method: "", Profile: "", GuestAPI: "", Name: "", Query: "", Stdout: "",
		Type: messageType, Error: "", ErrorKind: "", ValueKind: "", Input: nil, Value: nil,
		Arguments: nil, Progress: nil, ID: 0, ExitCode: 0,
	}
}

func writeHelperStderr(message string) {
	if _, err := os.Stderr.WriteString(message); err != nil {
		os.Exit(3)
	}
}

func testClient() executeworker.Client {
	return executeworker.Client{Executable: os.Args[0], Handler: nil, Progress: nil}
}

func TestClientIncludesWorkerStderrForFailurePaths(t *testing.T) {
	tests := []struct {
		name           string
		mode           string
		source         string
		expectedCauses []string
	}{
		{
			name:           "request write",
			mode:           "request-write",
			source:         strings.Repeat("x", 1<<20),
			expectedCauses: []string{"write execute worker frame", waitErrorText},
		},
		{
			name:           "stdout read",
			mode:           "stdout-read",
			source:         `1`,
			expectedCauses: []string{"read execute worker", waitErrorText},
		},
		{
			name:           "wait after result",
			mode:           "wait-after-result",
			source:         `1`,
			expectedCauses: []string{waitErrorText},
		},
		{
			name:           "abort wait failure",
			mode:           "abort",
			source:         `1`,
			expectedCauses: []string{"unexpected execute worker message", waitErrorText},
		},
		{
			name:           "premature exit",
			mode:           "premature-exit",
			source:         `1`,
			expectedCauses: []string{"execute worker exited without result"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(helperStderrEnv, test.mode)

			_, err := testClient().EvalRequest(t.Context(), canonicalTurnRequest(test.source))
			require.Error(t, err)

			for _, expectedCause := range test.expectedCauses {
				require.ErrorContains(t, err, expectedCause)
			}

			assert.ErrorContains(t, err, "worker diagnostic")
		})
	}
}

func TestClientBoundsWorkerStderr(t *testing.T) {
	t.Setenv(helperStderrEnv, "large")

	_, err := testClient().EvalRequest(t.Context(), canonicalTurnRequest(`1`))
	require.Error(t, err)
	require.ErrorContains(t, err, "stderr prefix")
	require.ErrorContains(t, err, "[execute worker stderr truncated]")
	assert.NotContains(t, err.Error(), "tail sentinel")
	assert.Less(t, len(err.Error()), 66<<10)
}

func TestClientHardCancelsInfiniteLoop(t *testing.T) {
	t.Setenv(helperEnv, "1")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := testClient().EvalRequest(ctx, canonicalTurnRequest(`for {}; 1`))
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 3*time.Second)
}

func canonicalTurnRequest(source string) *executeworker.Request {
	return &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: "execute.go", Source: source,
	}
}

func TestClientPreservesTypedToolCallResults(t *testing.T) {
	t.Setenv(helperEnv, "1")

	want := executeworker.ToolCallResult{
		Details: map[string]any{"path": "image.png"},
		Content: []tool.ContentBlock{
			{Type: tool.ContentTypeText, Text: "caption", Data: "", MIMEType: ""},
			{Type: tool.ContentTypeImage, Text: "", Data: "aW1hZ2U=", MIMEType: "image/png"},
		},
		Error: "nested failure", IsError: true,
	}
	client := testClient()
	client.Handler = func(_ context.Context, _ *executeworker.Message) (any, error) { return want, nil }

	request := canonicalTurnRequest(`import "librecode/tools"; tools.Call("image", nil)`)
	result, err := client.EvalRequest(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, want, result.Value)
}

func TestClientPreservesNullCallbackResult(t *testing.T) {
	t.Setenv(helperEnv, "1")

	client := testClient()
	client.Handler = func(_ context.Context, _ *executeworker.Message) (any, error) {
		return json.RawMessage("null"), nil
	}

	request := canonicalTurnRequest(`import "librecode/tools"; tools.Describe("missing")`)
	result, err := client.EvalRequest(t.Context(), request)
	require.NoError(t, err)

	raw, ok := result.Value.(json.RawMessage)
	require.True(t, ok)
	assert.JSONEq(t, "null", string(raw))
}

func TestClientCanonicalToolsPackage(t *testing.T) {
	t.Setenv(helperEnv, "1")

	client := testClient()
	client.Handler = func(_ context.Context, message *executeworker.Message) (any, error) {
		return []map[string]any{{workerNameKey: message.Query}}, nil
	}
	result, err := client.EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: "canonical.go", Source: `import "librecode/tools"; tools.Search("echo")`,
	})
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{workerNameKey: echoQuery}}, result.Value)

	_, err = client.EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: "legacy.go", Source: `import "tools"; tools.Search("echo")`,
	})
	require.ErrorContains(t, err, "tools")
}

func TestClientRejectsInvalidWorkerManifest(t *testing.T) {
	t.Setenv(helperEnv, "1")

	_, err := testClient().EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: "unknown", GuestAPIVersion: guestapi.CurrentVersion,
		Name: "bad.go", Source: "1",
	})
	require.ErrorContains(t, err, "unknown execute worker profile")

	_, err = testClient().EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: "99",
		Name: "bad.go", Source: "1",
	})
	require.ErrorContains(t, err, "incompatible guest API version")
}

func TestClientProgressFramesPreserveOrderAndFinalResult(t *testing.T) {
	t.Setenv(helperEnv, "1")

	var events []workflowprogress.Event

	client := testClient()
	client.Progress = func(_ context.Context, event workflowprogress.Event) error {
		events = append(events, event)

		return nil
	}

	result, err := client.EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: progressName, Source: `import "librecode/workflow"
workflow.Log("info", "first")
workflow.Log("info", "second")
42`,
	})
	require.NoError(t, err)
	assert.InDelta(t, 42, result.Value, 0)
	require.Len(t, events, 2)
	assert.Equal(t, []uint64{1, 2}, []uint64{events[0].Sequence, events[1].Sequence})
	assert.Equal(t, "first", events[0].Log.Message)
	assert.Equal(t, "second", events[1].Log.Message)
}

func TestClientProgressErrorStopsEvaluation(t *testing.T) {
	t.Setenv(helperEnv, "1")

	client := testClient()
	client.Progress = func(_ context.Context, _ workflowprogress.Event) error {
		return errors.New("progress rejected")
	}

	_, err := client.EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: progressName, Source: `import "librecode/workflow"; workflow.Log("info", "hello")`,
	})
	require.ErrorContains(t, err, "progress rejected")
}

func TestClientProgressCallbackReceivesCancellation(t *testing.T) {
	t.Setenv(helperEnv, "1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := testClient()
	client.Progress = func(ctx context.Context, _ workflowprogress.Event) error {
		cancel()
		<-ctx.Done()

		return ctx.Err()
	}

	_, err := client.EvalRequest(ctx, &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: progressName, Source: `import "librecode/workflow"; workflow.Log("info", "hello")`,
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestProgressFramesDoNotConsumeRPCCallbackBudget(t *testing.T) {
	t.Setenv(helperEnv, "1")

	client := testClient()
	client.Handler = func(_ context.Context, _ *executeworker.Message) (any, error) {
		return []string{"ok"}, nil
	}

	var source strings.Builder

	source.WriteString("import \"librecode/workflow\"\nimport \"librecode/tools\"\n")

	for range workflowprogress.MaxEvents {
		source.WriteString("workflow.Log(\"info\", \"event\")\n")
	}

	source.WriteString("tools.Search(\"after progress\")")

	result, err := client.EvalRequest(t.Context(), &executeworker.Request{
		Arguments: nil, Profile: guestapi.ProfileTurn, GuestAPIVersion: guestapi.CurrentVersion,
		Name: "budget.go", Source: source.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, []any{"ok"}, result.Value)
}

func TestClientCallbackRPC(t *testing.T) {
	t.Setenv(helperEnv, "1")

	client := testClient()
	client.Handler = func(_ context.Context, message *executeworker.Message) (any, error) {
		if message.Method != "search" || message.Query != echoQuery {
			return nil, errors.New("unexpected RPC")
		}

		return []map[string]any{{"name": echoQuery}}, nil
	}

	request := canonicalTurnRequest(`import "librecode/tools"; tools.Search("echo")`)
	result, err := client.EvalRequest(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{workerNameKey: echoQuery}}, result.Value)
}
