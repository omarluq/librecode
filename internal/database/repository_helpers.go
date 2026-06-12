package database

import (
	"fmt"
	"time"

	"github.com/samber/lo"
)

func collectSQLRows[T any, R any](rows []T, convert func(*T) (*R, error)) ([]R, error) {
	return lo.MapErr(rows, func(row T, _ int) (R, error) {
		item, err := convert(&row)
		if err != nil {
			var zero R
			return zero, err
		}

		return *item, nil
	})
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("database: parse timestamp: %w", err)
	}

	return parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
