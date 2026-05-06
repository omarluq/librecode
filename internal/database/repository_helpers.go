package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/samber/oops"
)

type rowScanner interface {
	Scan(dest ...any) error
}

type rowErrorInfo struct {
	scanCode  string
	scanMsg   string
	iterCode  string
	iterMsg   string
	closeCode string
	closeMsg  string
}

func collectRows[T any](rows *sql.Rows, scan func(rowScanner) (*T, error), errorInfo *rowErrorInfo) (
	items []T,
	err error,
) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = oops.In("database").Code(errorInfo.closeCode).Wrapf(closeErr, "%s", errorInfo.closeMsg)
		}
	}()

	items = []T{}
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			return nil, oops.In("database").Code(errorInfo.scanCode).Wrapf(scanErr, "%s", errorInfo.scanMsg)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.In("database").Code(errorInfo.iterCode).Wrapf(err, "%s", errorInfo.iterMsg)
	}

	return items, nil
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
