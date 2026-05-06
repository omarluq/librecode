package core

// SlashCommandSource identifies where a slash command came from.
type SlashCommandSource string

const (
	// SlashCommandSourceExtension is provided by a workflow extension.
	SlashCommandSourceExtension SlashCommandSource = "extension"
	// SlashCommandSourcePrompt is provided by a prompt template.
	SlashCommandSourcePrompt SlashCommandSource = "prompt"
	// SlashCommandSourceSkill is provided by a skill.
	SlashCommandSourceSkill SlashCommandSource = "skill"
)

// SlashCommandInfo describes a user-visible slash command.
type SlashCommandInfo struct {
	SourceInfo  SourceInfo         `json:"sourceInfo"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Source      SlashCommandSource `json:"source"`
}

// BuiltinSlashCommand describes a built-in librecode slash command.
type BuiltinSlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// BuiltinSlashCommands mirrors librecode's built-in command catalog.
var BuiltinSlashCommands = []BuiltinSlashCommand{
	{Name: "settings", Description: "Open settings menu"},
	{Name: "model", Description: "Select model (opens selector UI)"},
	{Name: "scoped-models", Description: "Enable/disable models for Ctrl+P cycling"},
	{Name: "export", Description: "Export session (HTML default, or specify path: .html/.jsonl)"},
	{Name: "import", Description: "Import and resume a session from a JSONL file"},
	{Name: "share", Description: "Share session as a secret GitHub gist"},
	{Name: "copy", Description: "Copy last agent message to clipboard"},
	{Name: "name", Description: "Set session display name"},
	{Name: "session", Description: "Show session info and stats"},
	{Name: "changelog", Description: "Show changelog entries"},
	{Name: "hotkeys", Description: "Show all keyboard shortcuts"},
	{Name: "fork", Description: "Create a new fork from a previous user message"},
	{Name: "clone", Description: "Duplicate the current session at the current position"},
	{Name: "tree", Description: "Navigate session tree (switch branches)"},
	{Name: "login", Description: "Configure provider authentication"},
	{Name: "logout", Description: "Remove provider authentication"},
	{Name: "new", Description: "Start a new session"},
	{Name: "compact", Description: "Manually compact the session context"},
	{Name: "resume", Description: "Resume a different session"},
	{Name: "reload", Description: "Reload keybindings, extensions, skills, prompts, and themes"},
	{Name: "quit", Description: "Quit librecode"},
}
