// Package timestamp provides compact durable wall-clock timestamps.
//
// UnixSeconds stores a UTC instant as whole seconds since
// 1970-01-01T00:00:00Z. Its range is 0 through 4294967295, ending at
// 2106-02-07T06:28:15Z. Observed instants are floored to their containing
// second, while deadlines and duration additions are rounded upward so they
// are never encoded earlier than the source instant. Zero is the Unix epoch,
// not an absent value; use Optional when absence is meaningful.
package timestamp

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/samber/oops"
)

// UnixSeconds is a compact UTC Unix timestamp with one-second resolution.
type UnixSeconds uint32

// Max is the latest instant representable by UnixSeconds.
const (
	Max         UnixSeconds = 1<<32 - 1
	decimalBase             = 10
)

// Optional represents a UnixSeconds value that may be absent.
// Its zero value is absent; a present Unix epoch has Valid set to true.
type Optional struct {
	UnixSeconds UnixSeconds
	Valid       bool
}

// FromUnix validates seconds before converting them to UnixSeconds.
func FromUnix(seconds int64) (UnixSeconds, error) {
	if seconds < 0 || seconds > int64(Max) {
		return 0, invalidRangeError(seconds)
	}

	return UnixSeconds(seconds), nil
}

// FromTime converts an observed instant by flooring it to its containing second.
func FromTime(value time.Time) (UnixSeconds, error) {
	return FromUnix(value.Unix())
}

// DeadlineFromTime converts a deadline by rounding it up to the next second
// when it has a fractional component. It rejects source instants outside the
// representable range, even when rounding could move them into range.
func DeadlineFromTime(value time.Time) (UnixSeconds, error) {
	seconds := value.Unix()
	if seconds < 0 || seconds > int64(Max) {
		return 0, invalidRangeError(seconds)
	}

	if value.Nanosecond() == 0 {
		return UnixSeconds(seconds), nil
	}

	if seconds == int64(Max) {
		return 0, oops.In("timestamp").Code("timestamp_overflow").
			Errorf("deadline exceeds maximum Unix second %d", Max)
	}

	return FromUnix(seconds + 1)
}

// Unix returns the timestamp as signed Unix seconds for boundary operations.
func (timestamp UnixSeconds) Unix() int64 {
	return int64(timestamp)
}

// Add returns the timestamp plus duration, rounded upward to whole seconds.
// It rejects exact results outside the UnixSeconds range before narrowing.
func (timestamp UnixSeconds) Add(duration time.Duration) (UnixSeconds, error) {
	wholeSeconds := int64(duration / time.Second)
	remainder := duration % time.Second
	result := timestamp.Unix() + wholeSeconds

	if result < 0 || result > int64(Max) || result == 0 && remainder < 0 {
		return 0, oops.In("timestamp").Code("timestamp_arithmetic_out_of_range").
			Errorf("adding %s to Unix second %d is outside the supported range", duration, timestamp)
	}

	if remainder > 0 {
		if result == int64(Max) {
			return 0, oops.In("timestamp").Code("timestamp_overflow").
				Errorf("adding %s to Unix second %d exceeds the supported range", duration, timestamp)
		}

		result++
	}

	return FromUnix(result)
}

// Sub returns the signed duration from other to timestamp.
func (timestamp UnixSeconds) Sub(other UnixSeconds) time.Duration {
	seconds := timestamp.Unix() - other.Unix()

	return time.Duration(seconds) * time.Second
}

// Compare compares timestamp with other and returns -1, 0, or 1.
func (timestamp UnixSeconds) Compare(other UnixSeconds) int {
	switch {
	case timestamp < other:
		return -1
	case timestamp > other:
		return 1
	default:
		return 0
	}
}

// Before reports whether timestamp occurs before other.
func (timestamp UnixSeconds) Before(other UnixSeconds) bool {
	return timestamp < other
}

// After reports whether timestamp occurs after other.
func (timestamp UnixSeconds) After(other UnixSeconds) bool {
	return timestamp > other
}

// Equal reports whether timestamp and other represent the same instant.
func (timestamp UnixSeconds) Equal(other UnixSeconds) bool {
	return timestamp == other
}

// Format formats the timestamp in UTC for presentation using a time layout.
func (timestamp UnixSeconds) Format(layout string) string {
	return time.Unix(timestamp.Unix(), 0).UTC().Format(layout)
}

// MarshalJSON encodes timestamp as a JSON number.
func (timestamp UnixSeconds) MarshalJSON() ([]byte, error) {
	return strconv.AppendUint(nil, uint64(timestamp), decimalBase), nil
}

// UnmarshalJSON decodes a JSON integer after validating its range.
func (timestamp *UnixSeconds) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return oops.In("timestamp").Code("invalid_timestamp_json").
			Errorf("Unix seconds cannot be null")
	}

	var raw uint64
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return oops.In("timestamp").Code("invalid_timestamp_json").Wrapf(err, "decode Unix seconds")
	}

	if raw > uint64(Max) {
		return oops.In("timestamp").Code("timestamp_out_of_range").
			Errorf("Unix seconds %d are outside the supported range 0..%d", raw, Max)
	}

	*timestamp = UnixSeconds(raw)

	return nil
}

// Value returns a signed integer suitable for database/sql and SQLite.
func (timestamp UnixSeconds) Value() (driver.Value, error) {
	return timestamp.Unix(), nil
}

// Scan validates and stores a signed SQL integer. NULL and all other SQL
// storage classes are invalid for a required timestamp.
func (timestamp *UnixSeconds) Scan(value any) error {
	seconds, ok := value.(int64)
	if !ok {
		return invalidSQLTypeError(value)
	}

	decoded, err := FromUnix(seconds)
	if err != nil {
		return oops.In("timestamp").Code("invalid_timestamp_sql").Wrapf(err, "scan Unix seconds")
	}

	*timestamp = decoded

	return nil
}

// MarshalJSON encodes a present timestamp as a number and an absent one as null.
func (optional Optional) MarshalJSON() ([]byte, error) {
	if !optional.Valid {
		return []byte("null"), nil
	}

	return optional.UnixSeconds.MarshalJSON()
}

// UnmarshalJSON decodes null as absent and a valid integer as present.
func (optional *Optional) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*optional = Optional{UnixSeconds: 0, Valid: false}

		return nil
	}

	var decoded UnixSeconds
	if err := decoded.UnmarshalJSON(trimmed); err != nil {
		return oops.In("timestamp").Code("invalid_optional_timestamp_json").
			Wrapf(err, "decode optional Unix seconds")
	}

	*optional = Optional{UnixSeconds: decoded, Valid: true}

	return nil
}

// Value returns nil for an absent timestamp and a signed integer when present.
func (optional Optional) Value() (value driver.Value, err error) {
	if !optional.Valid {
		return
	}

	return optional.UnixSeconds.Value()
}

// Scan stores SQL NULL as absent and validates a present signed integer.
func (optional *Optional) Scan(value any) error {
	if value == nil {
		*optional = Optional{UnixSeconds: 0, Valid: false}

		return nil
	}

	var decoded UnixSeconds
	if err := decoded.Scan(value); err != nil {
		return oops.In("timestamp").Code("invalid_optional_timestamp_sql").
			Wrapf(err, "scan optional Unix seconds")
	}

	*optional = Optional{UnixSeconds: decoded, Valid: true}

	return nil
}

func invalidRangeError(seconds int64) error {
	return oops.In("timestamp").Code("timestamp_out_of_range").
		Errorf("Unix seconds %d are outside the supported range 0..%d", seconds, Max)
}

func invalidSQLTypeError(value any) error {
	if value == nil {
		return oops.In("timestamp").Code("invalid_timestamp_sql").
			Errorf("required Unix seconds cannot be NULL")
	}

	return oops.In("timestamp").Code("invalid_timestamp_sql").
		Errorf("scan Unix seconds: expected int64, got %T (%s)", value, sqlValueDescription(value))
}

func sqlValueDescription(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case []byte:
		return strconv.Quote(string(typed))
	default:
		return fmt.Sprint(typed)
	}
}
