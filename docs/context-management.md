# Context Management and Compaction Design

## Status

Design proposal. No implementation is included in this document.

## Problem

librecode can resume very long sessions, rebuild the active branch, and send the full model-facing history to the selected provider. For large sessions this can exceed the provider's effective context window and produce errors like:

```text
[system] Your input exceeds the context window of this model. Please adjust your input and try again.
```

A related output-budget failure can happen when the provider accepts the request but ends the response before producing a complete assistant turn:

```text
[system] provider response incomplete: max_output_tokens
```

This is not necessarily an input context overflow. It means the provider stopped because the configured or provider-side maximum output token budget was exhausted.

The current runtime estimates context, but it does not enforce a budget, compact before overflow, or recover automatically when a provider rejects a request. It also surfaces incomplete provider responses as terminal errors without guided recovery. Repeating prompts such as `continue` in an already oversized session sends the same oversized request again.

## Current code scan

Relevant current behavior from the codebase:

- `internal/assistant/usage.go`
  - `estimateTokens(text)` uses trimmed UTF-8 rune count divided by 4.
  - `estimateInputTokens(systemPrompt, messages)` counts only system prompt and message content.
  - provider-reported usage is parsed by `usageFromObject` and merged by `mergeUsage`.
- `internal/assistant/context_build.go`
  - `Runtime.buildModelContext` builds base prompt, available skills prompt, active skill content, extension contributions, and model-facing messages.
  - `contextContributionMaxTokens` limits each extension contribution to 2048 estimated tokens.
  - `model.TokenUsage` already exposes `Breakdown`, `TopContributors`, `ContextWindow`, `ContextTokens`, `InputTokens`, and `OutputTokens`.
- `internal/assistant/runtime.go`
  - `modelResponse` emits estimated usage, builds a tool registry, then calls the provider.
  - There is no preflight budget gate before `completeWithRetry`.
  - `modelFacingMessages` currently includes only `user` and `assistant` roles.
  - `RoleCompactionSummary` and `RoleBranchSummary` are excluded by `isModelFacingRole`, so persisted compaction/branch summaries are not currently sent to providers from the assistant path.
- `internal/database/session_store.go`
  - `BuildContext` walks the active branch and applies entries.
  - `EntryTypeCompaction` replaces previous context with one `RoleCompactionSummary` message.
  - `AppendCompaction` already exists and stores `firstKeptEntryID`, `tokensBefore`, and summary details.
- `internal/database/entry_metadata.go`
  - entries already get `TokenEstimate`, `ModelFacing`, `Display`, and compaction metadata.
  - database-level `ModelFacing` marks compaction and branch summaries as model-facing, but assistant filtering later drops those roles.
- `internal/terminal/commands.go`
  - `/compact` currently renders `manual compaction is not implemented yet`.
- `internal/assistant/tool_schema.go`
  - provider payload includes tool schemas, but context estimation does not count schema overhead.
- `internal/assistant/openai_responses.go`, `openai_chat.go`, `anthropic.go`
  - provider payloads add envelopes, tools, reasoning settings, and tool-call loop messages that are not fully represented in preflight estimates.
- `internal/assistant/sse.go`
  - `response.incomplete` events become terminal errors through `sseProviderError`.
  - `incomplete_details.reason` is surfaced as `provider response incomplete: <reason>`, including `max_output_tokens`.
- `internal/assistant/retry.go`
  - context-window/provider token-limit messages are treated as non-retryable transient errors.
  - there is no special compact-and-retry path for context overflow or incomplete output recovery.

## Design goals

1. Never blindly send an obviously oversized request.
2. Use provider token usage counters when available.
3. Use provider-aware token counting when usage counters are not available.
4. Keep conservative reserves for output, tool schemas, hidden provider overhead, and estimation error.
5. Automatically compact before overflow.
6. Detect provider context-overflow errors, compact, and retry once.
7. Classify incomplete responses such as `max_output_tokens` separately from input overflow and present actionable recovery.
8. Make manual `/compact` real and useful.
9. Preserve task continuity by keeping a summary plus recent tail turns.
10. Keep the transcript and audit history durable; compact only model context.
11. Expose clear diagnostics through `/context`, events, logs, and extension lifecycle payloads.

## Non-goals

- Deleting old session entries from SQLite.
- Rewriting transcript rendering.
- Sending full historical tool outputs by default.
- Depending on provider-side conversation storage.
- Moving core context management into Lua extensions.

## External-agent practices to combine

The target system intentionally combines the strongest ideas from other coding agents:

- **Crush-style accounting:** track provider token usage across turns and use it for future estimates.
- **Kilo/opencode-style compaction policy:** compute usable context, reserve output headroom, keep recent tail turns, and prune bulky tool outputs if/when they become model-facing.
- **pi-style recovery:** compact when near the limit, and on context-overflow provider errors compact then retry once.
- **Goose-style token counting:** use provider/tokenizer-specific counting where available, including message and tool-schema overhead, with chars/4 only as fallback.

Provider counters are valuable, but they arrive only after a successful call. They must supplement, not replace, preflight estimation.

## High-level architecture

Add a Go-owned context-management subsystem under `internal/assistant`:

```text
Runtime.modelResponse
  -> buildModelContext
  -> ContextManager.Plan
       -> token counter
       -> budget calculator
       -> usage ledger lookup
       -> compaction planner
  -> maybe compact and rebuild context
  -> preflight guard
  -> provider request
  -> persist usage ledger
  -> on overflow: compact and retry once
```

Suggested files:

```text
internal/assistant/context_manager.go
internal/assistant/context_budget.go
internal/assistant/context_counter.go
internal/assistant/context_compaction.go
internal/assistant/context_overflow.go
internal/database/session_usage_repository.go
```

## Core types

```go
type ContextPolicy struct {
    Enabled              bool
    AutoCompact          bool
    OverflowRetry        bool
    AutoCompactThreshold int
    OutputReserveTokens  int
    ToolReserveTokens    int
    ProviderReserveTokens int
    SafetyMarginTokens   int
    TailTurns            int
    TailTokenBudget      int
}

type ContextBudget struct {
    ContextWindow        int
    UsableInputTokens    int
    OutputReserveTokens  int
    ToolSchemaTokens     int
    ProviderReserveTokens int
    SafetyMarginTokens   int
    AutoCompactAtTokens  int
}

type ContextPlan struct {
    Action       ContextAction
    Reason       string
    Budget       ContextBudget
    Usage        model.TokenUsage
    TailStartID  string
    TokensBefore int
}

type ContextAction string

const (
    ContextActionSend       ContextAction = "send"
    ContextActionCompact    ContextAction = "compact"
    ContextActionReject     ContextAction = "reject"
)
```

## Budget formula

```text
usable_input = context_window
  - output_reserve
  - tool_schema_tokens
  - provider_hidden_reserve
  - safety_margin

auto_compact_at = floor(usable_input * auto_compact_threshold_percent / 100)
```

Initial conservative defaults:

| Setting | Suggested default |
| --- | ---: |
| `assistant.context.enabled` | `true` |
| `assistant.context.auto_compact` | `true` |
| `assistant.context.overflow_retry` | `true` |
| `assistant.context.auto_compact_threshold` | `80` |
| `assistant.context.output_reserve_tokens` | `min(max_tokens, 32768)`, fallback `32768` for large models |
| `assistant.context.provider_reserve_tokens` | `8192` |
| `assistant.context.safety_margin_tokens` | `8192` |
| `assistant.context.tail_turns` | `8` |
| `assistant.context.tail_token_budget` | `20000` for large models |

For small models, clamp reserves so they do not consume the whole window:

```text
output_reserve = min(configured_or_model_max_output, max(2048, context_window * 20%))
safety_margin = max(1024, context_window * 5%)
tail_budget = clamp(context_window * 25%, 2000, 8000)
```

For unknown model context windows (`0`), do not reject by default. Still emit estimates and warnings, and rely on provider overflow recovery.

## Token-counting strategy

Counting tiers:

1. **Provider-reported usage** from previous successful calls.
2. **Provider-specific tokenizer** for current request estimation.
3. **Provider-family message overhead estimator** if tokenizer is unavailable.
4. **Current chars/4 estimate** as the final fallback.

The counter should estimate:

- system prompt / instructions
- available skills prompt
- active skill content
- extension context contributions
- user/assistant history
- compaction and branch summaries once they become model-facing
- provider message envelope overhead
- tool schemas
- tool-call loop additions during the current turn
- reasoning/thinking configuration overhead where meaningful

### Tokenizer providers

Introduce an interface:

```go
type TokenCounter interface {
    CountText(text string) int
    CountMessages(system string, messages []database.MessageEntity) int
    CountTools(definitions []tool.Definition) int
}
```

Initial implementations:

- `ApproxTokenCounter`: current chars/4 fallback plus per-message overhead.
- `OpenAITokenCounter`: use `o200k_base`-compatible tokenizer if a suitable Go dependency is selected.
- `AnthropicTokenCounter`: provider-specific approximation until an official/local tokenizer is available.

Dependency choice should be explicit and small. If no mature Go tokenizer is acceptable, keep the interface and ship the conservative estimator first.

## Provider usage ledger

Add durable per-turn usage storage. This avoids relying only on the latest in-memory `app.tokenUsage` and enables Crush/pi-style historical accounting.

Suggested migration:

```sql
CREATE TABLE session_turn_usage (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_entry_id TEXT NOT NULL,
    assistant_entry_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    context_window INTEGER NOT NULL DEFAULT 0,
    estimated_input_tokens INTEGER NOT NULL DEFAULT 0,
    provider_input_tokens INTEGER NOT NULL DEFAULT 0,
    provider_output_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    request_hash TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_session_turn_usage_session_created
    ON session_turn_usage(session_id, created_at);
```

Persist after a successful `Prompt` when both user and assistant entry IDs are known.

Use ledger data to improve future estimates:

```text
known_provider_prompt_tokens_up_to_last_success
+ estimated tokens for messages added since that point
+ current system/skills/tool schema estimate if they changed
```

Because system prompts, skills, active extensions, and tool schemas can change between turns, the ledger should be treated as calibration data, not an unconditional source of truth.

## Request hash and calibration

Store a hash over context-affecting inputs:

- provider
- model
- system prompt hash
- active skill names and content hash
- tool schema hash
- model-facing entry IDs up to assistant response

If the current request shape matches the last successful request except for a small suffix, reuse provider-reported prompt tokens for the prefix and estimate only the suffix. If not, fall back to full estimation plus a calibration multiplier.

Maintain a conservative calibration multiplier per provider/model:

```text
calibration = max(1.0, provider_input_tokens / estimated_input_tokens)
```

Use a rolling max or high percentile so undercounting is corrected over time.

## Preflight guard

Before provider call:

1. Build context.
2. Build tool registry and estimate tool schemas.
3. Compute budget.
4. Estimate request tokens.
5. If estimate is below `auto_compact_at`, send.
6. If estimate is between `auto_compact_at` and `usable_input`, auto-compact when enabled.
7. If estimate is above `usable_input`, compact or reject with a clear local error.

Local rejection message should include:

- estimated input tokens
- usable input budget
- context window
- top contributors
- suggested action (`/compact`, `/new`, reduce skills, switch model)

Do not send to provider when local preflight says it cannot fit.

## Real manual `/compact`

Manual compaction should:

1. Resolve active session and leaf.
2. Build current active branch context.
3. Select old messages for summarization and recent tail to keep.
4. Ask the selected model, or a configured summarization model, to summarize old context.
5. Append an `EntryTypeCompaction` entry as the new leaf child.
6. Render a system/custom message confirming compaction.
7. Update token usage display.

Initial command behavior:

```text
/compact
/compact --tail-turns 8
/compact --dry-run
```

`--dry-run` should report what would be summarized and what would be kept.

## Compaction summary must be model-facing

Two current filters must change during implementation:

- `isModelFacingRole` should include `RoleCompactionSummary` and likely `RoleBranchSummary`.
- provider role mappers should convert summary roles into user-visible context:
  - OpenAI Chat: user message with `core.CompactionSummaryPrefix` / suffix.
  - OpenAI Responses/Codex: user input item with wrapped summary text.
  - Anthropic: user message or system block depending on provider behavior.

Do not send raw summary text without wrappers; summaries should be clearly identified as historical compacted context.

## Compaction planner

The planner chooses a boundary:

```text
[summary region][kept tail]
```

Inputs:

- active branch entries/messages
- token estimates per message/entry
- user-configured tail turns
- tail token budget
- current prompt
- context budget

Initial policy:

- keep the latest N user/assistant turns, default 8
- also keep enough tail to cover recent tool-call effects if tool results later become model-facing
- summarize everything older than the kept tail
- never compact away the current user prompt
- if no useful old messages exist, reject with a clear message instead of writing an empty compaction

Advanced policy:

- keep pinned/session label entries
- keep recent explicit user requirements
- keep branch summaries and previous compaction summaries by folding them into the new summary
- protect the last failed/in-progress turn from compaction until persisted

## Summary prompt

The summarizer should preserve actionable state, not produce a vague transcript summary.

Required summary shape:

```markdown
<summary>
## Current objective
...

## Decisions and constraints
- ...

## Files/code touched or investigated
- path: reason/status

## User preferences
- ...

## Important facts from previous tool results
- ...

## Open tasks / next steps
- ...

## Caveats
- ...
</summary>
```

Summary prompt principles:

- preserve exact user instructions and constraints
- preserve unresolved errors and hypotheses
- preserve branch/PR/status context
- avoid stale implementation details unless still relevant
- mention omitted details explicitly when safe

## Recursive compaction

If context is already too large to summarize in one request:

1. Split old region into chunks.
2. Summarize chunks independently.
3. Summarize chunk summaries into one final summary.
4. If still too large, progressively reduce preserved detail and/or tail budget.

This prevents `/compact` from failing on the very sessions it is meant to rescue.

## Tool-output pruning

Current assistant context excludes tool results. If future provider/tool-loop work makes selected tool outputs model-facing, context management must support pruning.

Policy:

- protect recent tool-call/result pairs within the tail window
- summarize older tool results into the compaction summary
- drop or truncate bulky raw outputs outside the protected window
- preserve tool name, args summary, status, paths, and important excerpts
- never include secrets from tool output in summaries

This mirrors Kilo/opencode-style pruning without changing current default model-facing roles prematurely.

## Overflow detection and retry

Add explicit context overflow classification separate from transient retry:

```go
func IsContextOverflowError(err error) bool
```

Match provider codes/messages such as:

- `context length`
- `context window`
- `maximum context`
- `token limit`
- `input exceeds`
- `too many tokens`
- OpenAI invalid request context-length codes
- Anthropic context-window error types

Flow:

1. Provider returns overflow.
2. If request has not already compact-retried, run compaction.
3. Rebuild context.
4. Retry once immediately.
5. If it still fails, show a clear local message and stop.

This should be independent from transient retry backoff. Context overflow is non-transient, but recoverable via compaction.

## Incomplete response detection

OpenAI Responses streaming can emit `response.incomplete` with `incomplete_details.reason`. librecode currently renders this as:

```text
[system] provider response incomplete: max_output_tokens
```

Treat `max_output_tokens` as an output-budget exhaustion signal, not an input-context overflow signal. Recommended behavior:

1. Preserve any partial assistant text/tool state if the provider supplied usable output.
2. Show a clear local message explaining that generation stopped because the output token budget was exhausted.
3. Suggest concrete next actions: ask the model to continue, reduce requested scope, or increase the configured output token limit when the provider/model allows it.
4. Do not blindly retry the same request with the same output budget; that can reproduce the same incomplete response.
5. If the input context is also near the auto-compaction threshold, offer or run compaction before the next continuation request.

Other incomplete reasons, such as `content_filter`, should keep their provider-specific wording and should not be treated as context-budget failures.

## Provider/tool-loop considerations

Provider payloads differ:

- OpenAI Chat includes `messages`, `tools`, and tool result messages inside the loop.
- OpenAI Responses/Codex includes `instructions`, `input`, `tools`, `tool_choice`, optional streaming/reasoning fields, and tool outputs appended between loop iterations.
- Anthropic includes `system`, `messages`, `tools`, `max_tokens`, and thinking configuration.

The budget manager should estimate the initial request plus reserve some room for tool-loop growth. A simple first version can reserve tool schema tokens and output tokens only. Later versions can reserve `max_tool_rounds * average_tool_result_tokens` or dynamically re-run preflight before each provider loop iteration.

## Config

Proposed config shape:

```yaml
assistant:
  context:
    enabled: true
    auto_compact: true
    overflow_retry: true
    auto_compact_threshold: 80
    output_reserve_tokens: 0       # 0 = derive from model
    provider_reserve_tokens: 8192
    safety_margin_tokens: 8192
    tool_schema_reserve_tokens: 0  # 0 = estimate actual schemas
    tail_turns: 8
    tail_token_budget: 0           # 0 = derive from model
    tokenizer: auto                # auto | approximate | provider
    summarizer_provider: ""        # empty = current provider
    summarizer_model: ""           # empty = current model
```

Validation:

- threshold must be `1..100`
- reserves cannot be negative
- tail turns cannot be negative
- derived budgets must never become negative

## Lifecycle and extension events

Context management should remain Go-owned, but extension runtimes should observe decisions.

New event candidates:

- `context_preflight`
- `context_compaction_start`
- `context_compaction_end`
- `context_overflow_retry`
- `context_rejected`

Payload should include bounded/redacted fields:

```json
{
  "session_id": "...",
  "provider": "openai-codex",
  "model": "gpt-5.5",
  "estimated_tokens": 175000,
  "usable_input_tokens": 210000,
  "context_window": 272000,
  "action": "compact",
  "reason": "above_auto_compact_threshold",
  "topContributors": []
}
```

Extensions may eventually contribute compaction hints, but they should not be allowed to bypass hard safety limits.

## User-facing diagnostics

Enhance `/context` to show:

- estimated input tokens
- provider-reported input tokens from last successful turn
- context window
- usable input budget
- reserves breakdown
- auto-compaction threshold
- whether auto-compaction is enabled
- top contributors
- last compaction summary metadata

Example:

```text
context:
- ctx 148k/272k (54%), usable input 219k
- reserves: output 32k, tools 4k, provider 8k, safety 8k
- auto compact at: 175k (80% of usable)
- last provider usage: input 151k, output 2k
- top contributors:
  - history: 122k
  - skills: 15k
  - system: 1k
```

## Failure modes

| Failure | Behavior |
| --- | --- |
| No context window metadata | warn only, send with provider overflow recovery |
| Summary request overflows | chunk summaries recursively |
| Summary provider fails transiently | normal retry applies |
| Summary provider context-overflows | reduce chunk size/tail and retry |
| Cannot compact enough | local error with next actions |
| Provider response incomplete: `max_output_tokens` | preserve partial output when possible and show output-budget recovery guidance |
| User disables auto-compact | preflight rejects instead of silently sending oversized prompt |
| Tokenizer unavailable | conservative approximate counter |
| Ledger missing/corrupt | ignore ledger and estimate from current context |

## Security and privacy

- Compaction summaries become model-facing; never summarize secrets if detected.
- Tool output summarization must apply the same redaction rules as provider hooks.
- Provider hooks must not receive auth headers or secrets.
- Usage ledger stores token counts and hashes, not full prompts.
- Diagnostics should include previews already bounded by existing `TopContributors` behavior.

## Testing plan

Unit tests:

- budget calculation for large, small, and unknown windows
- reserve derivation and clamping
- token counter fallback behavior
- tool schema token estimation
- model-facing role inclusion for compaction summaries
- compaction planner tail boundary selection
- overflow error classifier
- incomplete response classifier for `max_output_tokens` versus provider safety/filter reasons
- usage ledger persistence and lookup
- request-hash/calibration logic

Runtime tests:

- preflight emits usage and sends when under budget
- preflight auto-compacts when over threshold
- preflight rejects when compaction disabled and request is too large
- `/compact --dry-run` reports plan without writes
- `/compact` appends compaction entry and rebuilt context includes summary
- provider overflow triggers compact-and-retry once
- overflow retry does not loop forever
- top contributors survive compaction diagnostics

Provider tests:

- OpenAI Chat maps compaction summary as user-visible context
- OpenAI Responses/Codex maps compaction summary as input context
- Anthropic maps compaction summary safely
- tool schemas are counted for each provider family

Integration tests:

- large synthetic session compacts before provider call
- recursive compaction works when old region exceeds summarizer budget
- usage ledger calibrates future estimates after provider usage arrives

## Rollout phases

### Phase 1: budget model and local preflight

Deliverables:

- `ContextBudget` and `ContextPolicy`
- conservative token estimator with tool schema counting
- preflight diagnostics in `model.TokenUsage.Breakdown`
- local rejection before obviously oversized provider calls
- `/context` budget/reserve display

Acceptance criteria:

- oversized synthetic context is rejected locally before provider client is called
- no behavior change for normal-sized sessions
- unknown-window models continue to work

### Phase 2: model-facing compaction summaries

Deliverables:

- include `RoleCompactionSummary` in assistant model-facing filter
- provider mappers wrap compaction summaries with clear summary markers
- tests for all provider families

Acceptance criteria:

- existing `AppendCompaction` entries affect future model context
- current user/assistant behavior remains unchanged without compaction entries

### Phase 3: manual `/compact`

Deliverables:

- terminal command implementation
- compaction planner
- summarization request path
- `AppendCompaction` integration
- dry-run mode

Acceptance criteria:

- `/compact` on a long session writes one compaction entry
- rebuilt context is summary + recent tail
- `/compact --dry-run` writes nothing

### Phase 4: auto-compaction before request

Deliverables:

- `ContextManager.Plan` integrated into `Runtime.modelResponse`
- automatic compaction when above threshold
- rebuild context after compaction
- lifecycle events and user-visible status

Acceptance criteria:

- long sessions compact automatically before provider call
- user sees that compaction happened
- compaction does not run repeatedly when already under threshold

### Phase 5: overflow compact-and-retry

Deliverables:

- `IsContextOverflowError`
- compact-and-retry once path
- separate from transient retry backoff
- clear failure if retry still overflows

Acceptance criteria:

- mocked provider overflow causes exactly one compaction and one retry
- repeated overflow does not loop
- non-overflow errors keep current retry behavior

### Phase 6: provider usage ledger

Deliverables:

- `session_turn_usage` migration and repository
- persist successful turn usage
- request hash and calibration multiplier
- `/context` shows last provider usage

Acceptance criteria:

- provider-reported input/output tokens are durable
- future estimates can reuse/calibrate from ledger
- old databases migrate cleanly

### Phase 7: provider/tokenizer-specific counting

Deliverables:

- `TokenCounter` interface
- approximate counter as baseline
- OpenAI `o200k_base` counter if dependency is acceptable
- provider-family message overhead estimators

Acceptance criteria:

- counting is closer than chars/4 on representative prompts
- dependency size/licensing is acceptable
- fallback remains deterministic and tested

### Phase 8: recursive compaction and tool-output pruning

Deliverables:

- chunked old-region summarization
- final summary merge
- optional tool-output pruning policy
- protected recent tool-call pairs

Acceptance criteria:

- extremely large sessions can still compact
- summaries preserve current task, decisions, files, and next steps
- bulky tool outputs do not dominate model context when tool outputs become model-facing

### Phase 9: polish and extension diagnostics

Deliverables:

- lifecycle events for context decisions
- richer `/context`
- docs and examples
- configuration documentation
- telemetry-free local diagnostics

Acceptance criteria:

- users understand why compaction happened
- extension authors can observe context decisions
- context failures provide actionable next steps

## Implementation notes

- Keep context management in Go core. Extensions can observe and contribute bounded hints later.
- Make compaction idempotent where possible: if the context after compaction is still above threshold, do not append duplicate summaries without a changed plan.
- Avoid summarizing the just-submitted user prompt away. The current prompt must always remain in the rebuilt context.
- Keep history durable. Compaction changes active branch context reconstruction, not stored transcript rows.
- Prefer small PRs matching the rollout phases.

## Open questions

1. Should summarization use the current model or a configurable cheaper/faster model by default?
2. Should auto-compaction ask for confirmation in interactive TUI, or run silently with a visible system note?
3. What tokenizer dependency is acceptable for `o200k_base` in this Go project?
4. Should branch summaries become model-facing in the same phase as compaction summaries?
5. What exact default reserves should be used for ChatGPT/Codex `gpt-5.5`, whose effective input budget appears lower than local metadata suggests?
