package terminal

import "github.com/omarluq/librecode/internal/model"

func MergeTerminalUsageForTest(current, next model.TokenUsage) model.TokenUsage {
	return mergeTerminalUsage(current, next)
}
