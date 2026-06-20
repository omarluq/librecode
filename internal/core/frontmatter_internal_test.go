package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testFrontmatterDelimiter = "---"

func TestParseSkillFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		content     string
		name        string
		wantBody    string
		wantName    string
		wantErrText string
	}{
		{
			content: strings.Join([]string{
				testFrontmatterDelimiter,
				"name: fix",
				"description: Fix bugs",
				testFrontmatterDelimiter,
				"Body",
				"",
			}, "\n"),
			name:        "yaml metadata",
			wantBody:    "Body\n",
			wantName:    "fix",
			wantErrText: "",
		},
		{
			content:     "plain body",
			name:        "plain markdown",
			wantBody:    "plain body",
			wantName:    "",
			wantErrText: "",
		},
		{
			content:     testFrontmatterDelimiter + "\nname: fix",
			name:        "unterminated frontmatter stays body",
			wantBody:    testFrontmatterDelimiter + "\nname: fix",
			wantName:    "",
			wantErrText: "",
		},
		{
			content:     testFrontmatterDelimiter + "\nname: [\n" + testFrontmatterDelimiter + "\nBody\n",
			name:        "invalid yaml",
			wantBody:    "",
			wantName:    "",
			wantErrText: "parse frontmatter",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			metadata, body, err := parseSkillFrontmatter(testCase.content)
			if testCase.wantErrText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrText)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantBody, body)
			assert.Equal(t, testCase.wantName, metadata.Name)
		})
	}
}

func TestParseSkillFrontmatterParsesNameDescriptionAndBody(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		testFrontmatterDelimiter,
		"name: fix",
		"description: Fix bugs",
		testFrontmatterDelimiter,
		"Body.",
	}, "\n")

	metadata, body, err := parseSkillFrontmatter(content)
	require.NoError(t, err)
	assert.Equal(t, "fix", metadata.Name)
	assert.Equal(t, "Fix bugs", metadata.Description)
	assert.Equal(t, "Body.", body)
}
