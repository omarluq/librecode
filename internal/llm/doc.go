// Package llm defines provider-neutral request, response, streaming, and error DTOs.
//
// The package intentionally contains no provider wire formats and no librecode
// runtime, database, tool, model, or extension imports. Provider packages should
// translate between their HTTP APIs and these types; assistant orchestration
// should translate between persisted session state and these types.
//
// This package is a forward-looking boundary for the provider refactor. The
// current runtime still uses provider.CompletionRequest while the migration is
// staged; assistant-side adapters exercise the intended shape and prevent the
// neutral DTO contract from drifting.
package llm
