package tool

import (
	"strings"

	gt "github.com/odvcencio/gotreesitter"
	"github.com/samber/oops"
)

// oopsInvalidLine is the shared error for a non-positive one-based line number.
func oopsInvalidLine() error {
	return oops.
		In("tool").
		Code("ast_invalid_line").
		Errorf("ast line must be one-based and positive")
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
	for index := range count {
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
	for index := range count {
		child := node.NamedChild(index)
		if child != nil && strings.Contains(child.Type(lang), "identifier") {
			return firstLine(child.Text(source))
		}
	}

	return ""
}

// firstLine collapses a node's text to its first non-empty line for compact output.
func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	result, _ := TruncateLine(trimmed, GrepMaxLineLength)

	return result
}
