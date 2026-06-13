package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
)

func TestFrontmatterExtractsYAMLMetadata(t *testing.T) {
	t.Parallel()

	content := frontmatterDelimiter + "\nname: fix\ndescription: Fix bugs\n" + frontmatterDelimiter + "\nBody\n"
	metadata, body := core.Frontmatter(content)

	assert.Equal(t, "name: fix\ndescription: Fix bugs", string(metadata))
	assert.Equal(t, "Body\n", body)
}

func TestFrontmatterKeepsPlainMarkdown(t *testing.T) {
	t.Parallel()

	metadata, body := core.Frontmatter("plain body")

	assert.Empty(t, metadata)
	assert.Equal(t, "plain body", body)
}
