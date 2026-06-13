package tool

import (
	"fmt"
	"strings"

	gt "github.com/odvcencio/gotreesitter"
)

// astDeclarationMarkers are substrings that mark a node type as a declaration
// worth surfacing in symbols mode. Kept as substrings so the set stays
// language-agnostic (declaration, definition, _item, etc.).
func astDeclarationMarkers() []string {
	return []string{
		"declaration",
		"definition",
		"_item",
		"_decl",
		"_def",
	}
}

// astSymbolNoise are declaration-like node types that add depth without
// navigational value, so symbols mode descends through them without emitting
// a line. Matched as substrings.
func astSymbolNoise() []string {
	return []string{
		"parameter_declaration",
		"_declaration_list",
		"declaration_list",
		"short_var_declaration",
		"var_spec",
		"const_spec",
	}
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
	for index := range count {
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
	for _, noise := range astSymbolNoise() {
		if strings.Contains(nodeType, noise) {
			return false
		}
	}

	for _, marker := range astDeclarationMarkers() {
		if strings.Contains(nodeType, marker) {
			return true
		}
	}

	return false
}
