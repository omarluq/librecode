package core

// ResourceDiagnostic reports a loaded resource warning, error, or name collision.
type ResourceDiagnostic struct {
	Collision *ResourceCollision `json:"collision,omitempty"`
	Type      string             `json:"type"`
	Message   string             `json:"message"`
	Path      string             `json:"path,omitempty"`
}

// ResourceCollision describes a resource name collision.
type ResourceCollision struct {
	ResourceType string `json:"resourceType"`
	Name         string `json:"name"`
	WinnerPath   string `json:"winnerPath"`
	LoserPath    string `json:"loserPath"`
}

func warningDiagnostic(message, path string) ResourceDiagnostic {
	return ResourceDiagnostic{Collision: nil, Type: diagnosticWarning, Message: message, Path: path}
}

func errorDiagnostic(message, path string) ResourceDiagnostic {
	return ResourceDiagnostic{Collision: nil, Type: diagnosticError, Message: message, Path: path}
}

func collisionResourceDiagnostic(resourceType, name, winnerPath, loserPath string) ResourceDiagnostic {
	return ResourceDiagnostic{
		Collision: &ResourceCollision{
			ResourceType: resourceType,
			Name:         name,
			WinnerPath:   winnerPath,
			LoserPath:    loserPath,
		},
		Type:    diagnosticCollision,
		Message: "name \"" + name + "\" collision",
		Path:    loserPath,
	}
}
