package units

import "math/bits"

// PercentOf returns a whole percentage clamped to PercentScale.
func PercentOf(value, total int) int {
	if value <= 0 || total <= 0 {
		return 0
	}

	if value >= total {
		return PercentScale
	}

	high, low := bits.Mul64(uint64(value), uint64(PercentScale))
	percentage, _ := bits.Div64(high, low, uint64(total))

	return int(uint(percentage))
}
