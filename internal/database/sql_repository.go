package database

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
)

func nilProviderError() error {
	return oops.In("database").Code("nil_sql_provider").Errorf("sql provider is required")
}

func isNilProvider(provider ksql.Provider) bool {
	if provider == nil {
		return true
	}

	value := reflect.ValueOf(provider)
	kind := value.Kind()
	nilable := kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice

	return nilable && value.IsNil()
}

func sameSQLProvider(left, right ksql.Provider) bool {
	if isNilProvider(left) || isNilProvider(right) {
		return false
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)

	if leftValue.Type() != rightValue.Type() || !leftValue.Comparable() || !rightValue.Comparable() {
		return false
	}

	return leftValue.Interface() == rightValue.Interface()
}

func newSQLProvider(connection *sql.DB) (ksql.DB, error) {
	if connection == nil {
		return ksql.DB{}, oops.In("database").Code("nil_sql_connection").Errorf("sql connection is required")
	}

	if err := connection.PingContext(context.Background()); err != nil {
		return ksql.DB{}, oops.In("database").Code("ping_sql_connection").Wrapf(err, "ping sql connection")
	}

	provider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		return ksql.DB{}, oops.In("database").Code("sql_provider").Wrapf(err, "create sql provider")
	}

	return provider, nil
}
