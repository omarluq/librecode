package executeworker_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	helperEnv = "LIBRECODE_EXECUTEWORKER_TEST_HELPER"
	echoQuery = "echo"
)

func TestMain(testMain *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		if err := executeworker.Serve(os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}

		os.Exit(0)
	}

	os.Exit(testMain.Run())
}

func testClient() executeworker.Client {
	return executeworker.Client{Executable: os.Args[0], Handler: nil}
}

func TestClientHardCancelsInfiniteLoop(t *testing.T) {
	t.Setenv(helperEnv, "1")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := testClient().Eval(ctx, `for {}; 1`)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 3*time.Second)
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

	result, err := client.Eval(t.Context(), `import "tools"; tools.Call("image", nil)`)
	require.NoError(t, err)
	assert.Equal(t, want, result.Value)
}

func TestClientPreservesNullCallbackResult(t *testing.T) {
	t.Setenv(helperEnv, "1")

	client := testClient()
	client.Handler = func(_ context.Context, _ *executeworker.Message) (any, error) {
		return json.RawMessage("null"), nil
	}

	result, err := client.Eval(t.Context(), `import "tools"; tools.Describe("missing")`)
	require.NoError(t, err)

	raw, ok := result.Value.(json.RawMessage)
	require.True(t, ok)
	assert.JSONEq(t, "null", string(raw))
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

	result, err := client.Eval(t.Context(), `import "tools"; tools.Search("echo")`)
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"name": echoQuery}}, result.Value)
}
