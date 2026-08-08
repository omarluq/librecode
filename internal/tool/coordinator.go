package tool

// Coordinator owns process-wide queues for conflicting workspace mutations.
// It coordinates one librecode process; SQLite task leases remain independent.
type Coordinator struct {
	files *fileMutationLocks
}

// NewCoordinator creates a process-local mutation coordinator.
func NewCoordinator() *Coordinator { return &Coordinator{files: newFileMutationLocks()} }
