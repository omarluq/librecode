package provider

import (
	"encoding/json"
	"slices"
	"strings"
)

const (
	requestShapeByteSizeKey                = "byte_size"
	requestShapeFunctionCallCountKey       = "function_call_count"
	requestShapeFunctionCallOutputCountKey = "function_call_output_count"
	requestShapeInputCountKey              = "input_count"
	requestShapeMessageCountKey            = "message_count"
	requestShapeToolCountKey               = "tool_count"
	requestShapeIncludeKey                 = "include"
	requestShapeParallelToolCallsKey       = "parallel_tool_calls"
	requestShapePromptCacheKey             = "prompt_cache_key"
)

// RequestShape contains safe, content-free metadata about an outbound provider request.
type RequestShape struct {
	Keys                    []string `json:"keys,omitempty"`
	ByteSize                int      `json:"byte_size,omitempty"`
	FunctionCallCount       int      `json:"function_call_count,omitempty"`
	FunctionCallOutputCount int      `json:"function_call_output_count,omitempty"`
	InputCount              int      `json:"input_count,omitempty"`
	KeyCount                int      `json:"key_count,omitempty"`
	MessageCount            int      `json:"message_count,omitempty"`
	ToolCount               int      `json:"tool_count,omitempty"`
	HasInclude              bool     `json:"has_include,omitempty"`
	HasParallelToolCalls    bool     `json:"has_parallel_tool_calls,omitempty"`
	HasPromptCacheKey       bool     `json:"has_prompt_cache_key,omitempty"`
	HasReasoning            bool     `json:"has_reasoning,omitempty"`
}

type providerPayloadShape struct {
	Input             []providerTypedItem        `json:"input"`
	Messages          []providerTypedItem        `json:"messages"`
	Tools             []providerTypedItem        `json:"tools"`
	Reasoning         map[string]json.RawMessage `json:"reasoning"`
	PromptCacheKey    string                     `json:"prompt_cache_key"`
	Include           []json.RawMessage          `json:"include"`
	ParallelToolCalls bool                       `json:"parallel_tool_calls"`
}

type providerTypedItem struct {
	Type string `json:"type"`
}

func providerRequestShape(payload map[string]any) *RequestShape {
	if len(payload) == 0 {
		return nil
	}

	shape := &RequestShape{
		Keys:                    sortedMapKeys(payload),
		ByteSize:                0,
		FunctionCallCount:       0,
		FunctionCallOutputCount: 0,
		InputCount:              0,
		KeyCount:                len(payload),
		MessageCount:            0,
		ToolCount:               0,
		HasInclude:              false,
		HasParallelToolCalls:    false,
		HasPromptCacheKey:       false,
		HasReasoning:            false,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return shape
	}

	shape.ByteSize = len(encoded)

	var typed providerPayloadShape
	if err := json.Unmarshal(encoded, &typed); err != nil {
		return shape
	}

	shape.InputCount = len(typed.Input)
	shape.MessageCount = len(typed.Messages)
	shape.ToolCount = len(typed.Tools)
	shape.FunctionCallCount, shape.FunctionCallOutputCount = countResponseFunctionItems(typed.Input)
	shape.HasInclude = len(typed.Include) > 0
	shape.HasParallelToolCalls = typed.ParallelToolCalls
	shape.HasPromptCacheKey = strings.TrimSpace(typed.PromptCacheKey) != ""
	shape.HasReasoning = len(typed.Reasoning) > 0

	return shape
}

func (shape *RequestShape) empty() bool {
	return shape == nil || shape.KeyCount == 0 && len(shape.Keys) == 0
}

// Payload returns the extension/lifecycle representation of the request shape.
func (shape *RequestShape) Payload() map[string]any {
	if shape.empty() {
		return map[string]any{}
	}

	payload := map[string]any{
		"has_include":             shape.HasInclude,
		"has_parallel_tool_calls": shape.HasParallelToolCalls,
		"has_prompt_cache_key":    shape.HasPromptCacheKey,
		"has_reasoning":           shape.HasReasoning,
		"key_count":               shape.KeyCount,
		"keys":                    shape.Keys,
	}
	setPositive(payload, requestShapeByteSizeKey, shape.ByteSize)
	setPositive(payload, requestShapeInputCountKey, shape.InputCount)
	setPositive(payload, requestShapeFunctionCallCountKey, shape.FunctionCallCount)
	setPositive(payload, requestShapeFunctionCallOutputCountKey, shape.FunctionCallOutputCount)
	setPositive(payload, requestShapeMessageCountKey, shape.MessageCount)
	setPositive(payload, requestShapeToolCountKey, shape.ToolCount)

	return payload
}

func setPositive(payload map[string]any, key string, value int) {
	if value > 0 {
		payload[key] = value
	}
}

func sortedMapKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

func countResponseFunctionItems(items []providerTypedItem) (calls, outputs int) {
	for _, item := range items {
		switch item.Type {
		case functionCallType:
			calls++
		case functionCallOutputType:
			outputs++
		}
	}

	return calls, outputs
}
