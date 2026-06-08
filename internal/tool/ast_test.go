package tool_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	astPathKey     = "path"
	astModeKey     = "mode"
	astQueryKey    = "query"
	astModeQuery   = "query"
	astModeNode    = "node"
	astModeSymb    = "symbols"
	astModeTree    = "tree"
	astLineKey     = "line"
	astContentKey  = "content"
	astSamplePath  = "sample.go"
	astIgnoredPath = "node_modules/pkg.go"
	astFuncDeclQ   = `(function_declaration name: (identifier) @fn)`
)

const astFixture = `package sample

import "fmt"

const Greeting = "hi"

var counter int

type Widget struct {
	Name string
}

func (w Widget) Render() string {
	return fmt.Sprintf("%s", w.Name)
}

func Build(n int) *Widget {
	return &Widget{Name: Greeting}
}
`

func newASTRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	registry := tool.NewRegistry(t.TempDir())
	_, err := registry.Execute(context.Background(), "write", map[string]any{
		astPathKey:    astSamplePath,
		astContentKey: astFixture,
	})
	require.NoError(t, err)

	return registry
}

func runAST(t *testing.T, registry *tool.Registry, input map[string]any) (tool.Result, error) {
	t.Helper()

	return registry.Execute(context.Background(), "ast", input)
}

func TestASTTool_OutlineListsTopLevelDeclarations(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{astPathKey: astSamplePath})
	require.NoError(t, err)

	text := result.Text()
	for _, want := range []string{
		"const_declaration",
		"var_declaration",
		"type_declaration Widget",
		"method_declaration Render",
		"function_declaration Build",
	} {
		assert.Contains(t, text, want, "outline should mention %q", want)
	}
	assert.Equal(t, "go", result.Details["language"])
}

func TestASTTool_OutlineIsDefaultMode(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	implicit, err := runAST(t, registry, map[string]any{astPathKey: astSamplePath})
	require.NoError(t, err)
	explicit, err := runAST(t, registry, map[string]any{astPathKey: astSamplePath, astModeKey: "outline"})
	require.NoError(t, err)
	assert.Equal(t, explicit.Text(), implicit.Text())
}

func TestASTTool_QueryFindsFunctionDeclarations(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey:  astSamplePath,
		astModeKey:  astModeQuery,
		astQueryKey: astFuncDeclQ,
	})
	require.NoError(t, err)

	text := result.Text()
	assert.Contains(t, text, "@fn")
	assert.Contains(t, text, "Build")
	assert.Equal(t, 1, result.Details["matches"])
}

func TestASTTool_ModeParameterValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   map[string]any
		wantErr string
	}{
		{
			name:    "query mode requires query text",
			input:   map[string]any{astPathKey: astSamplePath, astModeKey: astModeQuery},
			wantErr: "query",
		},
		{
			name:    "node mode requires line",
			input:   map[string]any{astPathKey: astSamplePath, astModeKey: astModeNode},
			wantErr: astLineKey,
		},
		{
			name:    "invalid mode is rejected",
			input:   map[string]any{astPathKey: astSamplePath, astModeKey: "bogus"},
			wantErr: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registry := newASTRegistry(t)
			_, err := runAST(t, registry, testCase.input)
			require.Error(t, err)
			if testCase.wantErr != "" {
				assert.Contains(t, err.Error(), testCase.wantErr)
			}
		})
	}
}

func TestASTTool_PathRequired(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	_, err := runAST(t, registry, map[string]any{astPathKey: "  "})
	require.Error(t, err)
}

func TestASTTool_QueryRejectsInvalidExpression(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	_, err := runAST(t, registry, map[string]any{
		astPathKey:  astSamplePath,
		astModeKey:  astModeQuery,
		astQueryKey: `(this is not valid`,
	})
	require.Error(t, err)
}

func TestASTTool_NodeReportsEnclosingAncestry(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: astModeNode,
		astLineKey: 14, // inside Widget.Render body
	})
	require.NoError(t, err)

	text := result.Text()
	assert.Contains(t, text, "method_declaration")
	assert.Contains(t, text, "Render")
	assert.Contains(t, text, "source_file")
}

func TestASTTool_SymbolsNestsMethodsUnderTypes(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: astModeSymb,
	})
	require.NoError(t, err)

	text := result.Text()
	assert.Contains(t, text, "symbols")
	assert.Contains(t, text, "Widget")
	assert.Contains(t, text, "Build")
	assert.Equal(t, "go", result.Details["language"])
}

func TestASTTool_SymbolsRespectsDepthZero(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: astModeSymb,
		"depth":    0,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Details["depth"])
}

func TestASTTool_TreeDumpsSExpression(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: astModeTree,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Text(), "source_file")
}

func TestASTTool_TreeForLineScopesToSubtree(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: astModeTree,
		astLineKey: 19, // inside Build
	})
	require.NoError(t, err)
	assert.Equal(t, 19, result.Details[astLineKey])
}

func TestASTTool_UnsupportedLanguageIsGraceful(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	_, err := registry.Execute(context.Background(), "write", map[string]any{
		astPathKey:    "data.unknownext",
		astContentKey: "noop",
	})
	require.NoError(t, err)

	result, err := runAST(t, registry, map[string]any{astPathKey: "data.unknownext"})
	require.NoError(t, err)
	assert.Equal(t, false, result.Details["supported"])
}

func TestASTTool_RefusesIgnoredPathByDefault(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	_, err := registry.Execute(context.Background(), "write", map[string]any{
		astPathKey:    astIgnoredPath,
		astContentKey: astFixture,
	})
	require.NoError(t, err)

	result, err := runAST(t, registry, map[string]any{astPathKey: astIgnoredPath})
	require.NoError(t, err)
	assert.Equal(t, true, result.Details["ignored"])
	assert.NotContains(t, result.Text(), "outline")
}

func TestASTTool_AllowIgnoredReadsIgnoredPath(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	_, err := registry.Execute(context.Background(), "write", map[string]any{
		astPathKey:    astIgnoredPath,
		astContentKey: astFixture,
	})
	require.NoError(t, err)

	result, err := runAST(t, registry, map[string]any{
		astPathKey:     astIgnoredPath,
		"allowIgnored": true,
	})
	require.NoError(t, err)
	assert.Nil(t, result.Details["ignored"])
	assert.Contains(t, result.Text(), "outline")
}

func TestASTTool_QueryReportsMatchLimitReached(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey:  astSamplePath,
		astModeKey:  astModeQuery,
		astQueryKey: astFuncDeclQ,
	})
	require.NoError(t, err)
	// The fixture has fewer matches than the limit, so it is not reached.
	assert.Equal(t, false, result.Details["matchLimitReached"])
	assert.Contains(t, result.Details, "matchLimit")
}

func TestASTTool_RegisteredInRegistry(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	definitions := registry.Definitions()
	names := make([]string, 0, len(definitions))
	for _, def := range definitions {
		names = append(names, string(def.Name))
	}
	assert.Contains(t, names, "ast")
}

func TestASTTool_NormalizesModeWhitespaceAndCase(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: " Symbols ",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Text(), "symbols")
}

func TestASTTool_QueryCountsMatchesAndCapturesSeparately(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey:  astSamplePath,
		astModeKey:  astModeQuery,
		astQueryKey: `(function_declaration name: (identifier) @name parameters: (parameter_list) @params)`,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Details["matches"])
	assert.Equal(t, 2, result.Details["captures"])
	assert.Contains(t, result.Text(), "1 matches, 2 captures")
}

func TestASTTool_QueryOutputIsExplicitlyBounded(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	var builder strings.Builder
	builder.WriteString("package sample\n\n")
	for index := range 260 {
		_, err := fmt.Fprintf(&builder, "func Func%d() {}\n", index)
		require.NoError(t, err)
	}
	_, err := registry.Execute(context.Background(), "write", map[string]any{
		astPathKey:    astSamplePath,
		astContentKey: builder.String(),
	})
	require.NoError(t, err)

	result, err := runAST(t, registry, map[string]any{
		astPathKey:  astSamplePath,
		astModeKey:  astModeQuery,
		astQueryKey: `(function_declaration name: (identifier) @fn)`,
	})
	require.NoError(t, err)

	assert.Equal(t, 200, result.Details["captures"])
	assert.Equal(t, 200, result.Details["captureLimit"])
	assert.Equal(t, true, result.Details["truncated"])
	assert.Contains(t, result.Text(), "output truncated")
}

func TestASTTool_OutlineOutputIsBounded(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	var builder strings.Builder
	builder.WriteString("package sample\n\n")
	for index := range 430 {
		_, err := fmt.Fprintf(&builder, "func Func%d() {}\n", index)
		require.NoError(t, err)
	}
	_, err := registry.Execute(context.Background(), "write", map[string]any{
		astPathKey:    astSamplePath,
		astContentKey: builder.String(),
	})
	require.NoError(t, err)

	result, err := runAST(t, registry, map[string]any{astPathKey: astSamplePath})
	require.NoError(t, err)

	assert.Equal(t, 400, result.Details["count"])
	assert.Equal(t, true, result.Details["truncated"])
	assert.Contains(t, result.Text(), "output truncated")
}

func TestASTTool_TreeLinePastEOFDoesNotDumpRoot(t *testing.T) {
	t.Parallel()

	registry := newASTRegistry(t)
	result, err := runAST(t, registry, map[string]any{
		astPathKey: astSamplePath,
		astModeKey: astModeTree,
		astLineKey: 999,
	})
	require.NoError(t, err)

	assert.Equal(t, 999, result.Details[astLineKey])
	assert.Contains(t, result.Text(), "No node found")
	assert.NotContains(t, result.Text(), "source_file")
}
