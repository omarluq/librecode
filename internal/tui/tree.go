package tui

import "github.com/gdamore/tcell/v3"

// TreeNode is one node in a tree view.
type TreeNode struct {
	Style    tcell.Style
	Value    string
	Text     string
	Children []*TreeNode
	Expanded bool
	Selected bool
}

// TreeView renders a selectable tree as an indented list.
type TreeView struct {
	Style         tcell.Style
	SelectedStyle tcell.Style
	Root          *TreeNode
	SelectedIndex int
}

// Flatten returns visible tree rows.
func (view *TreeView) Flatten() []Line {
	if view == nil || view.Root == nil {
		return []Line{}
	}

	lines := []Line{}

	var walk func(node *TreeNode, prefix string, root bool)

	walk = func(node *TreeNode, prefix string, root bool) {
		if node == nil {
			return
		}

		marker := computeMarker(node, root)
		style := resolveTreeStyle(node, view.Style, view.SelectedStyle)

		lines = append(lines, NewLine(style, prefix+marker+node.Text))
		if node.Expanded || root {
			for _, child := range node.Children {
				walk(child, prefix+"  ", false)
			}
		}
	}
	walk(view.Root, "", true)

	return lines
}

func computeMarker(node *TreeNode, root bool) string {
	if root {
		return ""
	}

	if len(node.Children) == 0 {
		return "  "
	}

	if node.Expanded {
		return "▾ "
	}

	return "▸ "
}

func resolveTreeStyle(node *TreeNode, defaultStyle, selectedStyle tcell.Style) tcell.Style {
	if node.Selected {
		return selectedStyle
	}

	if node.Style != (tcell.Style{}) {
		return node.Style
	}

	return defaultStyle
}

// Render returns visible tree lines.
func (view *TreeView) Render(width, height int) []Line {
	if width <= 0 || height <= 0 {
		return []Line{}
	}

	lines := view.Flatten()
	for index := range lines {
		lines[index] = lines[index].Truncate(width)
	}

	return Tail(lines, height)
}

// Draw draws the tree into rect.
func (view *TreeView) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, view.Render(rect.Width, rect.Height))
}

// TreeList is a tree flattened to a list.
type TreeList = TreeView
