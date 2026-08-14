package compaction

import (
	"errors"
	"fmt"
	"strings"
)

// Reason identifies why one logical compaction operation ran.
type Reason string

const (
	// ReasonManual identifies user-requested compaction.
	ReasonManual Reason = "manual"
	// ReasonPreRequest identifies automatic compaction before a request.
	ReasonPreRequest Reason = "pre_request"
	// ReasonPostResponse identifies automatic compaction after a response.
	ReasonPostResponse Reason = "post_response"
	// ReasonProviderOverflow identifies recovery from provider context overflow.
	ReasonProviderOverflow Reason = "provider_overflow"
)

// Validate rejects unknown reasons.
func (reason Reason) Validate() error {
	switch reason {
	case ReasonManual, ReasonPreRequest, ReasonPostResponse, ReasonProviderOverflow:
		return nil
	default:
		return fmt.Errorf("invalid compaction reason %q", strings.TrimSpace(string(reason)))
	}
}

// RetryIntent describes whether a successful compaction is followed by a replacement request.
type RetryIntent string

const (
	// RetryNone indicates that compaction is not followed by a replacement request.
	RetryNone RetryIntent = "none"
	// RetryAfterCompaction requests a replacement model request after compaction.
	RetryAfterCompaction RetryIntent = "retry_after_compaction"
)

// Validate rejects unknown retry intent values.
func (intent RetryIntent) Validate() error {
	switch intent {
	case RetryNone, RetryAfterCompaction:
		return nil
	default:
		return fmt.Errorf("invalid compaction retry intent %q", strings.TrimSpace(string(intent)))
	}
}

// Operation is immutable identity and policy for one logical compaction.
type Operation struct {
	ID          string
	Reason      Reason
	RetryIntent RetryIntent
}

// Validate verifies all operation contracts.
func (operation Operation) Validate() error {
	if strings.TrimSpace(operation.ID) == "" {
		return errors.New("compaction operation id is empty")
	}

	if err := operation.Reason.Validate(); err != nil {
		return err
	}

	return operation.RetryIntent.Validate()
}
