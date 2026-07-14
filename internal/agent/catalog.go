// Package agent discovers and validates subagent definitions.
package agent

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const definitionsDir = "agents"

//go:embed builtin/*.md
var builtins embed.FS

// Definition describes one invocable subagent execution profile.
type Definition struct {
	SourceInfo   core.SourceInfo `json:"source_info"`
	Model        ModelPolicy     `json:"model"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SystemPrompt string          `json:"system_prompt"`
	Permissions  PermissionMode  `json:"permissions"`
	Tools        []tool.Name     `json:"tools"`
	Limits       Limits          `json:"limits"`
}

// ModelPolicy optionally overrides the parent model selection.
type ModelPolicy struct {
	Provider string              `json:"provider,omitempty"`
	Model    string              `json:"model,omitempty"`
	Thinking model.ThinkingLevel `json:"thinking,omitempty"`
}

// Limits bounds one agent execution. Zero values inherit service defaults.
type Limits struct {
	Timeout time.Duration `json:"timeout,omitempty"`
}

// PermissionMode controls approval-required tools for background execution.
type PermissionMode string

const (
	// PermissionInherit uses the parent execution's permission policy.
	PermissionInherit PermissionMode = "inherit"
	// PermissionDeny rejects approval-required tools.
	PermissionDeny PermissionMode = "deny"
	// PermissionAsk pauses for explicit approval.
	PermissionAsk PermissionMode = "ask"
	// PermissionAllow permits tools allowed by administrator policy.
	PermissionAllow PermissionMode = "allow"
)

// Diagnostic reports a skipped or shadowed agent definition.
type Diagnostic struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// Catalog is an immutable set of validated definitions.
type Catalog struct {
	definitions map[string]Definition
	diagnostics []Diagnostic
}

type frontmatterDefinition struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Provider    string   `yaml:"provider"`
	Model       string   `yaml:"model"`
	Thinking    string   `yaml:"thinking"`
	Timeout     string   `yaml:"timeout"`
	Permissions string   `yaml:"permissions"`
	Tools       []string `yaml:"tools"`
}

// Load discovers project, user, and built-in definitions in precedence order.
func Load(cwd string) *Catalog {
	catalog := &Catalog{definitions: map[string]Definition{}, diagnostics: []Diagnostic{}}
	catalog.loadDir(filepath.Join(cwd, core.ConfigDirName, definitionsDir), core.SourceScopeProject)

	if home, err := core.LibrecodeHome(); err == nil {
		catalog.loadDir(filepath.Join(home, definitionsDir), core.SourceScopeUser)
	}

	catalog.loadFS(builtins, "builtin", core.SourceScopeTemporary)

	return catalog
}

// Get resolves a definition by normalized name.
func (catalog *Catalog) Get(name string) (Definition, bool) {
	if catalog == nil {
		return emptyDefinition(), false
	}

	definition, ok := catalog.definitions[normalizeName(name)]
	if !ok {
		return emptyDefinition(), false
	}

	definition.Tools = append([]tool.Name(nil), definition.Tools...)

	return definition, true
}

// Len returns the number of discovered definitions.
func (catalog *Catalog) Len() int {
	if catalog == nil {
		return 0
	}

	return len(catalog.definitions)
}

// Definitions returns definitions sorted by name.
func (catalog *Catalog) Definitions() []Definition {
	if catalog == nil {
		return nil
	}

	definitions := make([]Definition, 0, len(catalog.definitions))
	for name := range catalog.definitions {
		definition := catalog.definitions[name]
		definition.Tools = append([]tool.Name(nil), definition.Tools...)
		definitions = append(definitions, definition)
	}

	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })

	return definitions
}

// Diagnostics returns discovery diagnostics.
func (catalog *Catalog) Diagnostics() []Diagnostic {
	if catalog == nil {
		return nil
	}

	return append([]Diagnostic(nil), catalog.diagnostics...)
}

func (catalog *Catalog) loadDir(dir string, scope core.SourceScope) {
	catalog.loadFS(os.DirFS(dir), ".", scope)
}

func (catalog *Catalog) loadFS(files fs.FS, root string, scope core.SourceScope) {
	entries, err := fs.ReadDir(files, root)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{Path: root, Message: err.Error()})
		}

		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}

		definitionPath := pathpkg.Join(root, entry.Name())

		content, readErr := fs.ReadFile(files, definitionPath)
		if readErr != nil {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
				Path: definitionPath, Message: readErr.Error(),
			})

			continue
		}

		definition, parseErr := parseDefinition(string(content), definitionPath, scope)
		if parseErr != nil {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
				Path: definitionPath, Message: parseErr.Error(),
			})

			continue
		}

		key := normalizeName(definition.Name)
		if existing, exists := catalog.definitions[key]; exists {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
				Path: definitionPath,
				Message: fmt.Sprintf(
					"agent %q is shadowed by %s",
					definition.Name,
					existing.SourceInfo.Path,
				),
			})

			continue
		}

		catalog.definitions[key] = definition
	}
}

func parseDefinition(content, path string, scope core.SourceScope) (Definition, error) {
	var metadata frontmatterDefinition

	format := frontmatter.NewFormat("---", "---", yaml.Unmarshal)

	body, err := frontmatter.Parse(strings.NewReader(content), &metadata, format)
	if err != nil {
		return Definition{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	metadata.Name = normalizeName(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)

	systemPrompt := strings.TrimSpace(string(body))
	if metadata.Name == "" || metadata.Description == "" || systemPrompt == "" {
		return Definition{}, errors.New("name, description, and prompt are required")
	}

	tools := normalizeTools(metadata.Tools)
	if validateErr := validateTools(tools); validateErr != nil {
		return Definition{}, validateErr
	}

	modelPolicy, err := parseModelPolicy(&metadata)
	if err != nil {
		return Definition{}, err
	}

	limits, err := parseLimits(&metadata)
	if err != nil {
		return Definition{}, err
	}

	permissions, err := parsePermissionMode(metadata.Permissions)
	if err != nil {
		return Definition{}, err
	}

	return Definition{
		SourceInfo: core.NewSourceInfo(path, core.SourceInfoOptions{
			Scope: scope, Origin: core.SourceOriginTopLevel, BaseDir: filepath.Dir(path), Source: "local",
		}),
		Name: metadata.Name, Description: metadata.Description, SystemPrompt: systemPrompt, Tools: tools,
		Model: modelPolicy, Limits: limits, Permissions: permissions,
	}, nil
}

func normalizeTools(rawTools []string) []tool.Name {
	tools := make([]tool.Name, 0, len(rawTools))
	seen := make(map[tool.Name]struct{}, len(rawTools))

	for _, rawName := range rawTools {
		name := tool.Name(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		tools = append(tools, name)
	}

	return tools
}

func validateTools(tools []tool.Name) error {
	if _, err := tool.NewRegistryWithTools("", tools); err != nil {
		return fmt.Errorf("validate agent tools: %w", err)
	}

	return nil
}

func parseModelPolicy(metadata *frontmatterDefinition) (ModelPolicy, error) {
	policy := ModelPolicy{
		Provider: strings.TrimSpace(metadata.Provider), Model: strings.TrimSpace(metadata.Model),
		Thinking: model.ThinkingLevel(strings.ToLower(strings.TrimSpace(metadata.Thinking))),
	}
	if (policy.Provider == "") != (policy.Model == "") {
		return ModelPolicy{}, errors.New("provider and model must be set together")
	}

	if policy.Thinking != "" && !validThinkingLevel(policy.Thinking) {
		return ModelPolicy{}, fmt.Errorf("invalid thinking level %q", policy.Thinking)
	}

	return policy, nil
}

func parseLimits(metadata *frontmatterDefinition) (Limits, error) {
	var timeout time.Duration

	var err error
	if strings.TrimSpace(metadata.Timeout) != "" {
		timeout, err = time.ParseDuration(metadata.Timeout)
		if err != nil || timeout <= 0 {
			return Limits{}, fmt.Errorf("timeout must be a positive duration: %q", metadata.Timeout)
		}
	}

	return Limits{Timeout: timeout}, nil
}

func parsePermissionMode(raw string) (PermissionMode, error) {
	mode := PermissionMode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		return PermissionInherit, nil
	}

	switch mode {
	case PermissionInherit, PermissionDeny, PermissionAsk, PermissionAllow:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid permissions mode %q", raw)
	}
}

func validThinkingLevel(level model.ThinkingLevel) bool {
	switch level {
	case model.ThinkingOff, model.ThinkingMinimal, model.ThinkingLow, model.ThinkingMedium,
		model.ThinkingHigh, model.ThinkingXHigh, model.ThinkingMax:
		return true
	default:
		return false
	}
}

func emptyDefinition() Definition {
	return Definition{
		SourceInfo: core.SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""},
		Name:       "", Description: "", SystemPrompt: "", Tools: nil,
		Model:       ModelPolicy{Provider: "", Model: "", Thinking: ""},
		Limits:      Limits{Timeout: 0},
		Permissions: PermissionInherit,
	}
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
