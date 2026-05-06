package core

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontmatter extracts YAML frontmatter delimited by --- from markdown content.
func ParseFrontmatter[T any](content string) (metadata T, body string, err error) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return metadata, content, nil
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
		return metadata, content, nil
	}

	frontmatter := strings.TrimPrefix(content[:endIndex], "---")
	frontmatter = strings.TrimSpace(frontmatter)
	body = content[endIndex+separatorLength:]
	if strings.TrimSpace(frontmatter) == "" {
		return metadata, body, nil
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
		return metadata, body, err
	}

	return metadata, body, nil
}
