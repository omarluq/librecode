package tool

import (
	"fmt"
	"strings"

	gt "github.com/odvcencio/gotreesitter"
)

// astOutline renders the top-level named declarations of the tree.
func astOutline(tree *gt.Tree, lang *gt.Language, source []byte, language string) Result {
	root := tree.RootNode()
	var (
		lines     []string
		truncated bool
	)
	count := root.NamedChildCount()
	for index := 0; index < count; index++ {
		child := root.NamedChild(index)
		if child == nil {
			continue
		}
		if len(lines) >= maxASTOutlineLines {
			truncated = true

			break
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
	if truncated {
		body += fmt.Sprintf("\n  ... output truncated at %d declarations", maxASTOutlineLines)
	}

	return TextResult(body, map[string]any{
		astDetailLanguage:  language,
		astDetailCount:     len(lines),
		astDetailTruncated: truncated,
	})
}

// astTree dumps the S-expression for the whole file, or for the smallest named
// node enclosing line when line is provided.
func astTree(tree *gt.Tree, lang *gt.Language, line *int) (Result, error) {
	target := tree.RootNode()
	if line != nil {
		if *line < 1 {
			return emptyToolResult(), oopsInvalidLine()
		}
		target = namedNodeAtLine(tree.RootNode(), *line)
		if target == nil {
			return TextResult(
				fmt.Sprintf("No node found at line %d", *line),
				map[string]any{astDetailLine: *line},
			), nil
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

// namedNodeAtLine returns the smallest named node enclosing a one-based line.
func namedNodeAtLine(root *gt.Node, line int) *gt.Node {
	if root == nil || line < 1 {
		return nil
	}
	row := uint32(line - 1) //nolint:gosec // line is validated >= 1 above.
	if row > root.EndPoint().Row {
		return nil
	}
	point := gt.Point{Row: row, Column: 0}

	return root.NamedDescendantForPointRange(point, point)
}
