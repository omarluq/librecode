package units

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPercentOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		value, total int
		want         int
	}{
		{name: "unknown", value: 0, total: 100, want: 0},
		{name: "quarter", value: 25, total: 100, want: 25},
		{name: "clamped", value: 101, total: 100, want: PercentScale},
		{name: "large values do not overflow", value: math.MaxInt / 2, total: math.MaxInt, want: 49},
		{name: "large values preserve floor semantics", value: math.MaxInt - 1, total: math.MaxInt, want: 99},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, PercentOf(test.value, test.total))
		})
	}
}
