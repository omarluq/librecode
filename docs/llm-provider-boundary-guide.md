# LLM Provider Boundary Guide

This guide records the provider/tooling design patterns we want to borrow while decomposing librecode's assistant runtime and improving tool-call reliability.

## Reference libraries

- [omarluq/goai](https://github.com/omarluq/goai) — fork of [zendev-sh/goai](https://github.com/zendev-sh/goai)
- [omarluq/fantasy](https://github.com/omarluq/fantasy) — fork of [charmbracelet/fantasy](https://github.com/charmbracelet/fantasy)
- [omarluq/pgedge-go-llm-lib](https://github.com/omarluq/pgedge-go-llm-lib) — fork of [pgEdge/pgedge-go-llm-lib](https://github.com/pgEdge/pgedge-go-llm-lib)
- [omarluq/catwalk](https://github.com/omarluq/catwalk) — fork of [charmbracelet/catwalk](https://github.com/charmbracelet/catwalk)

## What to borrow

### Provider interface shape

Use a narrow provider boundary: providers should accept provider-neutral calls and return provider-neutral results. Runtime/session concerns should stay outside provider clients.

Useful shape:

```go
type LanguageModel interface {
    Generate(context.Context, Call) (*Response, error)
    Stream(context.Context, Call) (StreamResponse, error)
    Provider() string
    Model() string
}
```

The provider layer should own HTTP/SSE, wire formats, auth headers, provider errors, and request/response mapping. The assistant runtime should own sessions, compaction, persistence, extension hooks, and tool execution.

### Typed content parts

Prefer typed content parts over ad-hoc strings:

- text
- reasoning/thinking
- tool call
- tool result
- file/image/source attachments when supported

This makes it easier to preserve provider metadata, avoid empty-content provider issues, and map provider-specific capabilities without leaking them into the runtime.

### Structured tool errors

Tool failures should be represented as tool-result errors, not successful text output. This is especially important for Claude/Anthropic, where tool results support `is_error: true`.

Target semantics:

```go
type ToolResult struct {
    ToolCallID string
    Content    []ContentPart
    IsError    bool
}
```

If a local tool cannot run because arguments are missing, invalid, or execution failed, send the model a structured tool error so it can repair the call.

### Tool-call validation before execution

Validate malformed tool calls before running any side-effectful code:

- tool exists
- JSON object is valid
- required fields are present
- required string fields are non-empty when blank is not meaningful

Invalid calls should produce actionable repair feedback, for example:

```text
bash command is required; call bash with {"command":"<command>"}
```

### Model catalog separation

Keep model/provider catalog metadata separate from provider execution. Catwalk-style model metadata is useful for:

- context window
- output token defaults
- pricing
- reasoning support
- image support
- provider-specific capability flags

This belongs in `internal/model`, while provider execution belongs in a future `internal/provider` or `internal/llm` boundary.

## Proposed librecode package direction

```text
internal/llm
  Provider-neutral DTOs:
  Message, ContentPart, ToolCall, ToolResult, Usage, FinishReason, ProviderError

internal/provider
  OpenAI / Anthropic / OpenAI-compatible clients
  HTTP/SSE/retry/provider-specific wire mapping

internal/model
  Model registry/discovery/catalog metadata
  models.dev and Catwalk-style metadata normalization

internal/assistant
  Runtime orchestration
  sessions/branches
  compaction
  tool execution
  persistence
  extension hooks
  TUI events
```

## Immediate reliability work

Before large decomposition, improve current tool-call semantics in place:

1. Add structured tool-result error state.
2. Set Anthropic `tool_result.is_error = true` for failed tools.
3. Validate required tool arguments before execution.
4. Reject `bash {}` / blank bash commands before shell execution.
5. Make `write` distinguish missing `content` from intentional empty content.
