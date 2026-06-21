package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	searchTestGoGlob      = "*.go"
	searchTestRecursiveGo = "**/*.go"
)

func TestFindToolFind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup       func(t *testing.T, root string)
		input       FindInput
		name        string
		wantText    string
		wantErrText string
	}{
		{
			setup:       noopSearchSetup,
			input:       FindInput{Limit: nil, Pattern: " ", Path: ""},
			name:        "blank pattern",
			wantText:    "",
			wantErrText: "find pattern is required",
		},
		{
			setup:       noopSearchSetup,
			input:       FindInput{Limit: new(0), Pattern: searchTestGoGlob, Path: ""},
			name:        "invalid limit",
			wantText:    "",
			wantErrText: "find limit must be greater than zero",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "file.go", "package main")
			},
			input:       FindInput{Limit: nil, Pattern: searchTestGoGlob, Path: "file.go"},
			name:        "path must be directory",
			wantText:    "",
			wantErrText: "find path is not a directory",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "file.txt", "x")
			},
			input:       FindInput{Limit: nil, Pattern: searchTestGoGlob, Path: ""},
			name:        "no matches",
			wantText:    "No files found matching pattern",
			wantErrText: "",
		},
		{
			setup:       setupFindLimitFixture,
			input:       FindInput{Limit: new(1), Pattern: searchTestRecursiveGo, Path: ""},
			name:        "matches skip ignored directories and report limit",
			wantText:    "a.go\n\n[1 results limit reached",
			wantErrText: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			testCase.setup(t, root)

			result, err := NewFindTool(root).Find(context.Background(), testCase.input)
			assertToolResult(t, result, err, testCase.wantText, testCase.wantErrText)
		})
	}
}

func TestFindToolExecuteRejectsWrongJSONType(t *testing.T) {
	t.Parallel()

	_, err := NewFindTool(t.TempDir()).Execute(context.Background(), testArguments(`{"pattern":42}`))

	require.Error(t, err)
}

func TestGrepToolGrepValidation(t *testing.T) {
	t.Parallel()

	tests := []struct { //nolint:govet // Table-driven tests prefer readable field order over fieldalignment.
		input       GrepInput
		name        string
		wantErrText string
	}{
		{
			input:       grepInput(" "),
			name:        "blank pattern",
			wantErrText: "grep pattern is required",
		},
		{
			input:       grepInputWithContext("x", new(-1)),
			name:        "negative context",
			wantErrText: "grep context cannot be negative",
		},
		{
			input:       grepInput("["),
			name:        "invalid regexp",
			wantErrText: "compile grep pattern",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := NewGrepTool(t.TempDir()).Grep(context.Background(), testCase.input)
			assertToolResult(t, result, err, "", testCase.wantErrText)
		})
	}
}

func TestGrepToolGrepResults(t *testing.T) {
	t.Parallel()

	tests := []struct { //nolint:govet // Table-driven tests prefer readable field order over fieldalignment.
		setup    func(t *testing.T, root string)
		input    GrepInput
		name     string
		wantText string
	}{
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "file.txt", "alpha")
			},
			input:    grepInput("beta"),
			name:     "no matches",
			wantText: "No matches found",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "file.txt", "before\nNeedle\nafter\nneedle again")
			},
			input: GrepInput{
				Context:    new(1),
				Limit:      new(1),
				Pattern:    "needle",
				Path:       "",
				Glob:       "",
				IgnoreCase: true,
				Literal:    true,
			},
			name:     "literal ignore case with context and limit notice",
			wantText: "file.txt-1- before\nfile.txt:2: Needle\nfile.txt-3- after\n\n[1 matches limit reached",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "a.go", "package main")
				writeSearchFile(t, root, "a.txt", "package text")
			},
			input:    grepInputWithGlob("package", searchTestGoGlob),
			name:     "directory glob filters files",
			wantText: "a.go:1: package main",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "binary.dat", "match\x00match")
			},
			input:    grepInput("match"),
			name:     "binary file is ignored",
			wantText: "No matches found",
		},
		{
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSearchFile(t, root, "long.txt", strings.Repeat("x", GrepMaxLineLength+10))
			},
			input:    grepInput("x+"),
			name:     "long lines report truncation",
			wantText: "Some lines truncated",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			testCase.setup(t, root)

			result, err := NewGrepTool(root).Grep(context.Background(), testCase.input)
			assertToolResult(t, result, err, testCase.wantText, "")
		})
	}
}

func TestGrepToolExecuteRejectsWrongJSONType(t *testing.T) {
	t.Parallel()

	_, err := NewGrepTool(t.TempDir()).Execute(context.Background(), testArguments(`{"pattern":42}`))

	require.Error(t, err)
}

func TestRunGrepSearchRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runGrepSearch(ctx, grepSearch{
		matcher:      func(string) bool { return true },
		targets:      []grepTarget{{absolutePath: "x", displayPath: "x"}},
		contextLines: 0,
		limit:        1,
	})

	require.ErrorIs(t, err, context.Canceled)
}

func noopSearchSetup(t *testing.T, _ string) {
	t.Helper()
}

func setupFindLimitFixture(t *testing.T, root string) {
	t.Helper()

	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), privateDirMode))
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), privateDirMode))
	writeSearchFile(t, root, "a.go", "package a")
	writeSearchFile(t, root, filepath.Join("sub", "b.go"), "package b")
	writeSearchFile(t, root, filepath.Join(".git", "hidden.go"), "package hidden")
}

func writeSearchFile(t *testing.T, root, name, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), privateFileMode))
}

func grepInput(pattern string) GrepInput {
	return GrepInput{Context: nil, Limit: nil, Pattern: pattern, Path: "", Glob: "", IgnoreCase: false, Literal: false}
}

func grepInputWithContext(pattern string, contextLines *int) GrepInput {
	input := grepInput(pattern)
	input.Context = contextLines

	return input
}

func grepInputWithGlob(pattern, glob string) GrepInput {
	input := grepInput(pattern)
	input.Glob = glob

	return input
}
