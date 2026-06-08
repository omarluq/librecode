package tool

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type unifiedHunkStartCase struct {
	name   string
	header string
	want   int
	ok     bool
}

type generateDiffInternalCase struct {
	name             string
	oldContent       string
	newContent       string
	wantDiffContains string
	wantFirstLine    int
	wantTruncated    bool
}

func TestParseUnifiedHunkStart(t *testing.T) {
	t.Parallel()

	tests := []unifiedHunkStartCase{
		{name: "standard hunk", header: "@@ -23,8 +23,8 @@", want: 23, ok: true},
		{name: "single line range", header: "@@ -7 +8 @@", want: 7, ok: true},
		{name: "zero old start normalizes to one", header: "@@ -0,0 +1,3 @@", want: 1, ok: true},
		{name: "insertion only uses new range start", header: "@@ -9,0 +10,3 @@", want: 10, ok: true},
		{name: "missing new range", header: "@@ -1,2 @@", want: 0, ok: false},
		{name: "invalid old range", header: "@@ -x,2 +1,2 @@", want: 0, ok: false},
		{name: "invalid new range", header: "@@ -1,0 +x,2 @@", want: 0, ok: false},
		{name: "invalid old length", header: "@@ -1,x +1,2 @@", want: 0, ok: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseUnifiedHunkStart(testCase.header)

			assert.Equal(t, testCase.ok, ok)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestGenerateDiffStringInternalBranches(t *testing.T) {
	t.Parallel()

	tests := []generateDiffInternalCase{
		{
			name:             "no edits",
			oldContent:       "same\n",
			newContent:       "same\n",
			wantDiffContains: "",
			wantFirstLine:    0,
			wantTruncated:    false,
		},
		{
			name:             "deletion reports deleted line",
			oldContent:       "one\ntwo\nthree\n",
			newContent:       "one\nthree\n",
			wantDiffContains: "-two",
			wantFirstLine:    2,
			wantTruncated:    false,
		},
		{
			name:             "large diff truncates",
			oldContent:       strings.Repeat("old\n", editDiffMaxLines+50),
			newContent:       strings.Repeat("new\n", editDiffMaxLines+50),
			wantDiffContains: "--- before",
			wantFirstLine:    1,
			wantTruncated:    true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			details, err := generateDiffString(testCase.oldContent, testCase.newContent)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantFirstLine, details.FirstChangedLine)
			assert.Equal(t, testCase.wantTruncated, details.Truncated)
			if testCase.wantDiffContains != "" {
				assert.Contains(t, details.Diff, testCase.wantDiffContains)
			} else {
				assert.Empty(t, details.Diff)
			}
		})
	}
}
