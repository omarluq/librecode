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

	output := collectASTQueryOutput(tree, lang, source, query)

	return astQueryResult(output), nil
}

type astQueryOutput struct {
	lines               []string
	matchCount          int
	limitReached        bool
	captureLimitReached bool
}

func collectASTQueryOutput(tree *gt.Tree, lang *gt.Language, source []byte, query *gt.Query) astQueryOutput {
	cursor := query.Exec(tree.RootNode(), lang, source)
	cursor.SetMatchLimit(defaultASTMatchLimit)

	output := astQueryOutput{
		lines:               []string{},
		matchCount:          0,
		limitReached:        false,
		captureLimitReached: false,
	}

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		output.matchCount++
		if appendASTQueryCaptures(&output, match.Captures, source) {
			break
		}
	}

	output.limitReached = cursor.DidExceedMatchLimit()

	return output
}

func appendASTQueryCaptures(output *astQueryOutput, captures []gt.QueryCapture, source []byte) bool {
	for _, capture := range captures {
		if len(output.lines) >= maxASTQueryCaptures {
			output.captureLimitReached = true

			return true
		}

		node := capture.Node
		if node == nil {
			continue
		}

		output.lines = append(output.lines, astQueryCaptureLine(capture, source))
	}

	return false
}

func astQueryCaptureLine(capture gt.QueryCapture, source []byte) string {
	text := capture.TextOverride
	if text == "" {
		text = capture.Node.Text(source)
	}

	return fmt.Sprintf(
		"  L%d  @%s  %s",
		capture.Node.StartPoint().Row+1,
		capture.Name,
		firstLine(text),
	)
}

func astQueryResult(output astQueryOutput) Result {
	truncated := output.limitReached || output.captureLimitReached

	details := map[string]any{
		astDetailMatches:      output.matchCount,
		astDetailCaptures:     len(output.lines),
		astDetailCaptureLimit: maxASTQueryCaptures,
		astDetailMatchLimit:   defaultASTMatchLimit,
		astDetailLimitReached: output.limitReached,
		astDetailTruncated:    truncated,
	}
	if len(output.lines) == 0 {
		return TextResult("No matches found", details)
	}

	header := fmt.Sprintf("%d matches, %d captures:", output.matchCount, len(output.lines))

	body := header + "\n" + strings.Join(output.lines, "\n")
	if output.captureLimitReached {
		body += fmt.Sprintf("\n  ... output truncated at %d captures (narrow the query)", maxASTQueryCaptures)
	}

	if output.limitReached {
		body += fmt.Sprintf(
			"\n  ... output truncated by the %d-match query limit (narrow the query)",
			defaultASTMatchLimit,
		)
	}

	return TextResult(body, details)
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

	target := namedNodeAtLine(tree.RootNode(), *line)
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
