package timestamp_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/omarluq/librecode/internal/timestamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	negativeName  = "negative"
	epochName     = "epoch"
	nullName      = "null"
	nullJSON      = `null`
	overflowName  = "overflow"
	malformedName = "malformed"
	maximumName   = "maximum"
)

func TestFromUnixValidatesFullRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		seconds   int64
		want      timestamp.UnixSeconds
		wantError bool
	}{
		{name: negativeName, seconds: -1, want: 0, wantError: true},
		{name: epochName, seconds: 0, want: 0, wantError: false},
		{name: "signed 32-bit maximum", seconds: math.MaxInt32, want: math.MaxInt32, wantError: false},
		{name: "past signed 32-bit", seconds: int64(math.MaxInt32) + 1, want: 1 << 31, wantError: false},
		{name: "unsigned 32-bit maximum", seconds: int64(math.MaxUint32), want: timestamp.Max, wantError: false},
		{name: "past unsigned 32-bit", seconds: int64(math.MaxUint32) + 1, want: 0, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := timestamp.FromUnix(test.seconds)
			if test.wantError {
				require.ErrorContains(t, err, "outside the supported range")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.want, got)
		})
	}
}

func TestTimeConversionsUseDocumentedQuantization(t *testing.T) {
	t.Parallel()

	maxTime := time.Unix(int64(math.MaxUint32), 0).UTC()
	tests := []struct {
		input     time.Time
		convert   func(time.Time) (timestamp.UnixSeconds, error)
		name      string
		want      timestamp.UnixSeconds
		wantError bool
	}{
		{
			name: "observation exact second", input: time.Unix(10, 0), convert: timestamp.FromTime,
			want: 10, wantError: false,
		},
		{
			name: "observation fractional second floors", input: time.Unix(10, 999_999_999),
			convert: timestamp.FromTime, want: 10, wantError: false,
		},
		{
			name: "observation before epoch rejected", input: time.Unix(-1, 999_999_999),
			convert: timestamp.FromTime, want: 0, wantError: true,
		},
		{
			name: "deadline exact second", input: time.Unix(10, 0), convert: timestamp.DeadlineFromTime,
			want: 10, wantError: false,
		},
		{
			name: "deadline fractional second ceils", input: time.Unix(10, 1),
			convert: timestamp.DeadlineFromTime, want: 11, wantError: false,
		},
		{
			name: "deadline before epoch rejected", input: time.Unix(-1, 999_999_999),
			convert: timestamp.DeadlineFromTime, want: 0, wantError: true,
		},
		{
			name: "deadline maximum exact second", input: maxTime, convert: timestamp.DeadlineFromTime,
			want: timestamp.Max, wantError: false,
		},
		{
			name: "deadline ceiling overflows", input: maxTime.Add(time.Nanosecond),
			convert: timestamp.DeadlineFromTime, want: 0, wantError: true,
		},
		{
			name: "deadline second past maximum", input: maxTime.Add(time.Second),
			convert: timestamp.DeadlineFromTime, want: 0, wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.convert(test.input)
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.want, got)
		})
	}
}

func TestAddRoundsUpAndChecksRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		base      timestamp.UnixSeconds
		duration  time.Duration
		want      timestamp.UnixSeconds
		wantError bool
	}{
		{name: "zero", base: 10, duration: 0, want: 10, wantError: false},
		{name: "whole positive second", base: 10, duration: time.Second, want: 11, wantError: false},
		{name: "fractional positive second ceils", base: 10, duration: time.Nanosecond, want: 11, wantError: false},
		{name: "mixed positive duration ceils", base: 10, duration: time.Second + 1, want: 12, wantError: false},
		{name: "whole negative second", base: 10, duration: -time.Second, want: 9, wantError: false},
		{name: "fractional negative duration ceils", base: 10, duration: -time.Nanosecond, want: 10, wantError: false},
		{name: "mixed negative duration ceils", base: 10, duration: -time.Second - 1, want: 9, wantError: false},
		{name: "underflow whole second", base: 0, duration: -time.Second, want: 0, wantError: true},
		{name: "underflow fractional second", base: 0, duration: -time.Nanosecond, want: 0, wantError: true},
		{name: "maximum unchanged", base: timestamp.Max, duration: 0, want: timestamp.Max, wantError: false},
		{name: "overflow whole second", base: timestamp.Max, duration: time.Second, want: 0, wantError: true},
		{name: "overflow while ceiling", base: timestamp.Max, duration: time.Nanosecond, want: 0, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.base.Add(test.duration)
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.want, got)
		})
	}
}

func TestComparisonSubtractionAndFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		left        timestamp.UnixSeconds
		right       timestamp.UnixSeconds
		wantCompare int
		wantBefore  bool
		wantAfter   bool
		wantEqual   bool
	}{
		{
			name: "before", left: 1, right: 2, wantCompare: -1,
			wantBefore: true, wantAfter: false, wantEqual: false,
		},
		{
			name: "equal", left: 2, right: 2, wantCompare: 0,
			wantBefore: false, wantAfter: false, wantEqual: true,
		},
		{
			name: "after", left: timestamp.Max, right: 0, wantCompare: 1,
			wantBefore: false, wantAfter: true, wantEqual: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantCompare, test.left.Compare(test.right))
			assert.Equal(t, test.wantBefore, test.left.Before(test.right))
			assert.Equal(t, test.wantAfter, test.left.After(test.right))
			assert.Equal(t, test.wantEqual, test.left.Equal(test.right))
		})
	}

	assert.Equal(t, time.Duration(math.MaxUint32)*time.Second, timestamp.Max.Sub(0))
	assert.Equal(t, -time.Duration(math.MaxUint32)*time.Second, timestamp.UnixSeconds(0).Sub(timestamp.Max))
	assert.Equal(t, "1970-01-01 00:00:00 UTC", timestamp.UnixSeconds(0).Format("2006-01-02 15:04:05 MST"))
	assert.Equal(t, "2106-02-07T06:28:15Z", timestamp.Max.Format(time.RFC3339))
}

func TestUnixSecondsJSONRoundTripsNumbers(t *testing.T) {
	t.Parallel()

	for _, value := range []timestamp.UnixSeconds{0, math.MaxInt32, 1 << 31, timestamp.Max} {
		t.Run(value.Format("20060102T150405"), func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, value.Unix(), mustJSONInteger(t, encoded))

			var decoded timestamp.UnixSeconds
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			assert.Equal(t, value, decoded)
		})
	}
}

func TestUnixSecondsJSONRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: nullName, input: nullJSON},
		{name: negativeName, input: `-1`},
		{name: "fractional", input: `1.5`},
		{name: overflowName, input: `4294967296`},
		{name: "string", input: `"1"`},
		{name: "boolean", input: `true`},
		{name: "object", input: `{}`},
		{name: malformedName, input: `1x`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value := timestamp.UnixSeconds(42)
			require.Error(t, json.Unmarshal([]byte(test.input), &value))
			assert.Equal(t, timestamp.UnixSeconds(42), value)
		})
	}
}

func TestOptionalJSONDistinguishesAbsentFromEpoch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  timestamp.Optional
	}{
		{name: "absent", input: nullJSON, want: timestamp.Optional{UnixSeconds: 0, Valid: false}},
		{name: epochName, input: `0`, want: timestamp.Optional{UnixSeconds: 0, Valid: true}},
		{name: maximumName, input: `4294967295`, want: timestamp.Optional{UnixSeconds: timestamp.Max, Valid: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var decoded timestamp.Optional
			require.NoError(t, json.Unmarshal([]byte(test.input), &decoded))
			assert.Equal(t, test.want, decoded)

			encoded, err := json.Marshal(decoded)
			require.NoError(t, err)
			assert.JSONEq(t, test.input, string(encoded))
		})
	}

	invalid := []string{`-1`, `1.5`, `4294967296`, `"1"`, `true`, `{}`, `1x`}
	for _, input := range invalid {
		value := timestamp.Optional{UnixSeconds: 42, Valid: true}
		require.Error(t, json.Unmarshal([]byte(input), &value))
		assert.Equal(t, timestamp.Optional{UnixSeconds: 42, Valid: true}, value)
	}
}

func TestSQLValueUsesSignedIntegersAndNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value driver.Valuer
		want  driver.Value
		name  string
	}{
		{name: "required epoch", value: timestamp.UnixSeconds(0), want: int64(0)},
		{name: "required maximum", value: timestamp.Max, want: int64(math.MaxUint32)},
		{name: "optional absent", value: timestamp.Optional{UnixSeconds: 0, Valid: false}, want: nil},
		{
			name: "optional epoch", value: timestamp.Optional{UnixSeconds: 0, Valid: true},
			want: int64(0),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := test.value.Value()
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestUnixSecondsSQLScanValidatesStorageClassAndRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input     any
		name      string
		want      timestamp.UnixSeconds
		wantError bool
	}{
		{name: "null", input: nil, want: 0, wantError: true},
		{name: "negative", input: int64(-1), want: 0, wantError: true},
		{name: "epoch", input: int64(0), want: 0, wantError: false},
		{name: "signed 32-bit maximum", input: int64(math.MaxInt32), want: math.MaxInt32, wantError: false},
		{name: "past signed 32-bit", input: int64(math.MaxInt32) + 1, want: 1 << 31, wantError: false},
		{name: "maximum", input: int64(math.MaxUint32), want: timestamp.Max, wantError: false},
		{name: "overflow", input: int64(math.MaxUint32) + 1, want: 0, wantError: true},
		{name: "text", input: "1", want: 0, wantError: true},
		{name: "bytes", input: []byte("1"), want: 0, wantError: true},
		{name: "real", input: float64(1), want: 0, wantError: true},
		{name: "malformed", input: struct{}{}, want: 0, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value := timestamp.UnixSeconds(42)

			err := value.Scan(test.input)
			if test.wantError {
				require.Error(t, err)
				assert.Equal(t, timestamp.UnixSeconds(42), value)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.want, value)
		})
	}
}

func TestOptionalSQLScanDistinguishesNullFromEpoch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input     any
		name      string
		want      timestamp.Optional
		wantError bool
	}{
		{name: nullName, input: nil, want: timestamp.Optional{UnixSeconds: 0, Valid: false}, wantError: false},
		{name: epochName, input: int64(0), want: timestamp.Optional{UnixSeconds: 0, Valid: true}, wantError: false},
		{
			name: "maximum", input: int64(math.MaxUint32),
			want: timestamp.Optional{UnixSeconds: timestamp.Max, Valid: true}, wantError: false,
		},
		{
			name: negativeName, input: int64(-1),
			want: timestamp.Optional{UnixSeconds: 0, Valid: false}, wantError: true,
		},
		{
			name: overflowName, input: int64(math.MaxUint32) + 1,
			want: timestamp.Optional{UnixSeconds: 0, Valid: false}, wantError: true,
		},
		{
			name: "text", input: "1",
			want: timestamp.Optional{UnixSeconds: 0, Valid: false}, wantError: true,
		},
		{
			name: "real", input: float64(1),
			want: timestamp.Optional{UnixSeconds: 0, Valid: false}, wantError: true,
		},
		{
			name: malformedName, input: []byte("bad"),
			want: timestamp.Optional{UnixSeconds: 0, Valid: false}, wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value := timestamp.Optional{UnixSeconds: 42, Valid: true}

			err := value.Scan(test.input)
			if test.wantError {
				require.Error(t, err)
				assert.Equal(t, timestamp.Optional{UnixSeconds: 42, Valid: true}, value)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.want, value)
		})
	}
}

func TestCompactLayoutsAreInlineAndPointerFree(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uintptr(4), unsafe.Sizeof(timestamp.UnixSeconds(0)))
	assert.Equal(t, uintptr(8), unsafe.Sizeof(timestamp.Optional{UnixSeconds: 0, Valid: false}))
	assert.False(t, containsPointer(reflect.TypeFor[timestamp.UnixSeconds]()))
	assert.False(t, containsPointer(reflect.TypeFor[timestamp.Optional]()))
	assert.Equal(t, reflect.TypeFor[timestamp.UnixSeconds](), reflect.TypeFor[timestamp.Optional]().Field(0).Type)
}

func mustJSONInteger(t *testing.T, data []byte) int64 {
	t.Helper()

	var value int64
	require.NoError(t, json.Unmarshal(data, &value))

	return value
}

func containsPointer(valueType reflect.Type) bool {
	switch valueType.Kind() {
	case reflect.Array:
		return containsPointer(valueType.Elem())
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String,
		reflect.UnsafePointer:
		return true
	case reflect.Struct:
		for field := range valueType.Fields() {
			if containsPointer(field.Type) {
				return true
			}
		}
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return false
	}

	return false
}

var (
	_ json.Marshaler   = timestamp.UnixSeconds(0)
	_ json.Unmarshaler = (*timestamp.UnixSeconds)(nil)
	_ driver.Valuer    = timestamp.UnixSeconds(0)
	_ sql.Scanner      = (*timestamp.UnixSeconds)(nil)
	_ json.Marshaler   = timestamp.Optional{UnixSeconds: 0, Valid: false}
	_ json.Unmarshaler = (*timestamp.Optional)(nil)
	_ driver.Valuer    = timestamp.Optional{UnixSeconds: 0, Valid: false}
	_ sql.Scanner      = (*timestamp.Optional)(nil)
)
