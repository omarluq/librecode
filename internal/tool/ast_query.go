package tool

import (
	"fmt"
	"strings"

	gt "github.com/odvcencio/gotreesitter"
	"github.com/samber/oops"
)

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
		return emptyToolResult(), oopsInvalidLine()
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
