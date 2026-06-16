package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testWindowsOS = "windows"
	testLineOne   = "one"
	testABC       = "abc"
)

func TestFormatBashResultSuccess(t *testing.T) {
	t.Parallel()

	result, err := formatBashResult(context.Background(), BashInput{Timeout: nil, Command: ""}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "(no output)", result.Text())

	result, err = formatBashResult(context.Background(), BashInput{Timeout: nil, Command: ""}, []byte("hello"), nil)
	require.NoError(t, err)
	assert.Equal(t, "hello", result.Text())
}

func TestFormatBashResultErrors(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := formatBashResult(canceledCtx, BashInput{Timeout: nil, Command: ""}, []byte("partial"), nil)
	require.Error(t, err)
	assert.Empty(t, result.Content)
	assert.Contains(t, err.Error(), "partial\n\nCommand aborted")

	timedOut := float64(1)
	timedOutCtx, timeoutCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(timeoutCancel)

	result, err = formatBashResult(timedOutCtx, BashInput{Timeout: &timedOut, Command: ""}, []byte("partial"), nil)
	require.Error(t, err)
	assert.Empty(t, result.Content)
	assert.Contains(t, err.Error(), "Command timed out after 1 seconds")

	result, err = formatBashResult(
		context.Background(),
		BashInput{Timeout: nil, Command: ""},
		[]byte("partial"),
		assert.AnError,
	)
	require.Error(t, err)
	assert.Empty(t, result.Content)
	assert.Contains(t, err.Error(), assert.AnError.Error())
}

func TestFormatBashWaitErrorIncludesExitCode(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == testWindowsOS {
		t.Skip("test relies on Unix shell exit semantics")
	}

	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exit 7")
	runErr := cmd.Run()
	require.Error(t, runErr)

	result, err := formatBashWaitError([]byte("boom"), runErr)
	require.Error(t, err)
	assert.Empty(t, result.Content)
	assert.Contains(t, err.Error(), "boom")
	assert.Contains(t, err.Error(), "Command exited with code 7")
}

func TestBashTruncationNotice(t *testing.T) {
	t.Parallel()

	partial := TruncationResult{
		TruncatedBy:           TruncatedByNone,
		Content:               "",
		TotalLines:            3,
		TotalBytes:            0,
		OutputLines:           1,
		OutputBytes:           8,
		MaxLines:              0,
		MaxBytes:              0,
		Truncated:             false,
		LastLinePartial:       true,
		FirstLineExceedsLimit: false,
	}
	assert.Equal(t,
		"[Showing last 8B of line 3 (line is 12B). Full output: /tmp/out]",
		bashTruncationNotice(&partial, "/tmp/out", 12),
	)

	byLines := TruncationResult{
		TruncatedBy:           TruncatedByLines,
		Content:               "",
		TotalLines:            10,
		TotalBytes:            0,
		OutputLines:           3,
		OutputBytes:           0,
		MaxLines:              0,
		MaxBytes:              0,
		Truncated:             false,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
	}
	assert.Equal(t,
		"[Showing lines 8-10 of 10. Full output: /tmp/out]",
		bashTruncationNotice(&byLines, "/tmp/out", 0),
	)

	byBytes := byLines
	byBytes.TruncatedBy = TruncatedByBytes
	assert.Equal(t,
		fmt.Sprintf("[Showing lines 8-10 of 10 (%s limit). Full output: /tmp/out]", FormatSize(DefaultMaxBytes)),
		bashTruncationNotice(&byBytes, "/tmp/out", 0),
	)
}

func TestWriteFullBashOutputCreateDirectoryFailure(t *testing.T) {
	if runtime.GOOS == testWindowsOS {
		t.Skip("test relies on Unix directory permissions")
	}

	cacheFile := filepath.Join(t.TempDir(), "cache-file")
	require.NoError(t, os.WriteFile(cacheFile, []byte("not a directory"), 0o600))
	t.Setenv("XDG_CACHE_HOME", cacheFile)

	path, err := writeFullBashOutput([]byte("hello"))
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "create full bash output dir")
}

func TestTextReadResultAndFormatting(t *testing.T) {
	t.Parallel()

	two := 2
	result, err := textReadResult(
		ReadInput{Offset: &two, Limit: &two, Path: "file.txt", AllowIgnored: false},
		"one\ntwo\nthree\nfour",
	)
	require.NoError(t, err)
	assert.Equal(t, "two\nthree\n\n[1 more lines in file. Use offset=4 to continue.]", result.Text())

	tooFar := 10
	result, err = textReadResult(
		ReadInput{Offset: &tooFar, Limit: nil, Path: "file.txt", AllowIgnored: false},
		"one\ntwo",
	)
	require.Error(t, err)
	assert.Empty(t, result.Content)
	assert.Contains(t, err.Error(), "offset is beyond end of file")
}

func TestFormatReadOutputBranches(t *testing.T) {
	t.Parallel()

	input := ReadInput{Offset: nil, Limit: nil, Path: "notes.txt", AllowIgnored: false}
	firstLineTooLong := TruncationResult{
		TruncatedBy:           TruncatedByNone,
		Content:               "",
		TotalLines:            0,
		TotalBytes:            0,
		OutputLines:           0,
		OutputBytes:           0,
		MaxLines:              0,
		MaxBytes:              0,
		Truncated:             false,
		LastLinePartial:       false,
		FirstLineExceedsLimit: true,
	}
	got, details := formatReadOutput(input, []string{strings.Repeat("x", DefaultMaxBytes+1)}, &firstLineTooLong, 0, nil)
	assert.Contains(t, got, "exceeds 50.0KB limit")
	assert.Contains(t, details, detailTruncation)

	truncatedByLines := TruncationResult{
		TruncatedBy:           TruncatedByLines,
		Content:               "one\ntwo",
		TotalLines:            0,
		TotalBytes:            0,
		OutputLines:           2,
		OutputBytes:           0,
		MaxLines:              0,
		MaxBytes:              0,
		Truncated:             true,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
	}
	got, details = formatReadOutput(
		input,
		[]string{testLineOne, "two", "three", "four"},
		&truncatedByLines,
		0,
		nil,
	)
	assert.Contains(t, got, "[Showing lines 1-2 of 4. Use offset=3 to continue.]")
	assert.Contains(t, details, detailTruncation)

	truncatedByBytes := truncatedByLines
	truncatedByBytes.TruncatedBy = TruncatedByBytes
	truncatedByBytes.Content = testLineOne
	truncatedByBytes.OutputLines = 1
	got, details = formatReadOutput(input, []string{testLineOne, "two", "three", "four"}, &truncatedByBytes, 0, nil)
	assert.Contains(t, got, "(50.0KB limit). Use offset=2 to continue.")
	assert.Contains(t, details, detailTruncation)
}

func TestImageReadResultAndMIMEFallbacks(t *testing.T) {
	t.Parallel()

	result := imageReadResult("image/png", []byte("png-data"))
	require.Len(t, result.Content, 2)
	assert.Equal(t, ContentTypeText, result.Content[0].Type)
	assert.Equal(t, ContentTypeImage, result.Content[1].Type)
	assert.Equal(t, "image/png", result.Content[1].MIMEType)
	assert.NotEmpty(t, result.Content[1].Data)

	assert.Equal(t, "image/jpeg", detectSupportedImageMIMEType("photo.jpg", []byte("not detected")))
	assert.Equal(t, "image/png", imageMIMETypeFromExtension("icon.PNG"))
	assert.Equal(t, "image/gif", imageMIMETypeFromExtension("anim.gif"))
	assert.Equal(t, "image/webp", imageMIMETypeFromExtension("pic.webp"))
	assert.Empty(t, imageMIMETypeFromExtension("file.txt"))
}

func TestEditDiffErrorMessages(t *testing.T) {
	t.Parallel()

	assert.Contains(t, notFoundError("file.txt", 0, 1).Error(), "could not find the exact text")
	assert.Contains(t, notFoundError("file.txt", 2, 3).Error(), "edits[2]")
	assert.Contains(t, duplicateError("file.txt", 0, 1, 2).Error(), "found 2 occurrences of the text")
	assert.Contains(t, duplicateError("file.txt", 2, 3, 4).Error(), "edits[2]")
	assert.Contains(t, emptyOldTextError("file.txt", 0, 1).Error(), "oldText must not be empty")
	assert.Contains(t, emptyOldTextError("file.txt", 2, 3).Error(), "edits[2].oldText")
	assert.Contains(t, noChangeError("file.txt", 1).Error(), "replacement produced identical")
	assert.Contains(t, noChangeError("file.txt", 2).Error(), "replacements produced identical")
}

func TestApplyEditsToNormalizedContentBranches(t *testing.T) {
	t.Parallel()

	result, err := applyEditsToNormalizedContent(
		"quote: “hello”\n",
		[]Replacement{{OldText: `quote: "hello"`, NewText: "quote: bye"}},
		"file.txt",
	)
	require.NoError(t, err)
	assert.Equal(t, "quote: bye\n", result.newContent)

	_, err = applyEditsToNormalizedContent(
		"abcdef",
		[]Replacement{{OldText: testABC, NewText: "x"}, {OldText: "bcd", NewText: "y"}},
		"file.txt",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overlap")

	_, err = applyEditsToNormalizedContent(testABC, nil, "file.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edits must contain")

	_, err = applyEditsToNormalizedContent(testABC, []Replacement{{OldText: testABC, NewText: testABC}}, "file.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no changes made")
}

func TestRejectOverlappingEditsAllowsAdjacent(t *testing.T) {
	t.Parallel()

	err := rejectOverlappingEdits([]matchedEdit{
		{newText: "", editIndex: 0, matchIndex: 0, matchLength: 2},
		{newText: "", editIndex: 1, matchIndex: 2, matchLength: 2},
	}, "file.txt")
	require.NoError(t, err)
}

func TestFormatBashOutputWriteFailure(t *testing.T) {
	if runtime.GOOS == testWindowsOS {
		t.Skip("test relies on Unix directory permissions")
	}

	cacheFile := filepath.Join(t.TempDir(), "cache-file")
	require.NoError(t, os.WriteFile(cacheFile, []byte("not a directory"), 0o600))
	t.Setenv("XDG_CACHE_HOME", cacheFile)

	_, details, err := formatBashOutput([]byte(strings.Repeat("line\n", DefaultMaxLines+1)), "")
	require.Error(t, err)
	assert.Empty(t, details)
}

func TestFormatBashWaitErrorPropagatesFormattingFailure(t *testing.T) {
	if runtime.GOOS == testWindowsOS {
		t.Skip("test relies on Unix directory permissions")
	}

	cacheFile := filepath.Join(t.TempDir(), "cache-file")
	require.NoError(t, os.WriteFile(cacheFile, []byte("not a directory"), 0o600))
	t.Setenv("XDG_CACHE_HOME", cacheFile)

	result, err := formatBashWaitError([]byte(strings.Repeat("line\n", DefaultMaxLines+1)), assert.AnError)
	require.Error(t, err)
	assert.Empty(t, result.Content)
}
