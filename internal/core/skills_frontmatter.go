package core

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Metadata               map[string]any `yaml:"metadata"`
	Name                   string         `yaml:"name"`
	Description            string         `yaml:"description"`
	License                string         `yaml:"license"`
	Compatibility          string         `yaml:"compatibility"`
	AllowedTools           allowedTools   `yaml:"allowed-tools"`
	UserInvocable          bool           `yaml:"user-invocable"`
	DisableModelInvocation bool           `yaml:"disable-model-invocation"`
}

type allowedTools []string

func (tools *allowedTools) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Tag == "!!null" {
		*tools = []string{}
		return nil
	}

	switch value.Kind {
	case yaml.ScalarNode:
		*tools = strings.Fields(value.Value)
		return nil
	case yaml.SequenceNode:
		return unmarshalAllowedToolsSequence(tools, value.Content)
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		return fmt.Errorf("allowed-tools must be a string or string list")
	default:
		return fmt.Errorf("allowed-tools has unsupported YAML kind %d", value.Kind)
	}
}

func unmarshalAllowedToolsSequence(tools *allowedTools, items []*yaml.Node) error {
	parsed := make([]string, 0, len(items))
	for _, item := range items {
		if item.Kind != yaml.ScalarNode {
			return fmt.Errorf("allowed-tools entries must be strings")
		}
		trimmed := strings.TrimSpace(item.Value)
		if trimmed != "" {
			parsed = append(parsed, trimmed)
		}
	}
	*tools = parsed

	return nil
}
