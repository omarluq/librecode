package database

import (
	"fmt"
	"time"

	"github.com/samber/lo"
)

func collectSQLRows[T any, R any](rows []T, convert func(*T) (*R, error)) ([]R, error) {
	output, err := lo.MapErr(rows, func(row T, _ int) (R, error) {
		item, err := convert(&row)
		if err != nil {
			var zero R

			return zero, err
		}

		return *item, nil
	})
	if err != nil {
		return nil, fmt.Errorf("database: collect rows: %w", err)
	}

	return output, nil
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
