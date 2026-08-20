package assistant

import (
	"errors"
	"sync"
)

const defaultSteeringInboxCapacity = 32

var (
	// ErrSteeringInactive indicates that the session has no active steering inbox.
	ErrSteeringInactive = errors.New("steering run is inactive")
	// ErrSteeringStaleRun indicates that the request targets an older active run.
	ErrSteeringStaleRun = errors.New("steering run identity is stale")
	// ErrSteeringClosed indicates that the targeted inbox closed before accepting the request.
	ErrSteeringClosed = errors.New("steering inbox is closed")
	// ErrSteeringCapacity indicates that the active inbox cannot accept more messages.
	ErrSteeringCapacity = errors.New("steering inbox capacity exceeded")
	// ErrSteeringInvalidInput indicates that a steering request failed validation.
	ErrSteeringInvalidInput = errors.New("invalid steering input")
)

type steeringDraft struct {
	Text           string
	Images         []ImageAttachment
	HideUserPrompt bool
}

type steeringInboxRegistry struct {
	inboxes  map[string]*steeringInbox
	capacity int
	mu       sync.Mutex
}

type steeringInbox struct {
	runID    string
	drafts   []steeringDraft
	capacity int
	mu       sync.Mutex
	closed   bool
}

func newSteeringInboxRegistry(capacity int) *steeringInboxRegistry {
	if capacity <= 0 {
		capacity = defaultSteeringInboxCapacity
	}

	return &steeringInboxRegistry{
		inboxes:  make(map[string]*steeringInbox),
		capacity: capacity,
		mu:       sync.Mutex{},
	}
}

func (registry *steeringInboxRegistry) register(sessionID, runID string) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if _, exists := registry.inboxes[sessionID]; exists {
		return ErrSteeringStaleRun
	}

	registry.inboxes[sessionID] = &steeringInbox{
		drafts:   make([]steeringDraft, 0, registry.capacity),
		runID:    runID,
		capacity: registry.capacity,
		closed:   false,
		mu:       sync.Mutex{},
	}

	return nil
}

func (registry *steeringInboxRegistry) accept(sessionID, runID string, draft steeringDraft) error {
	registry.mu.Lock()
	inbox := registry.inboxes[sessionID]
	registry.mu.Unlock()

	if inbox == nil {
		return ErrSteeringInactive
	}

	return inbox.accept(runID, draft)
}

// settleIfEmpty atomically closes an inbox only when no steering has been
// accepted. Cache hits use this to prevent acceptance after deciding to return
// an unsteered response.
func (registry *steeringInboxRegistry) settleIfEmpty(sessionID, runID string) (bool, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	inbox := registry.inboxes[sessionID]
	if inbox == nil {
		return false, ErrSteeringInactive
	}

	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if inbox.runID != runID {
		return false, ErrSteeringStaleRun
	}

	if inbox.closed {
		return false, ErrSteeringClosed
	}

	if len(inbox.drafts) != 0 {
		return false, nil
	}

	inbox.closed = true
	inbox.drafts = nil

	delete(registry.inboxes, sessionID)

	return true, nil
}

func (registry *steeringInboxRegistry) drain(sessionID, runID string) ([]steeringDraft, error) {
	registry.mu.Lock()
	inbox := registry.inboxes[sessionID]
	registry.mu.Unlock()

	if inbox == nil {
		return nil, ErrSteeringInactive
	}

	return inbox.drain(runID)
}

// drainFinal atomically settles an empty inbox or drains an accepted prefix.
// Settling prevents a steering request from being accepted after the final
// provider checkpoint has decided to return the active run.
func (registry *steeringInboxRegistry) drainFinal(sessionID, runID string) ([]steeringDraft, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	inbox := registry.inboxes[sessionID]
	if inbox == nil {
		return nil, ErrSteeringInactive
	}

	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if inbox.runID != runID {
		return nil, ErrSteeringStaleRun
	}

	if inbox.closed {
		return nil, ErrSteeringClosed
	}

	if len(inbox.drafts) == 0 {
		inbox.closed = true
		inbox.drafts = nil

		delete(registry.inboxes, sessionID)

		return nil, nil
	}

	drafts := cloneSteeringDrafts(inbox.drafts)
	inbox.drafts = inbox.drafts[:0]

	return drafts, nil
}

func (registry *steeringInboxRegistry) restore(sessionID, runID string, drafts []steeringDraft) error {
	registry.mu.Lock()
	inbox := registry.inboxes[sessionID]
	registry.mu.Unlock()

	if inbox == nil {
		return ErrSteeringInactive
	}

	return inbox.restore(runID, drafts)
}

func (registry *steeringInboxRegistry) close(sessionID, runID string) ([]steeringDraft, error) {
	registry.mu.Lock()

	inbox := registry.inboxes[sessionID]
	if inbox == nil {
		registry.mu.Unlock()

		return nil, ErrSteeringInactive
	}

	if inbox.runID != runID {
		registry.mu.Unlock()

		return nil, ErrSteeringStaleRun
	}

	delete(registry.inboxes, sessionID)
	registry.mu.Unlock()

	return inbox.close(runID)
}

func (inbox *steeringInbox) accept(runID string, draft steeringDraft) error {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if inbox.runID != runID {
		return ErrSteeringStaleRun
	}

	if inbox.closed {
		return ErrSteeringClosed
	}

	if len(inbox.drafts) >= inbox.capacity {
		return ErrSteeringCapacity
	}

	inbox.drafts = append(inbox.drafts, cloneSteeringDraft(draft))

	return nil
}

func (inbox *steeringInbox) drain(runID string) ([]steeringDraft, error) {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if inbox.runID != runID {
		return nil, ErrSteeringStaleRun
	}

	if inbox.closed {
		return nil, ErrSteeringClosed
	}

	drafts := cloneSteeringDrafts(inbox.drafts)
	inbox.drafts = inbox.drafts[:0]

	return drafts, nil
}

func (inbox *steeringInbox) restore(runID string, drafts []steeringDraft) error {
	if len(drafts) == 0 {
		return nil
	}

	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if inbox.runID != runID {
		return ErrSteeringStaleRun
	}

	if inbox.closed {
		return ErrSteeringClosed
	}

	if len(inbox.drafts)+len(drafts) > inbox.capacity {
		return ErrSteeringCapacity
	}

	restored := make([]steeringDraft, 0, len(drafts)+len(inbox.drafts))
	restored = append(restored, cloneSteeringDrafts(drafts)...)
	restored = append(restored, inbox.drafts...)
	inbox.drafts = restored

	return nil
}

func (inbox *steeringInbox) close(runID string) ([]steeringDraft, error) {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()

	if inbox.runID != runID {
		return nil, ErrSteeringStaleRun
	}

	if inbox.closed {
		return nil, ErrSteeringClosed
	}

	inbox.closed = true
	drafts := cloneSteeringDrafts(inbox.drafts)
	inbox.drafts = nil

	return drafts, nil
}

func cloneSteeringDrafts(drafts []steeringDraft) []steeringDraft {
	if len(drafts) == 0 {
		return nil
	}

	cloned := make([]steeringDraft, len(drafts))
	for index := range drafts {
		cloned[index] = cloneSteeringDraft(drafts[index])
	}

	return cloned
}

func cloneSteeringDraft(draft steeringDraft) steeringDraft {
	images := make([]ImageAttachment, len(draft.Images))
	for index := range draft.Images {
		images[index] = draft.Images[index]
		images[index].Data = append([]byte(nil), draft.Images[index].Data...)
	}

	return steeringDraft{Text: draft.Text, Images: images, HideUserPrompt: draft.HideUserPrompt}
}
