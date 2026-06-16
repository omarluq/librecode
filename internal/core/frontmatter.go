package core

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter extracts YAML frontmatter delimited by --- from markdown content.
func Frontmatter(content string) (data []byte, body string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return nil, content
	}

	separator := "\n---\n"
	endIndex := strings.Index(content, separator)

	separatorLength := len(separator)
	if endIndex == -1 {
		separator = "\r\n---\r\n"
		endIndex = strings.Index(content, separator)
		separatorLength = len(separator)
	}

	if endIndex == -1 {
		return nil, content
	}

	frontmatter := strings.TrimPrefix(content[:endIndex], "---")
	frontmatter = strings.TrimSpace(frontmatter)

	return []byte(frontmatter), content[endIndex+separatorLength:]
}

func parseSkillFrontmatter(content string) (skillFrontmatter, string, error) {
	var metadata skillFrontmatter

	frontmatter, body := Frontmatter(content)
	if strings.TrimSpace(string(frontmatter)) == "" {
		return metadata, body, nil
	}

	if err := yaml.Unmarshal(frontmatter, &metadata); err != nil {
		return metadata, body, coreError(err, "parse frontmatter")
	}

	return metadata, body, nil
}
