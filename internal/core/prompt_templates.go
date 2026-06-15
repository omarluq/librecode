package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

const maxPromptDescriptionLength = 60

// PromptTemplate is a markdown-backed slash prompt.
type PromptTemplate struct {
	SourceInfo   SourceInfo `json:"source_info"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	ArgumentHint string     `json:"argument_hint,omitempty"`
	Content      string     `json:"content"`
	FilePath     string     `json:"file_path"`
}

// LoadPromptTemplatesOptions controls prompt discovery.
type LoadPromptTemplatesOptions struct {
	CWD             string   `json:"cwd"`
	AgentDir        string   `json:"agent_dir"`
	PromptPaths     []string `json:"prompt_paths"`
	IncludeDefaults bool     `json:"include_defaults"`
}

// LoadPromptTemplatesResult contains loaded prompts and diagnostics.
type LoadPromptTemplatesResult struct {
	Prompts     []PromptTemplate     `json:"prompts"`
	Diagnostics []ResourceDiagnostic `json:"diagnostics"`
}

type promptFrontmatter struct {
	Description  string `yaml:"description"`
	ArgumentHint string `yaml:"argument_hint"`
}

// LoadPromptTemplates loads prompt templates from defaults and explicit paths.
func LoadPromptTemplates(options *LoadPromptTemplatesOptions) LoadPromptTemplatesResult {
	resolvedOptions := promptTemplateOptions(options)
	paths := promptTemplatePaths(resolvedOptions)
	result := LoadPromptTemplatesResult{Prompts: []PromptTemplate{}, Diagnostics: []ResourceDiagnostic{}}

	for _, rawPath := range paths {
		templates, diagnostics := loadPromptTemplatePath(rawPath, resolvedOptions.CWD, resolvedOptions.AgentDir)
		result.Prompts = append(result.Prompts, templates...)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}

	return result
}

// DedupePromptTemplates removes duplicate slash prompt names and reports collisions.
func DedupePromptTemplates(prompts []PromptTemplate) LoadPromptTemplatesResult {
	result := LoadPromptTemplatesResult{Prompts: []PromptTemplate{}, Diagnostics: []ResourceDiagnostic{}}
	seen := map[string]PromptTemplate{}

	for index := range prompts {
		prompt := prompts[index]
		if existing, ok := seen[prompt.Name]; ok {
			name := "/" + prompt.Name
			result.Diagnostics = append(result.Diagnostics,
				collisionResourceDiagnostic(resourceTypePrompt, name, existing.FilePath, prompt.FilePath),
			)

			continue
		}

		seen[prompt.Name] = prompt
		result.Prompts = append(result.Prompts, prompt)
	}

	return result
}

func promptTemplateOptions(options *LoadPromptTemplatesOptions) *LoadPromptTemplatesOptions {
	if options != nil {
		return options
	}

	return &LoadPromptTemplatesOptions{
		CWD:             "",
		AgentDir:        "",
		PromptPaths:     nil,
		IncludeDefaults: false,
	}
}

func promptTemplatePaths(options *LoadPromptTemplatesOptions) []string {
	paths := []string{}
	if options.IncludeDefaults {
		paths = append(
			paths,
			filepath.Join(options.AgentDir, promptDirName),
			filepath.Join(options.CWD, ConfigDirName, promptDirName),
		)
	}

	return append(paths, options.PromptPaths...)
}

func loadPromptTemplatePath(rawPath, cwd, agentDir string) ([]PromptTemplate, []ResourceDiagnostic) {
	resolvedPath := resolveResourcePath(rawPath, cwd)
	if !resourcePathExists(resolvedPath) {
		return []PromptTemplate{}, []ResourceDiagnostic{}
	}

	info, err := statResource(resolvedPath)
	if err != nil {
		return []PromptTemplate{}, []ResourceDiagnostic{warningDiagnostic(err.Error(), resolvedPath)}
	}

	if info.IsDir() {
		return loadPromptTemplatesFromDir(resolvedPath, cwd, agentDir)
	}

	if info.Mode().IsRegular() && strings.HasSuffix(resolvedPath, ".md") {
		sourceInfo := sourceInfoForPrompt(resolvedPath, cwd, agentDir)

		template, err := loadPromptTemplateFromFile(resolvedPath, &sourceInfo)
		if err != nil {
			return []PromptTemplate{}, []ResourceDiagnostic{warningDiagnostic(err.Error(), resolvedPath)}
		}

		return []PromptTemplate{template}, []ResourceDiagnostic{}
	}

	return []PromptTemplate{}, []ResourceDiagnostic{}
}

func loadPromptTemplatesFromDir(dir, cwd, agentDir string) ([]PromptTemplate, []ResourceDiagnostic) {
	entries, err := readResourceDir(dir)
	if err != nil {
		return []PromptTemplate{}, []ResourceDiagnostic{warningDiagnostic(err.Error(), dir)}
	}

	markdownFiles := lo.Filter(entries, func(entry os.DirEntry, _ int) bool {
		return !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md")
	})

	result := LoadPromptTemplatesResult{Prompts: []PromptTemplate{}, Diagnostics: []ResourceDiagnostic{}}

	for _, entry := range markdownFiles {
		filePath := filepath.Join(dir, entry.Name())
		sourceInfo := sourceInfoForPrompt(filePath, cwd, agentDir)

		template, err := loadPromptTemplateFromFile(filePath, &sourceInfo)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, warningDiagnostic(err.Error(), filePath))

			continue
		}

		result.Prompts = append(result.Prompts, template)
	}

	return result.Prompts, result.Diagnostics
}

func loadPromptTemplateFromFile(filePath string, sourceInfo *SourceInfo) (PromptTemplate, error) {
	content, err := readResourceFile(filePath)
	if err != nil {
		return PromptTemplate{}, err
	}

	frontmatter, body, err := parsePromptFrontmatter(content)
	if err != nil {
		return PromptTemplate{}, oops.In("core").Code("prompt_frontmatter").Wrapf(err, "parse prompt frontmatter")
	}

	description := frontmatter.Description
	if description == "" {
		description = firstDescriptionLine(body)
	}

	return PromptTemplate{
		SourceInfo:   *sourceInfo,
		Name:         strings.TrimSuffix(filepath.Base(filePath), ".md"),
		Description:  description,
		ArgumentHint: frontmatter.ArgumentHint,
		Content:      body,
		FilePath:     filePath,
	}, nil
}

func sourceInfoForPrompt(filePath, cwd, agentDir string) SourceInfo {
	globalPromptsDir := filepath.Join(agentDir, promptDirName)
	projectPromptsDir := filepath.Join(cwd, ConfigDirName, promptDirName)
	scope := SourceScopeTemporary

	baseDir := filepath.Dir(filePath)
	if isUnderPath(filePath, globalPromptsDir) {
		scope = SourceScopeUser
		baseDir = globalPromptsDir
	} else if isUnderPath(filePath, projectPromptsDir) {
		scope = SourceScopeProject
		baseDir = projectPromptsDir
	}

	return NewSourceInfo(filePath, SourceInfoOptions{
		Scope:   scope,
		Origin:  SourceOriginTopLevel,
		BaseDir: baseDir,
		Source:  resourceSourceLocal,
	})
}

func firstDescriptionLine(content string) string {
	line, ok := lo.Find(strings.Split(content, "\n"), func(line string) bool {
		return strings.TrimSpace(line) != ""
	})
	if !ok {
		return ""
	}

	trimmed := strings.TrimSpace(line)
	if len(trimmed) > maxPromptDescriptionLength {
		return trimmed[:maxPromptDescriptionLength] + "..."
	}

	return trimmed
}
