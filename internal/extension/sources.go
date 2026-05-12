package extension

import (
	"fmt"
	"strings"
)

const (
	sourceSchemeOfficial = "official"
	sourceSchemeGitHub   = "github"
	sourceSchemePath     = "path"
)

// ConfiguredSource is one extension source entry from user configuration.
type ConfiguredSource struct {
	Source  string `json:"source" mapstructure:"source" yaml:"source"`
	Version string `json:"version" mapstructure:"version" yaml:"version"`
}

// SourceRef describes one configured extension source.
type SourceRef struct {
	Scheme  string
	Value   string
	Version string
}

// ParseSourceRef parses extension source strings like official:vim-mode or github:user/repo.
func ParseSourceRef(source, version string) (SourceRef, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return SourceRef{}, fmt.Errorf("extension: source is required")
	}

	scheme, value, ok := strings.Cut(trimmed, ":")
	if !ok || strings.TrimSpace(scheme) == "" || strings.TrimSpace(value) == "" {
		return SourceRef{}, fmt.Errorf("extension: source %q must use scheme:value form", source)
	}

	ref := SourceRef{
		Scheme:  strings.ToLower(strings.TrimSpace(scheme)),
		Value:   strings.TrimSpace(value),
		Version: strings.TrimSpace(version),
	}
	if err := ref.validate(); err != nil {
		return SourceRef{}, err
	}

	return ref, nil
}

// Key returns the stable source key used in diagnostics and lockfiles.
func (source SourceRef) Key() string {
	return source.Scheme + ":" + source.Value
}

// LocalPath returns the local path for path source refs.
func (source SourceRef) LocalPath() (string, bool) {
	if source.Scheme == sourceSchemePath {
		return source.Value, true
	}

	return "", false
}

func (source SourceRef) validate() error {
	switch source.Scheme {
	case sourceSchemeOfficial:
		return validateOfficialSource(source.Value)
	case sourceSchemeGitHub:
		return validateGitHubSource(source.Value)
	case sourceSchemePath:
		return nil
	default:
		return fmt.Errorf("extension: unsupported source scheme %q", source.Scheme)
	}
}

func validateOfficialSource(value string) error {
	if strings.Contains(value, "/") {
		return fmt.Errorf("extension: official source %q must be official:name", value)
	}

	return nil
}

func validateGitHubSource(value string) error {
	repo, subdir, _ := strings.Cut(value, "//")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("extension: github source %q must be github:owner/repo or github:owner/repo//subdir", value)
	}
	if strings.Contains(subdir, "..") {
		return fmt.Errorf("extension: github source %q has invalid subdir", value)
	}

	return nil
}
