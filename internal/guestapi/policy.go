// Package guestapi defines the versioned public contract exposed to restricted
// MVM programs. It contains policy only; worker bindings remain responsible for
// enforcing the contract for a particular execution profile.
package guestapi

import "fmt"

// Version identifies a guest API independently of any persisted source format.
type Version string

const (
	// Version2 is the canonical unified package contract.
	Version2 Version = "2"
	// CurrentVersion is the sole supported guest API version.
	CurrentVersion = Version2
)

// Profile identifies an execution guarantee profile for capability policy.
type Profile string

const (
	// ProfileTurn identifies synchronous provider-turn execution.
	ProfileTurn Profile = "turn"
	// ProfileDurable identifies detached persisted workflow execution.
	ProfileDurable Profile = "durable"
)

// Canonical guest import paths. Artifact and state packages reserve names; they
// do not imply that those capabilities are implemented yet.
const (
	PackageTools     = "librecode/tools"
	PackageAgents    = "librecode/agents"
	PackageWorkflow  = "librecode/workflow"
	PackageArtifacts = "librecode/artifacts"
	PackageState     = "librecode/state"
)

// CapabilityErrorCode is a stable machine-readable guest capability failure.
type CapabilityErrorCode string

const (
	// ErrorUnavailable means a known capability is not exposed in the selected profile.
	ErrorUnavailable CapabilityErrorCode = "guest_capability_unavailable"
	// ErrorDenied means policy refused an otherwise supported capability.
	ErrorDenied CapabilityErrorCode = "guest_capability_denied"
	// ErrorUnsupported means the selected guest API/runtime does not implement the capability.
	ErrorUnsupported CapabilityErrorCode = "guest_capability_unsupported"
	// ErrorRecursive means a capability attempted to invoke the outer execute surface.
	ErrorRecursive CapabilityErrorCode = "guest_capability_recursive"
)

// Availability describes whether a canonical function is part of a profile's
// contract. Implemented is false for functions reserved for a later
// epic phase; callers must receive ErrorUnsupported until that phase ships.
type Availability struct {
	Package     string
	Function    string
	Turn        bool
	Durable     bool
	Implemented bool
}

// AvailabilityManifest returns the canonical function policy.
func AvailabilityManifest() []Availability {
	return []Availability{
		{Package: PackageTools, Function: "Search", Turn: true, Durable: false, Implemented: true},
		{Package: PackageTools, Function: "Describe", Turn: true, Durable: false, Implemented: true},
		{Package: PackageTools, Function: "Call", Turn: true, Durable: false, Implemented: true},
		{Package: PackageAgents, Function: "Run", Turn: false, Durable: true, Implemented: true},
		{Package: PackageAgents, Function: "Spawn", Turn: false, Durable: true, Implemented: true},
		{Package: PackageAgents, Function: "Wait", Turn: false, Durable: true, Implemented: true},
		{Package: PackageAgents, Function: "List", Turn: false, Durable: true, Implemented: true},
		{Package: PackageAgents, Function: "Cancel", Turn: false, Durable: true, Implemented: true},
		{Package: PackageWorkflow, Function: "Parallel", Turn: true, Durable: true, Implemented: true},
		{Package: PackageWorkflow, Function: "Pipeline", Turn: true, Durable: true, Implemented: true},
		{Package: PackageWorkflow, Function: "Phase", Turn: true, Durable: true, Implemented: true},
		{Package: PackageWorkflow, Function: "Item", Turn: true, Durable: true, Implemented: true},
		{Package: PackageWorkflow, Function: "Event", Turn: true, Durable: true, Implemented: true},
		{Package: PackageWorkflow, Function: "Log", Turn: true, Durable: true, Implemented: true},
		{Package: PackageArtifacts, Function: "Put", Turn: true, Durable: true, Implemented: false},
		{Package: PackageArtifacts, Function: "Get", Turn: true, Durable: true, Implemented: false},
		{Package: PackageState, Function: "Get", Turn: false, Durable: true, Implemented: false},
		{Package: PackageState, Function: "Put", Turn: false, Durable: true, Implemented: false},
	}
}

// ValidateWorkerContract rejects worker contracts that this binary cannot execute.
func ValidateWorkerContract(profile Profile, version Version) error {
	switch profile {
	case ProfileTurn, ProfileDurable:
	default:
		return fmt.Errorf("unknown execute worker profile %q", profile)
	}

	if version != CurrentVersion {
		return fmt.Errorf("incompatible guest API version %q", version)
	}

	return nil
}

// Available reports whether a function belongs to the selected profile. It
// does not report implementation readiness, which is represented separately.
func (availability Availability) Available(profile Profile) bool {
	switch profile {
	case ProfileTurn:
		return availability.Turn
	case ProfileDurable:
		return availability.Durable
	default:
		return false
	}
}
