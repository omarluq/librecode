// Package agent discovers and validates subagent definitions.
package agent

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/tool"
)

const definitionsDir = "agents"

//go:embed builtin/*.md
var builtins embed.FS

// Definition describes one invocable subagent.
type Definition struct {
	SourceInfo   core.SourceInfo `json:"source_info"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SystemPrompt string          `json:"system_prompt"`
	Tools        []tool.Name     `json:"tools"`
}

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

		path := filepath.Join(root, entry.Name())

		content, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{Path: path, Message: readErr.Error()})

			continue
		}

		definition, parseErr := parseDefinition(string(content), path, scope)
		if parseErr != nil {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{Path: path, Message: parseErr.Error()})

			continue
		}

		key := normalizeName(definition.Name)
		if existing, exists := catalog.definitions[key]; exists {
			catalog.diagnostics = append(catalog.diagnostics, Diagnostic{
				Path: path,
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

	tools := make([]tool.Name, 0, len(metadata.Tools))
	seen := map[tool.Name]bool{}

	for _, rawName := range metadata.Tools {
		name := tool.Name(strings.TrimSpace(rawName))
		if name == "" || seen[name] {
			continue
		}

		seen[name] = true
		tools = append(tools, name)
	}

	if err := validateTools(tools); err != nil {
		return Definition{}, err
	}

	return Definition{
		SourceInfo: core.NewSourceInfo(path, core.SourceInfoOptions{
			Scope: scope, Origin: core.SourceOriginTopLevel, BaseDir: filepath.Dir(path), Source: "local",
		}),
		Name: metadata.Name, Description: metadata.Description, SystemPrompt: systemPrompt, Tools: tools,
	}, nil
}

func validateTools(tools []tool.Name) error {
	registry, err := tool.NewRegistryWithTools("", tools)
	if err != nil {
		return fmt.Errorf("validate agent tools: %w", err)
	}

	for _, definition := range registry.Definitions() {
		if !definition.ReadOnly {
			return fmt.Errorf("tool %q is not read-only", definition.Name)
		}
	}

	return nil
}

func emptyDefinition() Definition {
	return Definition{
		SourceInfo: core.SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""},
		Name:       "", Description: "", SystemPrompt: "", Tools: nil,
	}
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
