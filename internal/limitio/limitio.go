// Package limitio contains helpers for bounded in-memory reads.
package limitio

import (
	"fmt"
	"io"

	"github.com/omarluq/librecode/internal/bytefmt"
)

// ReadAll reads up to limit bytes from reader and fails if more data is present.
func ReadAll(reader io.Reader, limit int64, label string) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("%s read limit cannot be negative", label)
	}

	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, limitError(err, "read limited input")
	}

	if int64(len(content)) > limit {
		return nil, fmt.Errorf("%s exceeds limit of %s", label, bytefmt.Format(limit))
	}

	return content, nil
}
