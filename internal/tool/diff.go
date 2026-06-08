package tool

import (
	"fmt"
	"strconv"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/samber/oops"
)

const (
	editDiffContextLines = 4
	editDiffMaxLines     = 400
	editDiffMaxBytes     = 24 * 1024
)

func generateDiffString(oldContent, newContent string) (EditDetails, error) {
	edits := udiff.Lines(oldContent, newContent)
	if len(edits) == 0 {
		return EditDetails{Diff: "", FirstChangedLine: 0, Truncated: false}, nil
	}

	diff, err := udiff.ToUnified("before", "after", oldContent, edits, editDiffContextLines)
	if err != nil {
		return EditDetails{Diff: "", FirstChangedLine: 0, Truncated: false}, oops.
			In("tool").
			Code("edit_generate_diff").
			Wrapf(err, "generate unified diff")
	}

	truncation := TruncateHead(diff, TruncationOptions{MaxLines: editDiffMaxLines, MaxBytes: editDiffMaxBytes})
	return EditDetails{
		Diff:             truncation.Content,
		FirstChangedLine: firstChangedLineFromUnifiedDiff(diff),
		Truncated:        truncation.Truncated,
	}, nil
}

func firstChangedLineFromUnifiedDiff(diff string) int {
	currentLine := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@") {
			lineNumber, ok := parseUnifiedHunkStart(line)
			if !ok {
				continue
			}
			currentLine = lineNumber
			continue
		}
		if currentLine == 0 || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "-"):
			return currentLine
		case strings.HasPrefix(line, "+"):
			return currentLine
		case strings.HasPrefix(line, " "):
			currentLine++
		}
	}

	return 0
}

func parseUnifiedHunkStart(header string) (int, bool) {
	parts := strings.Split(header, " ")
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "-") {
		return 0, false
	}
	lineRange := strings.TrimPrefix(parts[1], "-")
	lineText, _, _ := strings.Cut(lineRange, ",")
	lineNumber, err := strconv.Atoi(lineText)
	if err != nil {
		return 0, false
	}
	if lineNumber == 0 {
		return 1, true
	}

	return lineNumber, true
}

func diffTruncationMessage(details EditDetails) string {
	if !details.Truncated {
		return ""
	}

	return fmt.Sprintf("\n\n[diff truncated to %d lines / %s]", editDiffMaxLines, FormatSize(editDiffMaxBytes))
}
