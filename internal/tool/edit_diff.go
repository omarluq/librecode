package tool

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Replacement is one exact text replacement for the edit tool.
type Replacement struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// EditDetails describes the applied edit diff.
type EditDetails struct {
	Diff             string `json:"diff"`
	FirstChangedLine int    `json:"first_changed_line,omitempty"`
}

type fuzzyMatchResult struct {
	contentForReplacement string
	index                 int
	matchLength           int
	found                 bool
	usedFuzzyMatch        bool
}

type matchedEdit struct {
	newText     string
	editIndex   int
	matchIndex  int
	matchLength int
}

type appliedEdits struct {
	baseContent string
	newContent  string
}

func detectLineEnding(content string) string {
	crlfIndex := strings.Index(content, "\r\n")
	lfIndex := strings.Index(content, "\n")
	if lfIndex == -1 || crlfIndex == -1 {
		return "\n"
	}
	if crlfIndex < lfIndex {
		return "\r\n"
	}

	return "\n"
}

func normalizeToLF(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}

	return text
}

func stripBOM(content string) (bom, text string) {
	if after, ok := strings.CutPrefix(content, "\uFEFF"); ok {
		return "\uFEFF", after
	}

	return "", content
}

func normalizeForFuzzyMatch(text string) string {
	normalizedText := norm.NFKC.String(text)
	lines := strings.Split(normalizedText, "\n")
	for lineIndex := range lines {
		lines[lineIndex] = strings.TrimRightFunc(lines[lineIndex], unicode.IsSpace)
	}

	return replaceFuzzyRunes(strings.Join(lines, "\n"))
}

func replaceFuzzyRunes(text string) string {
	replacer := strings.NewReplacer(
		"\u2018", "'",
		"\u2019", "'",
		"\u201A", "'",
		"\u201B", "'",
		"\u201C", "\"",
		"\u201D", "\"",
		"\u201E", "\"",
		"\u201F", "\"",
		"\u2010", "-",
		"\u2011", "-",
		"\u2012", "-",
		"\u2013", "-",
		"\u2014", "-",
		"\u2015", "-",
		"\u2212", "-",
	)

	return unicodeSpaceReplacer.Replace(replacer.Replace(text))
}

func fuzzyFindText(content, oldText string) fuzzyMatchResult {
	if exactIndex := strings.Index(content, oldText); exactIndex != -1 {
		return fuzzyMatchResult{
			found:                 true,
			usedFuzzyMatch:        false,
			index:                 exactIndex,
			matchLength:           len(oldText),
			contentForReplacement: content,
		}
	}

	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	fuzzyIndex := strings.Index(fuzzyContent, fuzzyOldText)
	if fuzzyIndex == -1 {
		return fuzzyMatchResult{
			found:                 false,
			usedFuzzyMatch:        false,
			index:                 -1,
			matchLength:           0,
			contentForReplacement: content,
		}
	}

	return fuzzyMatchResult{
		found:                 true,
		usedFuzzyMatch:        true,
		index:                 fuzzyIndex,
		matchLength:           len(fuzzyOldText),
		contentForReplacement: fuzzyContent,
	}
}

func applyEditsToNormalizedContent(
	normalizedContent string,
	edits []Replacement,
	displayPath string,
) (appliedEdits, error) {
	normalizedEdits, err := normalizeEdits(edits, displayPath)
	if err != nil {
		return appliedEdits{baseContent: "", newContent: ""}, err
	}

	baseContent := normalizedContent
	for _, edit := range normalizedEdits {
		if fuzzyFindText(normalizedContent, edit.OldText).usedFuzzyMatch {
			baseContent = normalizeForFuzzyMatch(normalizedContent)
			break
		}
	}

	matchedEdits, err := collectMatchedEdits(baseContent, normalizedEdits, displayPath)
	if err != nil {
		return appliedEdits{baseContent: "", newContent: ""}, err
	}
	if err := rejectOverlappingEdits(matchedEdits, displayPath); err != nil {
		return appliedEdits{baseContent: "", newContent: ""}, err
	}

	newContent := applyMatchedEdits(baseContent, matchedEdits)
	if baseContent == newContent {
		return appliedEdits{baseContent: "", newContent: ""}, noChangeError(displayPath, len(normalizedEdits))
	}

	return appliedEdits{baseContent: baseContent, newContent: newContent}, nil
}

func normalizeEdits(edits []Replacement, displayPath string) ([]Replacement, error) {
	if len(edits) == 0 {
		return []Replacement{}, fmt.Errorf("edit tool input is invalid: edits must contain at least one replacement")
	}

	normalizedEdits := make([]Replacement, 0, len(edits))
	for editIndex, edit := range edits {
		if edit.OldText == "" {
			return []Replacement{}, emptyOldTextError(displayPath, editIndex, len(edits))
		}
		normalizedEdits = append(normalizedEdits, Replacement{
			OldText: normalizeToLF(edit.OldText),
			NewText: normalizeToLF(edit.NewText),
		})
	}

	return normalizedEdits, nil
}

func collectMatchedEdits(baseContent string, edits []Replacement, displayPath string) ([]matchedEdit, error) {
	matchedEdits := make([]matchedEdit, 0, len(edits))
	for editIndex, edit := range edits {
		matchResult := fuzzyFindText(baseContent, edit.OldText)
		if !matchResult.found {
			return []matchedEdit{}, notFoundError(displayPath, editIndex, len(edits))
		}
		occurrences := countOccurrences(baseContent, edit.OldText)
		if occurrences > 1 {
			return []matchedEdit{}, duplicateError(displayPath, editIndex, len(edits), occurrences)
		}
		matchedEdits = append(matchedEdits, matchedEdit{
			newText:     edit.NewText,
			editIndex:   editIndex,
			matchIndex:  matchResult.index,
			matchLength: matchResult.matchLength,
		})
	}

	return matchedEdits, nil
}

func countOccurrences(content, oldText string) int {
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	if fuzzyOldText == "" {
		return 0
	}

	return strings.Count(fuzzyContent, fuzzyOldText)
}

func rejectOverlappingEdits(edits []matchedEdit, displayPath string) error {
	sortedEdits := append([]matchedEdit{}, edits...)
	sortMatchedEdits(sortedEdits)
	for editIndex := 1; editIndex < len(sortedEdits); editIndex++ {
		previousEdit := sortedEdits[editIndex-1]
		currentEdit := sortedEdits[editIndex]
		if previousEdit.matchIndex+previousEdit.matchLength > currentEdit.matchIndex {
			return fmt.Errorf(
				"edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions",
				previousEdit.editIndex,
				currentEdit.editIndex,
				displayPath,
			)
		}
	}

	return nil
}

func applyMatchedEdits(baseContent string, edits []matchedEdit) string {
	sortedEdits := append([]matchedEdit{}, edits...)
	sortMatchedEdits(sortedEdits)
	newContent := baseContent
	for editIndex := len(sortedEdits) - 1; editIndex >= 0; editIndex-- {
		edit := sortedEdits[editIndex]
		newContent = newContent[:edit.matchIndex] + edit.newText + newContent[edit.matchIndex+edit.matchLength:]
	}

	return newContent
}

func sortMatchedEdits(edits []matchedEdit) {
	for leftIndex := 1; leftIndex < len(edits); leftIndex++ {
		currentEdit := edits[leftIndex]
		rightIndex := leftIndex - 1
		for rightIndex >= 0 && edits[rightIndex].matchIndex > currentEdit.matchIndex {
			edits[rightIndex+1] = edits[rightIndex]
			rightIndex--
		}
		edits[rightIndex+1] = currentEdit
	}
}

func notFoundError(displayPath string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf(
			"could not find the exact text in %s. The old text must match exactly including whitespace",
			displayPath,
		)
	}

	return fmt.Errorf("could not find edits[%d] in %s. The oldText must match exactly", editIndex, displayPath)
}

func duplicateError(displayPath string, editIndex, totalEdits, occurrences int) error {
	if totalEdits == 1 {
		return fmt.Errorf(
			"found %d occurrences of the text in %s. The text must be unique",
			occurrences,
			displayPath,
		)
	}

	return fmt.Errorf(
		"found %d occurrences of edits[%d] in %s. Each oldText must be unique",
		occurrences,
		editIndex,
		displayPath,
	)
}

func emptyOldTextError(displayPath string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("oldText must not be empty in %s", displayPath)
	}

	return fmt.Errorf("edits[%d].oldText must not be empty in %s", editIndex, displayPath)
}

func noChangeError(displayPath string, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("no changes made to %s. The replacement produced identical content", displayPath)
	}

	return fmt.Errorf("no changes made to %s. The replacements produced identical content", displayPath)
}
