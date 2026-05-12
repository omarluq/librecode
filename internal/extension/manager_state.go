package extension

// ManagerState describes configured, resolved, and loaded extension state.
type ManagerState struct {
	Configured []ResolvedSource
	Loaded     []LoadedExtension
}
