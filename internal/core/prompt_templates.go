package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

const maxPromptDescriptionLength = 60

var positionArgumentPattern = regexp.MustCompile(`\$(\d+)`)

// PromptTemplate is a markdown-backed slash prompt.
type PromptTemplate struct {
	SourceInfo   SourceInfo `json:"sourceInfo"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	ArgumentHint string     `json:"argumentHint,omitempty"`
	Content      string     `json:"content"`
	FilePath     string     `json:"filePath"`
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
	ArgumentHint string `yaml:"argument-hint"`
}

// ParseCommandArgs parses bash-style quoted command arguments.
func ParseCommandArgs(argsString string) []string {
	args := []string{}
	var current strings.Builder
	var quote rune
	for _, character := range argsString {
		switch {
		case quote != 0 && character == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(character)
		case character == '\'' || character == '"':
			quote = character
		case character == ' ' || character == '\t':
			args = appendCurrentArg(args, &current)
		default:
			current.WriteRune(character)
		}
	}

	return appendCurrentArg(args, &current)
}

// SubstituteArgs replaces positional/Claude-style argument placeholders in prompt content.
func SubstituteArgs(content string, args []string) string {
	result := substitutePositionalArgs(content, args)
	result = substituteSlicedArgs(result, args)
	allArgs := strings.Join(args, " ")
	result = strings.ReplaceAll(result, "$ARGUMENTS", allArgs)
	result = strings.ReplaceAll(result, "$@", allArgs)

	return result
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

// ExpandPromptTemplate expands /name args when name matches a template.
func ExpandPromptTemplate(text string, templates []PromptTemplate) string {
	if !strings.HasPrefix(text, "/") {
		return text
	}
	templateName, argsString, found := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	if !found {
		argsString = ""
	}
	template, ok := lo.Find(templates, func(template PromptTemplate) bool {
		return template.Name == templateName
	})
	if !ok {
		return text
	}

	return SubstituteArgs(template.Content, ParseCommandArgs(argsString))
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

func appendCurrentArg(args []string, current *strings.Builder) []string {
	if current.Len() == 0 {
		return args
	}
	args = append(args, current.String())
	current.Reset()

	return args
}

func substitutePositionalArgs(content string, args []string) string {
	return positionArgumentPattern.ReplaceAllStringFunc(content, func(match string) string {
		position, err := strconv.Atoi(strings.TrimPrefix(match, "$"))
		if err != nil || position < 1 || position > len(args) {
			return ""
		}

		return args[position-1]
	})
}

func substituteSlicedArgs(content string, args []string) string {
	result := content
	for start := 1; start <= len(args); start++ {
		placeholder := "${@:" + strconv.Itoa(start) + "}"
		result = strings.ReplaceAll(result, placeholder, strings.Join(args[start-1:], " "))
		for length := 1; length <= len(args)-start+1; length++ {
			sliced := args[start-1 : start-1+length]
			placeholder = "${@:" + strconv.Itoa(start) + ":" + strconv.Itoa(length) + "}"
			result = strings.ReplaceAll(result, placeholder, strings.Join(sliced, " "))
		}
	}

	return result
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
	frontmatter, body, err := ParseFrontmatter[promptFrontmatter](content)
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
