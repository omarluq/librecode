package tool

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type generateDiffInternalCase struct {
	name             string
	oldContent       string
	newContent       string
	wantDiffContains string
	wantFirstLine    int
	wantTruncated    bool
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
			name:             "insertion reports inserted line",
			oldContent:       "one\nthree\n",
			newContent:       "one\ntwo\nthree\n",
			wantDiffContains: "+two",
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
