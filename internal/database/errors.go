package database

import "github.com/samber/oops"

func databaseDecodeEntryDataError(err error) error {
	return oops.In("database").Code("decode_entry_data").Wrapf(err, "decode entry data")
}

func databaseDecodeEntryUsageError(err error) error {
	return oops.In("database").Code("decode_entry_usage").Wrapf(err, "decode entry token usage")
}
