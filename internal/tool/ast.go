package tool

import (
	"context"
	"fmt"
	"strings"

	gt "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/samber/oops"
)

const (
	// astModeOutline lists top-level declarations as a symbol outline.
	astModeOutline = "outline"
	// astModeSymbols lists declarations recursively as an indented symbol tree.
	astModeSymbols = "symbols"
	// astModeQuery runs a tree-sitter S-expression query.
	astModeQuery = "query"
	// astModeNode reports the smallest named node enclosing a line.
	astModeNode = "node"
	// astModeTree renders a node's S-expression (whole file or one line's subtree).
	astModeTree = "tree"

	defaultASTMatchLimit = 200
	// maxASTQueryCaptures caps query capture output to protect the context budget.
	maxASTQueryCaptures = 200
	// maxASTOutlineLines caps outline output to protect the context budget.
	maxASTOutlineLines = 400
	// maxASTSymbolLines caps symbols output to protect the context budget.
	maxASTSymbolLines = 400
	// maxASTTreeChars caps the S-expression dump length for tree mode.
	maxASTTreeChars = 8000
	// defaultSymbolDepth is the recursion depth used when none is requested.
	defaultSymbolDepth = 2

	astDetailLanguage  = "language"
	astDetailCount     = "count"
	astDetailDepth     = "depth"
	astDetailTruncated = "truncated"
	astDetailLine      = "line"

	astDetailMatches      = "matches"
	astDetailCaptures     = "captures"
	astDetailCaptureLimit = "captureLimit"
	astDetailMatchLimit   = "matchLimit"
	astDetailLimitReached = "matchLimitReached"
)

// ASTInput contains arguments for the ast tool.
type ASTInput struct {
	Line         *int   `json:"line,omitempty"`
	Depth        *int   `json:"depth,omitempty"`
	Path         string `json:"path"`
	Mode         string `json:"mode,omitempty"`
	Query        string `json:"query,omitempty"`
	AllowIgnored bool   `json:"allowIgnored,omitempty"`
}

// ASTTool inspects a source file's syntax tree via a pure-Go tree-sitter.
type ASTTool struct {
	cwd string
}

// NewASTTool creates the ast tool for cwd.
func NewASTTool(cwd string) *ASTTool {
	return &ASTTool{cwd: cwd}
}

// Definition returns ast tool metadata.
func (astTool *ASTTool) Definition() Definition {
	return Definition{
		Schema:        nil,
		Name:          NameAST,
		Label:         "ast",
		Description:   astDescription(),
		PromptSnippet: "Inspect a file's syntax tree (symbol outline, structural queries)",
		PromptGuidelines: []string{
			"Use ast outline to see a file's top-level declarations before reading it line by line.",
			"Use ast symbols (with optional depth) for a nested symbol tree: types plus their methods/fields.",
			"Use ast query with a tree-sitter S-expression to find declarations or call sites structurally.",
			"Use ast node with a line number to learn which construct encloses that line.",
			"Use ast tree to dump the raw S-expression for a file or a single line's subtree.",
		},
		ReadOnly: true,
	}
}

// Execute runs the ast tool.
func (astTool *ASTTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[ASTInput](input)
	if err != nil {
		return emptyToolResult(), err
	}

	return astTool.Inspect(ctx, args)
}

// Inspect parses one source file and returns the requested structural view.
func (astTool *ASTTool) Inspect(ctx context.Context, input ASTInput) (Result, error) {
	if strings.TrimSpace(input.Path) == "" {
		return emptyToolResult(), oops.In("tool").Code("ast_path_required").Errorf("ast path is required")
	}
	parsed, special, err := astTool.parse(ctx, input.Path, input.AllowIgnored)
	if err != nil {
		return emptyToolResult(), err
	}
	if special != nil {
		return *special, nil
	}

	switch astMode(input.Mode) {
	case astModeOutline:
		return astOutline(parsed.tree, parsed.lang, parsed.source, parsed.language), nil
	case astModeSymbols:
		return astSymbols(parsed.tree, parsed.lang, parsed.source, parsed.language, input.Depth), nil
	case astModeQuery:
		return astQuery(parsed.tree, parsed.lang, parsed.source, input.Query)
	case astModeNode:
		return astNode(parsed.tree, parsed.lang, parsed.source, input.Line)
	case astModeTree:
		return astTree(parsed.tree, parsed.lang, input.Line)
	default:
		return emptyToolResult(), oops.
			In("tool").
			Code("ast_invalid_mode").
			With("mode", input.Mode).
			Errorf("ast mode must be one of outline, symbols, query, node, tree")
	}
}

// astParse holds a parsed tree alongside the language metadata needed to read it.
type astParse struct {
	tree     *gt.Tree
	lang     *gt.Language
	language string
	source   []byte
}

// astMode normalizes an optional mode string, defaulting to outline.
func astMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return astModeOutline
	}

	return mode
}

// parse resolves, reads, and parses path. A non-nil special result means the
// file was skipped (ignored path) or has no available grammar, and should be
// reported to the model as-is.
func (astTool *ASTTool) parse(ctx context.Context, path string, allowIgnored bool) (*astParse, *Result, error) {
	absolutePath, err := ResolveReadPath(path, astTool.cwd)
	if err != nil {
		return nil, nil, oops.In("tool").Code("ast_resolve_path").Wrapf(err, "resolve ast path")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	if ignored, reason := ignoredReadPath(absolutePath, astTool.cwd); ignored && !allowIgnored {
		skipped := ignoredReadResult(path, reason)

		return nil, &skipped, nil
	}

	entry := grammars.DetectLanguage(absolutePath)
	if entry == nil {
		unsupported := TextResult(
			fmt.Sprintf("No syntax grammar available for %s", path),
			map[string]any{"path": path, "supported": false},
		)

		return nil, &unsupported, nil
	}

	source, err := readResolvedPath(absolutePath)
	if err != nil {
		return nil, nil, oops.In("tool").Code("ast_read_file").Wrapf(err, "read ast file")
	}

	lang := entry.Language()
	tree, err := gt.NewParser(lang).Parse(source)
	if err != nil {
		return nil, nil, oops.In("tool").Code("ast_parse").Wrapf(err, "parse ast file")
	}

	return &astParse{tree: tree, lang: lang, source: source, language: entry.Name}, nil, nil
}

func astDescription() string {
	return "Inspect a source file's syntax tree using a pure-Go tree-sitter parser. " +
		"Modes: 'outline' (default) lists top-level declarations with line numbers; " +
		"'symbols' renders a nested declaration tree (optional 'depth'); " +
		"'query' runs a tree-sitter S-expression and returns matched nodes; " +
		"'node' returns the named-node ancestry enclosing a given line; " +
		"'tree' dumps the raw S-expression for the file, or one line's subtree when 'line' is set. " +
		"Respects workspace .gitignore/default ignored paths unless allowIgnored is true. " +
		"Read-only and structural — use it to navigate code by construct rather than by text."
}
