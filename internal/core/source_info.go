// Package core contains Pi-compatible assistant core data structures.
package core

// SourceScope identifies whether a resource came from user, project, or temporary config.
type SourceScope string

const (
	// SourceScopeUser identifies user-scoped resources.
	SourceScopeUser SourceScope = "user"
	// SourceScopeProject identifies project-scoped resources.
	SourceScopeProject SourceScope = "project"
	// SourceScopeTemporary identifies temporary CLI/session resources.
	SourceScopeTemporary SourceScope = "temporary"
)

// SourceOrigin identifies whether a resource came from a package or top-level path.
type SourceOrigin string

const (
	// SourceOriginPackage identifies package-provided resources.
	SourceOriginPackage SourceOrigin = "package"
	// SourceOriginTopLevel identifies top-level resources.
	SourceOriginTopLevel SourceOrigin = "top-level"
)

// SourceInfo describes where a loaded resource came from.
type SourceInfo struct {
	Path    string       `json:"path"`
	Source  string       `json:"source"`
	Scope   SourceScope  `json:"scope"`
	Origin  SourceOrigin `json:"origin"`
	BaseDir string       `json:"base_dir,omitempty"`
}

// SourceInfoOptions contains optional source metadata.
type SourceInfoOptions struct {
	Scope   SourceScope  `json:"scope"`
	Origin  SourceOrigin `json:"origin"`
	BaseDir string       `json:"base_dir,omitempty"`
	Source  string       `json:"source"`
}

// NewSourceInfo creates SourceInfo with Pi-compatible defaults.
func NewSourceInfo(path string, options SourceInfoOptions) SourceInfo {
	scope := options.Scope
	if scope == "" {
		scope = SourceScopeTemporary
	}
	origin := options.Origin
	if origin == "" {
		origin = SourceOriginTopLevel
	}

	return SourceInfo{Path: path, Source: options.Source, Scope: scope, Origin: origin, BaseDir: options.BaseDir}
}
