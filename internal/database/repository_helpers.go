package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

func querySQLRow[Row any, Entity any](
	ctx context.Context,
	provider ksql.Provider,
	convert func(*Row) (*Entity, error),
	query string,
	entity string,
	args ...any,
) (entityValue *Entity, found bool, queryErr error) {
	var row Row
	if err := provider.QueryOne(ctx, &row, query, args...); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("query_"+entity).Wrapf(err, "query %s", entity)
	}

	converted, err := convert(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_"+entity).Wrapf(err, "scan %s", entity)
	}

	return converted, true, nil
}

func querySQLRows[Row any, Entity any](
	ctx context.Context,
	provider ksql.Provider,
	convert func(*Row) (*Entity, error),
	query string,
	entity string,
	args ...any,
) ([]Entity, error) {
	rows := []Row{}
	if err := provider.Query(ctx, &rows, query, args...); err != nil {
		return nil, oops.In("database").Code("query_"+entity).Wrapf(err, "query %s", entity)
	}

	entities, err := collectSQLRows(rows, convert)
	if err != nil {
		return nil, oops.In("database").Code("scan_"+entity).Wrapf(err, "scan %s", entity)
	}

	return entities, nil
}

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
