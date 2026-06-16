package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	cloneContributorSystem = "system"
	cloneContributorTool   = "tool"
)

func TestCloneTokenContributors(t *testing.T) {
	t.Parallel()

	contributors := []TokenContributor{
		{Label: cloneContributorSystem, Role: cloneContributorSystem, Preview: "sys", Tokens: 10, Chars: 40},
		{Label: "tools", Role: cloneContributorTool, Preview: cloneContributorTool, Tokens: 20, Chars: 80},
	}

	cloned := CloneTokenContributors(contributors)
	contributors[0].Label = "mutated"
	contributors[0].Tokens = 99

	assert.Equal(t, []TokenContributor{
		{Label: cloneContributorSystem, Role: cloneContributorSystem, Preview: "sys", Tokens: 10, Chars: 40},
		{Label: "tools", Role: cloneContributorTool, Preview: cloneContributorTool, Tokens: 20, Chars: 80},
	}, cloned)
}

func TestCloneTokenContributorsPreservesEmptyAsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, CloneTokenContributors(nil))
	assert.Nil(t, CloneTokenContributors([]TokenContributor{}))
}
