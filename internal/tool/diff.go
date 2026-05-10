package tool

import (
	"fmt"
	"strings"
)

const editDiffContextLines = 4

type diffWindow struct {
	oldStart int
	oldEnd   int
	newStart int
	newEnd   int
	prefix   int
}

func generateDiffString(oldContent, newContent string) EditDetails {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	prefix := commonLinePrefix(oldLines, newLines)
	oldSuffix, newSuffix := commonLineSuffix(oldLines, newLines, prefix)
	if prefix == len(oldLines) && prefix == len(newLines) {
		return EditDetails{Diff: "", FirstChangedLine: 0}
	}

	window := newDiffWindow(prefix, oldSuffix, newSuffix, len(oldLines), len(newLines))
	lineNumberWidth := len(fmt.Sprint(max(len(oldLines), len(newLines))))
	output := make([]string, 0, windowSize(window))
	output = appendContextLines(output, ' ', oldLines, window.oldStart, prefix, lineNumberWidth)
	output = appendContextLines(output, '-', oldLines, prefix, oldSuffix, lineNumberWidth)
	output = appendContextLines(output, '+', newLines, prefix, newSuffix, lineNumberWidth)
	output = appendContextLines(output, ' ', oldLines, oldSuffix, window.oldEnd, lineNumberWidth)

	return EditDetails{Diff: strings.Join(output, "\n"), FirstChangedLine: prefix + 1}
}

func commonLinePrefix(leftLines, rightLines []string) int {
	limit := min(len(leftLines), len(rightLines))
	for lineIndex := range limit {
		if leftLines[lineIndex] != rightLines[lineIndex] {
			return lineIndex
		}
	}

	return limit
}

func commonLineSuffix(leftLines, rightLines []string, prefix int) (leftSuffix, rightSuffix int) {
	leftIndex := len(leftLines)
	rightIndex := len(rightLines)
	for leftIndex > prefix && rightIndex > prefix {
		if leftLines[leftIndex-1] != rightLines[rightIndex-1] {
			break
		}
		leftIndex--
		rightIndex--
	}

	return leftIndex, rightIndex
}

func newDiffWindow(prefix, oldSuffix, newSuffix, oldLength, newLength int) diffWindow {
	return diffWindow{
		oldStart: max(0, prefix-editDiffContextLines),
		oldEnd:   min(oldLength, oldSuffix+editDiffContextLines),
		newStart: max(0, prefix-editDiffContextLines),
		newEnd:   min(newLength, newSuffix+editDiffContextLines),
		prefix:   prefix,
	}
}

func windowSize(window diffWindow) int {
	oldSize := max(0, window.oldEnd-window.oldStart)
	newChangedSize := max(0, window.newEnd-window.newStart)

	return oldSize + newChangedSize
}

func appendContextLines(
	output []string,
	marker rune,
	lines []string,
	start int,
	end int,
	lineNumberWidth int,
) []string {
	for lineIndex := start; lineIndex < end; lineIndex++ {
		lineNumber := fmt.Sprintf("%*d", lineNumberWidth, lineIndex+1)
		output = append(output, fmt.Sprintf("%c%s %s", marker, lineNumber, lines[lineIndex]))
	}

	return output
}
