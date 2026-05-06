package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

type testFrontmatter struct {
	Description string `yaml:"description"`
	Name        string `yaml:"name"`
}

func TestParseFrontmatterExtractsYAMLMetadata(t *testing.T) {
	t.Parallel()

	content := frontmatterDelimiter + "\nname: fix\ndescription: Fix bugs\n" + frontmatterDelimiter + "\nBody\n"
	metadata, body, err := core.ParseFrontmatter[testFrontmatter](content)
	require.NoError(t, err)

	assert.Equal(t, testFrontmatter{Description: "Fix bugs", Name: "fix"}, metadata)
	assert.Equal(t, "Body\n", body)
}

func TestParseFrontmatterKeepsPlainMarkdown(t *testing.T) {
	t.Parallel()

	metadata, body, err := core.ParseFrontmatter[testFrontmatter]("plain body")
	require.NoError(t, err)

	assert.Equal(t, testFrontmatter{Description: "", Name: ""}, metadata)
	assert.Equal(t, "plain body", body)
}
