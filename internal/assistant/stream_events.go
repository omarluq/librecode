package assistant

import "github.com/omarluq/librecode/internal/provider"

func emitStreamEvent(onEvent func(StreamEvent), event StreamEvent) {
	if onEvent != nil {
		onEvent(event)
	}
}

func providerStreamEvent(event provider.StreamEvent) StreamEvent {
	return StreamEvent{
		ToolEvent: event.ToolEvent,
		Usage:     event.Usage,
		Kind:      providerStreamEventKind(event.Kind),
		Text:      event.Text,
	}
}

func providerStreamEventKind(kind provider.StreamEventKind) StreamEventKind {
	switch kind {
	case provider.StreamEventTextDelta:
		return StreamEventTextDelta
	case provider.StreamEventThinkingDelta:
		return StreamEventThinkingDelta
	case provider.StreamEventToolStart:
		return StreamEventToolStart
	case provider.StreamEventToolResult:
		return StreamEventToolResult
	default:
		return StreamEventUnknown
	}
}

func wrapProviderEvent(onEvent func(StreamEvent)) func(provider.StreamEvent) {
	if onEvent == nil {
		return nil
	}

	return func(event provider.StreamEvent) {
		onEvent(providerStreamEvent(event))
	}
}
