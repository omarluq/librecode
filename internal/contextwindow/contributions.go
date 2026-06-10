package contextwindow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/samber/oops"
)

const (
	payloadContributionsKey = "contributions"
	jsonContentKey          = "content"
	jsonRoleKey             = "role"
	jsonToolNameKey         = "name"
)

// AppendContributions appends extension context blocks to result.SystemPrompt.
func AppendContributions(result *BuildResult, contributions []Contribution) {
	if result == nil || len(contributions) == 0 {
		return
	}

	builder := strings.Builder{}
	builder.WriteString(result.SystemPrompt)
	builder.WriteString("\n\n<extension_context>")
	for index := range contributions {
		contribution := contributions[index]
		result.Contributions = append(result.Contributions, contribution)
		builder.WriteString("\n<block")
		if contribution.Name != "" {
			builder.WriteString(" name=")
			builder.WriteString(strconv.Quote(contribution.Name))
		}
		builder.WriteString(" source=")
		builder.WriteString(strconv.Quote(contribution.Source))
		builder.WriteString(" role=")
		builder.WriteString(strconv.Quote(contribution.Role))
		builder.WriteString(" tokens=")
		builder.WriteString(strconv.Itoa(contribution.Tokens))
		builder.WriteString(">\n")
		builder.WriteString(contribution.Content)
		builder.WriteString("\n</block>")
	}
	builder.WriteString("\n</extension_context>")
	result.SystemPrompt = builder.String()
}

// ContributionsFromPayload parses extension context-build contributions.
func ContributionsFromPayload(payload map[string]any) ([]Contribution, error) {
	raw, found := payload[payloadContributionsKey]
	if !found || raw == nil {
		return []Contribution{}, nil
	}

	rawContributions, ok := raw.([]any)
	if !ok {
		if rawMap, mapOK := raw.(map[string]any); mapOK {
			rawContributions = numericMapValues(rawMap)
		} else {
			return nil, oops.In("contextwindow").
				Code("invalid_context_contributions").
				Errorf("context contributions must be a list")
		}
	}

	contributions := make([]Contribution, 0, len(rawContributions))
	for index, rawContribution := range rawContributions {
		contribution, err := contributionFromValue(rawContribution)
		if err != nil {
			return nil, oops.In("contextwindow").
				Code("invalid_context_contribution").
				Wrapf(err, "context contribution %d", index)
		}
		contributions = append(contributions, contribution)
	}

	return contributions, nil
}

// numericMapValues returns values ordered by consecutive 1-indexed string keys.
// A gap in the sequence, such as {"1": a, "3": b}, returns an empty slice.
func numericMapValues(values map[string]any) []any {
	items := make([]any, 0, len(values))
	for index := 1; index <= len(values); index++ {
		value, ok := values[fmt.Sprint(index)]
		if !ok {
			return []any{}
		}
		items = append(items, value)
	}

	return items
}

func contributionFromValue(value any) (Contribution, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return Contribution{}, fmt.Errorf("must be an object")
	}
	content := strings.TrimSpace(stringFromAny(object[jsonContentKey]))
	if content == "" {
		return Contribution{}, fmt.Errorf("content is required")
	}
	tokens := EstimateTokens(content)
	if tokens > ContributionMaxTokens {
		return Contribution{}, fmt.Errorf(
			"content exceeds %d-token contribution limit",
			ContributionMaxTokens,
		)
	}

	source := stringFromAny(object["source"])
	if source == "" {
		source = ContributionSourceExtension
	}
	role := stringFromAny(object[jsonRoleKey])
	if role == "" {
		role = ContributionRoleSystem
	}

	return Contribution{
		Metadata: mapFromAny(object["metadata"]),
		Source:   source,
		Name:     stringFromAny(object[jsonToolNameKey]),
		Role:     role,
		Content:  content,
		Tokens:   tokens,
	}, nil
}

func stringFromAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}

	return ""
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}

	return map[string]any{}
}
