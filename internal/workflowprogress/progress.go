// Package workflowprogress defines and enforces the bounded progress contract
// shared by MVM execution profiles. Progress is presentation metadata only; it
// does not create work, dependencies, or scheduling barriers.
package workflowprogress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	// ContractVersion identifies the serialized progress envelope contract.
	ContractVersion = 1
	// MaxEvents bounds accepted progress updates in one evaluation.
	MaxEvents = 256
	// MaxIDBytes bounds phase/item identifiers.
	MaxIDBytes = 128
	// MaxTextBytes bounds titles, event names, log messages, and levels.
	MaxTextBytes = 4096
	// MaxDataBytes bounds the encoded custom event data object.
	MaxDataBytes = 16 << 10
)

// Kind identifies the typed body carried by an Event.
type Kind string

// Progress event kinds.
const (
	KindPhase Kind = "phase"
	KindItem  Kind = "item"
	KindEvent Kind = "event"
	KindLog   Kind = "log"
)

// State is phase/item presentation state.
type State string

// Phase and item states.
const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

// Phase is a phase snapshot. A phase groups progress and has no scheduling semantics.
type Phase struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State State  `json:"state"`
}

// Item is an item snapshot. PhaseID may be empty when no phase was declared.
type Item struct {
	ID      string `json:"id"`
	PhaseID string `json:"phase_id,omitempty"`
	Title   string `json:"title"`
	State   State  `json:"state"`
}

// Custom is a named, structured progress observation.
type Custom struct {
	Data map[string]any `json:"data"`
	Name string         `json:"name"`
}

// Log is a bounded diagnostic progress message.
type Log struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Event is the versioned, ordered progress envelope. Exactly one typed body is set.
type Event struct {
	Phase    *Phase  `json:"phase,omitempty"`
	Item     *Item   `json:"item,omitempty"`
	Custom   *Custom `json:"event,omitempty"`
	Log      *Log    `json:"log,omitempty"`
	Kind     Kind    `json:"kind"`
	Version  int     `json:"version"`
	Sequence uint64  `json:"sequence"`
}

// Sink receives accepted events synchronously in sequence order.
type Sink func(context.Context, Event) error

// Emitter owns validation, transition state, and sequence allocation for one evaluation.
type Emitter struct {
	sink   Sink
	phases map[string]State
	items  map[string]State
	mu     sync.Mutex
	count  uint64
}

// New returns an evaluation-scoped progress emitter. A nil sink validates and tracks
// state while discarding delivery.
func New(sink Sink) *Emitter {
	return &Emitter{
		sink: sink, phases: make(map[string]State), items: make(map[string]State),
		mu: sync.Mutex{}, count: 0,
	}
}

// Phase validates and emits a phase snapshot.
func (emitter *Emitter) Phase(ctx context.Context, identifier, title, state string) error {
	if err := validID("phase", identifier); err != nil {
		return err
	}

	if err := validText("phase title", title); err != nil {
		return err
	}

	parsed, err := parseState(state)
	if err != nil {
		return err
	}

	event := Event{
		Phase: &Phase{ID: identifier, Title: title, State: parsed}, Item: nil, Custom: nil, Log: nil,
		Kind: KindPhase, Version: 0, Sequence: 0,
	}

	return emitter.emit(ctx, event, func() error {
		return validateTransition("phase", identifier, emitter.phases, parsed)
	}, func() { emitter.phases[identifier] = parsed })
}

// Item validates and emits an item snapshot.
func (emitter *Emitter) Item(ctx context.Context, identifier, phaseID, title, state string) error {
	if err := validID("item", identifier); err != nil {
		return err
	}

	if phaseID != "" {
		if err := validID("item phase", phaseID); err != nil {
			return err
		}
	}

	if err := validText("item title", title); err != nil {
		return err
	}

	parsed, err := parseState(state)
	if err != nil {
		return err
	}

	event := Event{
		Phase: nil, Item: &Item{ID: identifier, PhaseID: phaseID, Title: title, State: parsed},
		Custom: nil, Log: nil, Kind: KindItem, Version: 0, Sequence: 0,
	}

	return emitter.emit(ctx, event, func() error {
		if phaseID != "" {
			if _, found := emitter.phases[phaseID]; !found {
				return fmt.Errorf("progress item phase %q is not declared", phaseID)
			}
		}

		return validateTransition("item", identifier, emitter.items, parsed)
	}, func() { emitter.items[identifier] = parsed })
}

// Event validates and emits a custom structured observation.
func (emitter *Emitter) Event(ctx context.Context, name string, data map[string]any) error {
	if err := validText("event name", name); err != nil {
		return err
	}

	if data == nil {
		data = map[string]any{}
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode progress event data: %w", err)
	}

	if len(encoded) > MaxDataBytes {
		return fmt.Errorf("progress event data is %d bytes; limit is %d", len(encoded), MaxDataBytes)
	}

	return emitter.emit(ctx, Event{
		Phase: nil, Item: nil, Custom: &Custom{Name: name, Data: data}, Log: nil,
		Kind: KindEvent, Version: 0, Sequence: 0,
	}, nil, nil)
}

// Log validates and emits a diagnostic message.
func (emitter *Emitter) Log(ctx context.Context, level, message string) error {
	if err := validText("log level", level); err != nil {
		return err
	}

	if err := validText("log message", message); err != nil {
		return err
	}

	return emitter.emit(ctx, Event{
		Phase: nil, Item: nil, Custom: nil, Log: &Log{Level: level, Message: message},
		Kind: KindLog, Version: 0, Sequence: 0,
	}, nil, nil)
}

func (emitter *Emitter) emit(ctx context.Context, event Event, validate func() error, commit func()) error {
	emitter.mu.Lock()
	defer emitter.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("emit progress: %w", err)
	}

	if emitter.count >= MaxEvents {
		return fmt.Errorf("progress event limit %d exceeded", MaxEvents)
	}

	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}

	event.Version = ContractVersion

	event.Sequence = emitter.count + 1
	if emitter.sink != nil {
		if err := emitter.sink(ctx, event); err != nil {
			return fmt.Errorf("emit progress event: %w", err)
		}
	}

	if commit != nil {
		commit()
	}

	emitter.count++

	return nil
}

// ValidateEvent validates an envelope received from an isolated worker.
func ValidateEvent(event Event) error {
	if event.Version != ContractVersion {
		return fmt.Errorf("unsupported progress contract version %d", event.Version)
	}

	if event.Sequence == 0 || event.Sequence > MaxEvents {
		return fmt.Errorf("progress sequence %d is outside 1..%d", event.Sequence, MaxEvents)
	}

	bodies := boolInt(event.Phase != nil) + boolInt(event.Item != nil) +
		boolInt(event.Custom != nil) + boolInt(event.Log != nil)

	if bodies != 1 {
		return errors.New("progress event must contain exactly one typed body")
	}

	switch event.Kind {
	case KindPhase:
		return validatePhase(event.Phase)
	case KindItem:
		return validateItem(event.Item)
	case KindEvent:
		return validateCustom(event.Custom)
	case KindLog:
		return validateLog(event.Log)
	default:
		return fmt.Errorf("unknown progress event kind %q", event.Kind)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

//nolint:wsl_v5 // Keep validation clauses compact and consistently ordered.
func validatePhase(phase *Phase) error {
	if phase == nil {
		return errors.New("phase progress event is missing its phase body")
	}
	if err := validID("phase", phase.ID); err != nil {
		return err
	}
	if err := validText("phase title", phase.Title); err != nil {
		return err
	}
	_, err := parseState(string(phase.State))

	return err
}

//nolint:wsl_v5 // Keep validation clauses compact and consistently ordered.
func validateItem(item *Item) error {
	if item == nil {
		return errors.New("item progress event is missing its item body")
	}
	if err := validID("item", item.ID); err != nil {
		return err
	}
	if item.PhaseID != "" {
		if err := validID("item phase", item.PhaseID); err != nil {
			return err
		}
	}
	if err := validText("item title", item.Title); err != nil {
		return err
	}
	_, err := parseState(string(item.State))

	return err
}

//nolint:wsl_v5 // Keep validation clauses compact and consistently ordered.
func validateCustom(custom *Custom) error {
	if custom == nil {
		return errors.New("custom progress event is missing its event body")
	}
	if err := validText("event name", custom.Name); err != nil {
		return err
	}
	encoded, err := json.Marshal(custom.Data)
	if err != nil {
		return fmt.Errorf("encode progress event data: %w", err)
	}
	if len(encoded) > MaxDataBytes {
		return fmt.Errorf("progress event data is %d bytes; limit is %d", len(encoded), MaxDataBytes)
	}

	return nil
}

//nolint:wsl_v5 // Keep validation clauses compact and consistently ordered.
func validateLog(log *Log) error {
	if log == nil {
		return errors.New("log progress event is missing its log body")
	}
	if err := validText("log level", log.Level); err != nil {
		return err
	}

	return validText("log message", log.Message)
}

func validateTransition(kind, identifier string, states map[string]State, next State) error {
	previous, found := states[identifier]
	if found && terminal(previous) {
		return fmt.Errorf("progress %s %q is terminal in state %q", kind, identifier, previous)
	}

	if found && previous == next {
		return fmt.Errorf("progress %s %q already has state %q", kind, identifier, next)
	}

	return nil
}

func parseState(value string) (State, error) {
	state := State(value)
	switch state {
	case StatePending, StateRunning, StateSucceeded, StateFailed, StateCanceled:
		return state, nil
	default:
		return "", fmt.Errorf("invalid progress state %q", value)
	}
}

func terminal(state State) bool {
	return state == StateSucceeded || state == StateFailed || state == StateCanceled
}

func validID(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("progress %s ID is required", name)
	}

	if len(value) > MaxIDBytes {
		return fmt.Errorf("progress %s ID is %d bytes; limit is %d", name, len(value), MaxIDBytes)
	}

	return nil
}

func validText(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("progress %s is required", name)
	}

	if len(value) > MaxTextBytes {
		return fmt.Errorf("progress %s is %d bytes; limit is %d", name, len(value), MaxTextBytes)
	}

	return nil
}
