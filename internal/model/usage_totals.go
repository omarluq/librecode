package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// UsageTotals is cumulative provider-reported usage for one run. Reported
// distinguishes an unknown snapshot from a valid report containing zero tokens.
type UsageTotals struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	ProviderRoundTrips int64 `json:"provider_round_trips"`
	Reported           bool  `json:"-"`
}

// UsageTotalsFromTokenUsage converts one provider usage observation into
// cumulative usage. Reported distinguishes an explicit zero provider report
// from absent usage; positive legacy values remain compatible.
func UsageTotalsFromTokenUsage(usage TokenUsage) (UsageTotals, error) {
	cumulative := UsageTotals{
		InputTokens:        int64(usage.InputTokens),
		OutputTokens:       int64(usage.OutputTokens),
		ProviderRoundTrips: 0,
		Reported:           usage.Reported() || usage.InputTokens > 0 || usage.OutputTokens > 0,
	}
	if err := cumulative.Validate(); err != nil {
		return UsageTotals{}, err
	}

	return cumulative, nil
}

// TotalTokens returns the checked sum of provider input and output tokens.
func (usage UsageTotals) TotalTokens() (int64, error) {
	if err := usage.Validate(); err != nil {
		return 0, err
	}

	if usage.InputTokens > math.MaxInt64-usage.OutputTokens {
		return 0, errors.New("provider token total overflows int64")
	}

	return usage.InputTokens + usage.OutputTokens, nil
}

// Validate rejects values which cannot be valid provider usage.
func (usage UsageTotals) Validate() error {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.ProviderRoundTrips < 0 {
		return errors.New("provider usage cannot be negative")
	}

	return nil
}

// Add returns the checked cumulative usage of two snapshots.
func (usage UsageTotals) Add(other UsageTotals) (UsageTotals, error) {
	if err := usage.Validate(); err != nil {
		return UsageTotals{}, err
	}

	if err := other.Validate(); err != nil {
		return UsageTotals{}, err
	}

	if usage.InputTokens > math.MaxInt64-other.InputTokens ||
		usage.OutputTokens > math.MaxInt64-other.OutputTokens ||
		usage.ProviderRoundTrips > math.MaxInt64-other.ProviderRoundTrips {
		return UsageTotals{}, errors.New("provider usage overflows int64")
	}

	return UsageTotals{
		InputTokens:        usage.InputTokens + other.InputTokens,
		OutputTokens:       usage.OutputTokens + other.OutputTokens,
		ProviderRoundTrips: usage.ProviderRoundTrips + other.ProviderRoundTrips,
		Reported:           usage.Reported || other.Reported,
	}, nil
}

// UsageAggregate describes checked aggregation across a set of runs.
type UsageAggregate struct {
	Usage UsageTotals
	Known int
	Total int
}

// AggregateUsage sums each supplied run once and tracks partial knowledge.
func AggregateUsage(usages []UsageTotals) (UsageAggregate, error) {
	aggregate := UsageAggregate{
		Usage: UsageTotals{
			InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false,
		},
		Known: 0,
		Total: len(usages),
	}
	for _, usage := range usages {
		if err := usage.Validate(); err != nil {
			return UsageAggregate{}, err
		}

		if !usage.Reported {
			continue
		}

		var err error

		aggregate.Usage, err = aggregate.Usage.Add(usage)
		if err != nil {
			return UsageAggregate{}, err
		}

		aggregate.Known++
	}

	aggregate.Usage.Reported = aggregate.Known > 0

	return aggregate, nil
}

// MarshalJSON preserves the persisted usage field names while retaining the
// distinction between unknown usage and a reported zero.
func (usage UsageTotals) MarshalJSON() ([]byte, error) {
	if err := usage.Validate(); err != nil {
		return nil, err
	}

	if usage == (UsageTotals{InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false}) {
		return []byte("{}"), nil
	}

	type wireUsage struct {
		InputTokens        int64 `json:"input_tokens,omitempty"`
		OutputTokens       int64 `json:"output_tokens,omitempty"`
		ProviderRoundTrips int64 `json:"provider_round_trips,omitempty"`
		Reported           bool  `json:"reported"`
	}

	encoded, err := json.Marshal(wireUsage(usage))
	if err != nil {
		return nil, fmt.Errorf("marshal provider usage: %w", err)
	}

	return encoded, nil
}

// UnmarshalJSON accepts legacy snapshots and ignores unknown fields. Positive
// legacy fields imply reported usage; an explicit reported field is authoritative.
func (usage *UsageTotals) UnmarshalJSON(data []byte) error {
	type wireUsage struct {
		Reported           *bool `json:"reported"`
		InputTokens        int64 `json:"input_tokens"`
		OutputTokens       int64 `json:"output_tokens"`
		ProviderRoundTrips int64 `json:"provider_round_trips"`
	}

	var wire wireUsage
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("unmarshal provider usage: %w", err)
	}

	decoded := UsageTotals{
		InputTokens:        wire.InputTokens,
		OutputTokens:       wire.OutputTokens,
		ProviderRoundTrips: wire.ProviderRoundTrips,
		Reported:           false,
	}
	if wire.Reported != nil {
		decoded.Reported = *wire.Reported
	} else {
		decoded.Reported = decoded.InputTokens > 0 || decoded.OutputTokens > 0 || decoded.ProviderRoundTrips > 0
	}

	if err := decoded.Validate(); err != nil {
		return err
	}

	*usage = decoded

	return nil
}
