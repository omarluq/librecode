package core

import (
	"strings"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

func parseSkillFrontmatter(content string) (skillFrontmatter, string, error) {
	var metadata skillFrontmatter

	body, err := frontmatter.Parse(strings.NewReader(content), &metadata, skillFrontmatterFormat())
	if err != nil {
		return metadata, "", coreError(err, "parse frontmatter")
	}

	return metadata, string(body), nil
}

func skillFrontmatterFormat() *frontmatter.Format {
	return frontmatter.NewFormat("---", "---", yaml.Unmarshal)
}
