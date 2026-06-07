package tool

import (
	"context"
	"fmt"
	"os"
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

	astDetailMatchLimit   = "matchLimit"
	astDetailLimitReached = "matchLimitReached"
)

// astDeclarationMarkers are substrings that mark a node type as a declaration
// worth surfacing in symbols mode. Kept as substrings so the set stays
// language-agnostic (declaration, definition, _item, etc.).
var astDeclarationMarkers = []string{
	"declaration",
	"definition",
	"_item",
	"_decl",
	"_def",
}

// astSymbolNoise are declaration-like node types that add depth without
// navigational value, so symbols mode descends through them without emitting
// a line. Matched as substrings.
var astSymbolNoise = []string{
	"parameter_declaration",
	"_declaration_list",
	"declaration_list",
	"short_var_declaration",
	"var_spec",
	"const_spec",
}

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

	//nolint:gosec // The ast tool intentionally reads user-selected workspace paths.
	source, err := os.ReadFile(absolutePath)
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

// astOutline renders the top-level named declarations of the tree.
func astOutline(tree *gt.Tree, lang *gt.Language, source []byte, language string) Result {
	root := tree.RootNode()
	var lines []string
	count := root.NamedChildCount()
	for index := 0; index < count; index++ {
		child := root.NamedChild(index)
		if child == nil {
			continue
		}
		lines = append(lines, formatOutlineNode(child, lang, source))
	}

	if len(lines) == 0 {
		return TextResult(
			fmt.Sprintf("No top-level declarations found (%s)", language),
			map[string]any{astDetailLanguage: language, astDetailCount: 0},
		)
	}

	header := fmt.Sprintf("%s outline (%d top-level declarations):", language, len(lines))
	body := header + "\n" + strings.Join(lines, "\n")

	return TextResult(body, map[string]any{
		astDetailLanguage: language,
		astDetailCount:    len(lines),
	})
}

// astSymbols renders a nested, indented tree of declaration nodes down to depth.
func astSymbols(tree *gt.Tree, lang *gt.Language, source []byte, language string, depth *int) Result {
	maxDepth := defaultSymbolDepth
	if depth != nil && *depth >= 0 {
		maxDepth = *depth
	}

	var (
		lines     []string
		truncated bool
	)
	collectSymbols(tree.RootNode(), lang, source, 0, maxDepth, &lines, &truncated)

	if len(lines) == 0 {
		return TextResult(
			fmt.Sprintf("No declarations found (%s)", language),
			map[string]any{"language": language, "count": 0},
		)
	}

	header := fmt.Sprintf("%s symbols (depth %d):", language, maxDepth)
	body := header + "\n" + strings.Join(lines, "\n")
	if truncated {
		body += fmt.Sprintf("\n  ... output truncated at %d symbols (narrow with depth)", maxASTSymbolLines)
	}

	return TextResult(body, map[string]any{
		astDetailLanguage:  language,
		astDetailCount:     len(lines),
		astDetailDepth:     maxDepth,
		astDetailTruncated: truncated,
	})
}

// collectSymbols walks named declaration nodes, appending one indented line per
// declaration until depth or the line cap is exhausted.
func collectSymbols(
	node *gt.Node,
	lang *gt.Language,
	source []byte,
	depth, maxDepth int,
	lines *[]string,
	truncated *bool,
) {
	if node == nil || *truncated {
		return
	}
	count := node.NamedChildCount()
	for index := 0; index < count; index++ {
		child := node.NamedChild(index)
		if child == nil {
			continue
		}
		if !isDeclarationNode(child, lang) {
			// Descend through wrappers (blocks, bodies) without spending a depth level.
			collectSymbols(child, lang, source, depth, maxDepth, lines, truncated)

			continue
		}
		if len(*lines) >= maxASTSymbolLines {
			*truncated = true

			return
		}
		*lines = append(*lines, formatSymbolNode(child, lang, source, depth))
		if depth < maxDepth {
			collectSymbols(child, lang, source, depth+1, maxDepth, lines, truncated)
		}
	}
}

// formatSymbolNode renders one declaration line indented by depth.
func formatSymbolNode(node *gt.Node, lang *gt.Language, source []byte, depth int) string {
	indent := strings.Repeat("  ", depth+1)
	line := node.StartPoint().Row + 1
	name := nodeName(node, lang, source)
	nodeType := node.Type(lang)
	if name == "" {
		return fmt.Sprintf("%sL%d  %s", indent, line, nodeType)
	}

	return fmt.Sprintf("%sL%d  %s %s", indent, line, nodeType, name)
}

// isDeclarationNode reports whether a node type looks like a declaration worth
// surfacing. Noise wrappers (parameter lists, declaration-list containers) are
// excluded so symbols mode stays navigation-focused.
func isDeclarationNode(node *gt.Node, lang *gt.Language) bool {
	nodeType := node.Type(lang)
	for _, noise := range astSymbolNoise {
		if strings.Contains(nodeType, noise) {
			return false
		}
	}
	for _, marker := range astDeclarationMarkers {
		if strings.Contains(nodeType, marker) {
			return true
		}
	}

	return false
}

// astTree dumps the S-expression for the whole file, or for the smallest named
// node enclosing line when line is provided.
func astTree(tree *gt.Tree, lang *gt.Language, line *int) (Result, error) {
	target := tree.RootNode()
	if line != nil {
		if *line < 1 {
			return emptyToolResult(), oops.
				In("tool").
				Code("ast_invalid_line").
				Errorf("ast line must be one-based and positive")
		}
		row := uint32(*line - 1) //nolint:gosec // line is validated >= 1 above.
		point := gt.Point{Row: row, Column: 0}
		if enclosing := tree.RootNode().NamedDescendantForPointRange(point, point); enclosing != nil {
			target = enclosing
		}
	}

	sexpr := target.SExpr(lang)
	truncated := false
	if len(sexpr) > maxASTTreeChars {
		sexpr = sexpr[:maxASTTreeChars] + "\n... (truncated)"
		truncated = true
	}

	details := map[string]any{astDetailTruncated: truncated, "root": target.Type(lang)}
	if line != nil {
		details[astDetailLine] = *line
	}

	return TextResult(sexpr, details), nil
}

// formatOutlineNode renders a single declaration as "L<line> <type> <name>".
func formatOutlineNode(node *gt.Node, lang *gt.Language, source []byte) string {
	line := node.StartPoint().Row + 1
	name := nodeName(node, lang, source)
	nodeType := node.Type(lang)
	if name == "" {
		return fmt.Sprintf("  L%d  %s", line, nodeType)
	}

	return fmt.Sprintf("  L%d  %s %s", line, nodeType, name)
}

// nodeName extracts a representative identifier for a declaration node.
func nodeName(node *gt.Node, lang *gt.Language, source []byte) string {
	if name := fieldName(node, lang, source); name != "" {
		return name
	}
	// Many languages wrap the identifier one level down (e.g. Go's
	// type_declaration -> type_spec -> name). Descend into named children and
	// retry the common name fields, then fall back to any identifier-like node.
	count := node.NamedChildCount()
	for index := 0; index < count; index++ {
		if child := node.NamedChild(index); child != nil {
			if name := childFieldName(child, lang, source); name != "" {
				return name
			}
		}
	}

	return identifierChildName(node, lang, source)
}

// fieldName returns the first matching name-like field directly on node.
func fieldName(node *gt.Node, lang *gt.Language, source []byte) string {
	for _, field := range []string{"name", "declarator", "type"} {
		if named := node.ChildByFieldName(field, lang); named != nil {
			return firstLine(named.Text(source))
		}
	}

	return ""
}

// childFieldName returns a name/declarator field one level below node.
func childFieldName(child *gt.Node, lang *gt.Language, source []byte) string {
	for _, field := range []string{"name", "declarator"} {
		if named := child.ChildByFieldName(field, lang); named != nil {
			return firstLine(named.Text(source))
		}
	}

	return ""
}

// identifierChildName returns the first identifier-like named child's text.
func identifierChildName(node *gt.Node, lang *gt.Language, source []byte) string {
	count := node.NamedChildCount()
	for index := 0; index < count; index++ {
		child := node.NamedChild(index)
		if child != nil && strings.Contains(child.Type(lang), "identifier") {
			return firstLine(child.Text(source))
		}
	}

	return ""
}

// astQuery runs a tree-sitter S-expression query and returns matched captures.
func astQuery(tree *gt.Tree, lang *gt.Language, source []byte, queryText string) (Result, error) {
	if strings.TrimSpace(queryText) == "" {
		return emptyToolResult(), oops.
			In("tool").
			Code("ast_query_required").
			Errorf("ast query mode requires a non-empty query")
	}
	query, err := gt.NewQuery(queryText, lang)
	if err != nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("ast_query_compile").
			Wrapf(err, "compile ast query")
	}

	cursor := query.Exec(tree.RootNode(), lang, source)
	cursor.SetMatchLimit(defaultASTMatchLimit)

	var lines []string
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		for _, capture := range match.Captures {
			node := capture.Node
			if node == nil {
				continue
			}
			text := capture.TextOverride
			if text == "" {
				text = node.Text(source)
			}
			lines = append(lines, fmt.Sprintf(
				"  L%d  @%s  %s",
				node.StartPoint().Row+1,
				capture.Name,
				firstLine(text),
			))
		}
	}

	limitReached := cursor.DidExceedMatchLimit()

	if len(lines) == 0 {
		return TextResult("No matches found", map[string]any{"matches": 0}), nil
	}

	header := fmt.Sprintf("%d matches:", len(lines))
	body := header + "\n" + strings.Join(lines, "\n")
	if limitReached {
		body += fmt.Sprintf("\n  ... more matches beyond the %d-match limit (narrow the query)", defaultASTMatchLimit)
	}

	return TextResult(body, map[string]any{
		"matches":             len(lines),
		astDetailMatchLimit:   defaultASTMatchLimit,
		astDetailLimitReached: limitReached,
	}), nil
}

// astNode reports the named node ancestry enclosing a one-based line.
func astNode(tree *gt.Tree, lang *gt.Language, source []byte, line *int) (Result, error) {
	if line == nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("ast_line_required").
			Errorf("ast node mode requires a line number")
	}
	if *line < 1 {
		return emptyToolResult(), oops.
			In("tool").
			Code("ast_invalid_line").
			Errorf("ast line must be one-based and positive")
	}

	row := uint32(*line - 1) //nolint:gosec // line is validated >= 1 above; the offset is non-negative.
	point := gt.Point{Row: row, Column: 0}
	target := tree.RootNode().NamedDescendantForPointRange(point, point)
	if target == nil {
		return TextResult(
			fmt.Sprintf("No node found at line %d", *line),
			map[string]any{astDetailLine: *line},
		), nil
	}

	var chain []string
	for node := target; node != nil; node = node.Parent() {
		name := nodeName(node, lang, source)
		entry := node.Type(lang)
		if name != "" {
			entry = fmt.Sprintf("%s %s", entry, name)
		}
		chain = append(chain, fmt.Sprintf("L%d %s", node.StartPoint().Row+1, entry))
	}
	// chain is innermost-first; reverse to render outermost-first.
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}

	header := fmt.Sprintf("Node ancestry at line %d (outermost to innermost):", *line)
	body := header + "\n  " + strings.Join(chain, "\n  > ")

	return TextResult(body, map[string]any{astDetailLine: *line, astDetailDepth: len(chain)}), nil
}

// firstLine collapses a node's text to its first non-empty line for compact output.
func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	result, truncated := TruncateLine(trimmed, GrepMaxLineLength)
	if truncated {
		return result
	}

	return result
}
