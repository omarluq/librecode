package terminal

import (
	"strings"

	"github.com/omarluq/librecode/internal/model"
)

func contextContributorLines(contributors []model.TokenContributor) []string {
	if len(contributors) == 0 {
		return nil
	}
	lines := make([]string, 0, len(contributors))
	for index := range contributors {
		contributor := contributors[index]
		if contributor.Tokens <= 0 {
			continue
		}
		lines = append(lines, contextContributorLine(&contributor))
	}

	return lines
}

func contextContributorLine(contributor *model.TokenContributor) string {
	parts := []string{
		"  - " + contributor.Label,
		compactCount(contributor.Tokens),
	}
	if contributor.Role != "" {
		parts = append(parts, contributor.Role)
	}
	if contributor.Preview != "" {
		parts = append(parts, "— "+contributor.Preview)
	}

	return strings.Join(parts, " ")
}
