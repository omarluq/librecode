package database

import "github.com/gofrs/uuid/v5"

func newUUIDv7() string {
	return uuid.Must(uuid.NewV7()).String()
}
