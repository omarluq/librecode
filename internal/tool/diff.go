package tool

import (
	"fmt"

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

	diff, err := udiff.ToUnifiedDiff("before", "after", oldContent, edits, editDiffContextLines)
	if err != nil {
		return EditDetails{Diff: "", FirstChangedLine: 0, Truncated: false}, oops.
			In("tool").
			Code("edit_generate_diff").
			Wrapf(err, "generate unified diff")
	}

	diffText := diff.String()
	truncation := TruncateHead(diffText, TruncationOptions{MaxLines: editDiffMaxLines, MaxBytes: editDiffMaxBytes})

	return EditDetails{
		Diff:             truncation.Content,
		FirstChangedLine: firstChangedLineFromUnifiedDiff(diff),
		Truncated:        truncation.Truncated,
	}, nil
}

func firstChangedLineFromUnifiedDiff(diff udiff.UnifiedDiff) int {
	for _, hunk := range diff.Hunks {
		lineNumber := hunk.FromLine
		if lineNumber <= 0 {
			lineNumber = 1
		}

		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Delete, udiff.Insert:
				return lineNumber
			case udiff.Equal:
				lineNumber++
			}
		}
	}

	return 0
}

func diffTruncationMessage(details EditDetails) string {
	if !details.Truncated {
		return ""
	}

	return fmt.Sprintf("\n\n[diff truncated to %d lines / %s]", editDiffMaxLines, FormatSize(editDiffMaxBytes))
}
