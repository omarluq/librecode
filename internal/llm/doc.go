// Package llm defines provider-neutral request, response, streaming, and error DTOs.
//
// The package intentionally contains no provider wire formats and no librecode
// runtime, database, tool, model, or extension imports. Provider packages should
// translate between their HTTP APIs and these types; assistant orchestration
// should translate between persisted session state and these types.
//
// This package is the provider boundary for generation requests and responses.
// Assistant code translates persisted session state into these DTOs, while
// provider code translates them into HTTP payloads and normalized responses.
package llm
