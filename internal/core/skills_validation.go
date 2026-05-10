package core

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/samber/lo"
)

var (
	skillNamePattern               = regexp.MustCompile(`^[a-z0-9-]+$`)
	skillActivationSentencePattern = regexp.MustCompile(`[.;\n]+`)
)

func skillDiagnostics(filePath, name, parentDirName string, frontmatter *skillFrontmatter) []ResourceDiagnostic {
	messages := append(validateSkillName(name, parentDirName), validateSkillDescription(frontmatter.Description)...)
	messages = append(messages, validateSkillCompatibility(frontmatter.Compatibility)...)

	return lo.Map(messages, func(message string, _ int) ResourceDiagnostic {
		return warningDiagnostic(message, filePath)
	})
}

func validateSkillName(name, parentDirName string) []string {
	errors := []string{}
	if name != parentDirName {
		errors = append(errors, fmt.Sprintf("name %q does not match parent directory %q", name, parentDirName))
	}
	if len(name) > maxSkillNameLength {
		errors = append(errors, fmt.Sprintf("name exceeds %d characters (%d)", maxSkillNameLength, len(name)))
	}
	if !skillNamePattern.MatchString(name) {
		errors = append(errors,
			"name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)",
		)
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errors = append(errors, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errors = append(errors, "name must not contain consecutive hyphens")
	}

	return errors
}

func validateSkillDescription(description string) []string {
	if strings.TrimSpace(description) == "" {
		return []string{"description is required"}
	}
	if len(description) > maxSkillDescriptionLength {
		message := fmt.Sprintf(
			"description exceeds %d characters (%d)",
			maxSkillDescriptionLength,
			len(description),
		)

		return []string{message}
	}

	return []string{}
}

func validateSkillCompatibility(compatibility string) []string {
	if len(compatibility) <= maxSkillCompatibilitySize {
		return []string{}
	}

	return []string{fmt.Sprintf(
		"compatibility exceeds %d characters (%d)",
		maxSkillCompatibilitySize,
		len(compatibility),
	)}
}
