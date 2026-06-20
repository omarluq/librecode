// Package bytefmt formats byte counts for user-facing messages.
package bytefmt

import (
	"strings"

	"github.com/dustin/go-humanize"
)

// Format formats byteCount using compact IEC units.
func Format(byteCount int64) string {
	if byteCount <= 0 {
		return "0B"
	}

	return strings.ReplaceAll(humanize.IBytes(uint64(byteCount)), " ", "")
}
