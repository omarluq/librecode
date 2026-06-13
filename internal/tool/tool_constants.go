package tool

import (
	"fmt"
	"time"
)

const (
	commandWaitDelay          = 2 * time.Second
	limitSuggestionMultiplier = 2
	maxTruncationLines        = 200
	privateDirMode            = 0o700
	privateFileMode           = 0o600
	secureDirMode             = 0o700
)

func limitReachedNotice(noun string, limit int, suffix string) string {
	message := fmt.Sprintf(
		"%d %s limit reached. Use limit=%d for more",
		limit,
		noun,
		limit*limitSuggestionMultiplier,
	)
	if suffix != "" {
		message += ", " + suffix
	}

	return message
}
