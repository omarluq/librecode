package compaction

import (
	"errors"
	"fmt"
	"strings"
)

const (
	checkpointCodeDuplicate = "duplicate"
	checkpointCodeEmpty     = "empty"
	checkpointCodeMissing   = "missing"
	checkpointCodeOrder     = "order"
	checkpointCodePrefix    = "prefix"
	checkpointCodeReserved  = "reserved"

	checkpointHeadingsText = `Goal
User constraints and preferences
Completed work
Work in progress
Files changed/read
Commands and validation
Decisions
Errors and blockers
Exact next steps`
)

// ErrCheckpointStructure identifies an invalid generated checkpoint structure.
var ErrCheckpointStructure = errors.New("invalid checkpoint structure")

// CheckpointStructureError reports format defects without retaining summary text.
type CheckpointStructureError struct {
	Code    string
	Heading string
}

func (err *CheckpointStructureError) Error() string {
	if err == nil {
		return ErrCheckpointStructure.Error()
	}

	return fmt.Sprintf("%v: %s heading %q", ErrCheckpointStructure, err.Code, err.Heading)
}

func (err *CheckpointStructureError) Unwrap() error { return ErrCheckpointStructure }

// ValidateCheckpoint validates only the stable structure, never model prose.
func ValidateCheckpoint(text string) error {
	headings := strings.Split(checkpointHeadingsText, "\n")
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	wanted := 0
	hasBullet := false
	seen := make(map[string]bool, len(headings))

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if err := validateNonHeadingLine(line, wanted, headings, &hasBullet); err != nil {
			return err
		}

		if !strings.HasPrefix(line, "## ") {
			continue
		}

		heading := strings.TrimPrefix(line, "## ")
		if err := validateHeading(heading, wanted, hasBullet, headings, seen); err != nil {
			return err
		}

		seen[heading] = true
		wanted++
		hasBullet = false
	}

	return validateCheckpointEnd(wanted, hasBullet, headings)
}

func validateNonHeadingLine(line string, wanted int, headings []string, hasBullet *bool) error {
	if strings.HasPrefix(line, "### Librecode ") {
		return &CheckpointStructureError{Code: checkpointCodeReserved, Heading: strings.TrimPrefix(line, "### ")}
	}

	if strings.HasPrefix(line, "## ") {
		return nil
	}

	if wanted == 0 && strings.TrimSpace(line) != "" {
		return &CheckpointStructureError{Code: checkpointCodePrefix, Heading: headings[0]}
	}

	if wanted > 0 && strings.HasPrefix(strings.TrimSpace(line), "- ") {
		*hasBullet = true
	}

	return nil
}

func validateHeading(
	heading string,
	wanted int,
	hasBullet bool,
	headings []string,
	seen map[string]bool,
) error {
	if wanted > 0 && !hasBullet {
		return &CheckpointStructureError{Code: checkpointCodeEmpty, Heading: headings[wanted-1]}
	}

	if seen[heading] {
		return &CheckpointStructureError{Code: checkpointCodeDuplicate, Heading: heading}
	}

	if wanted >= len(headings) || heading != headings[wanted] {
		return &CheckpointStructureError{Code: checkpointCodeOrder, Heading: heading}
	}

	return nil
}

func validateCheckpointEnd(wanted int, hasBullet bool, headings []string) error {
	if wanted != len(headings) {
		return &CheckpointStructureError{Code: checkpointCodeMissing, Heading: headings[wanted]}
	}

	if !hasBullet {
		return &CheckpointStructureError{Code: checkpointCodeEmpty, Heading: headings[len(headings)-1]}
	}

	return nil
}

// RepairPrompt asks for a single structural repair without authorizing new facts.
func RepairPrompt() string {
	return checkpointContract + `

Repair only the structure of the checkpoint supplied in the user message. Preserve every factual bullet,
add no facts, and return only the complete repaired checkpoint.`
}

const checkpointContract = `Return a structured coding checkpoint with exactly these level-two headings in this order:
## Goal
## User constraints and preferences
## Completed work
## Work in progress
## Files changed/read
## Commands and validation
## Decisions
## Errors and blockers
## Exact next steps

Under every heading use concise factual "- " bullets, or exactly "- None" when no fact is known.
Return only the checkpoint. Do not invent status, command outcomes, file changes, errors, paths, commands,
or identifiers. Preserve exact commands, paths, error messages, identifiers, and user prohibitions when present.
Distinguish changed files from files only read. A tool invocation alone does not prove completion.
Do not emit headings beginning with "Librecode"; those are reserved for deterministic appendices.`
