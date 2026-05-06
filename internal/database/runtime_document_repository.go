package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/samber/oops"
)

// DocumentRepository persists Pi runtime documents that upstream persists as JSON files.
type DocumentRepository struct {
	connection *sql.DB
	now        func() time.Time
}

// NewDocumentRepository creates a document repository.
func NewDocumentRepository(connection *sql.DB) *DocumentRepository {
	return &DocumentRepository{connection: connection, now: time.Now}
}

// Get loads one document by namespace and key.
func (repository *DocumentRepository) Get(ctx context.Context, namespace, key string) (*DocumentEntity, bool, error) {
	const query = `
SELECT namespace, document_key, value_json, updated_at
FROM runtime_documents
WHERE namespace = ? AND document_key = ?`

	document, err := scanDocument(repository.connection.QueryRowContext(ctx, query, namespace, key))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_document").Wrapf(err, "load runtime document")
	}

	return document, true, nil
}

// Put stores or replaces one runtime document.
func (repository *DocumentRepository) Put(ctx context.Context, document *DocumentEntity) error {
	if err := validateDocumentEntity(document); err != nil {
		return oops.In("database").Code("validate_document").Wrapf(err, "validate runtime document")
	}

	updatedAt := document.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = repository.now().UTC()
	}
	const statement = `
INSERT INTO runtime_documents (namespace, document_key, value_json, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(namespace, document_key) DO UPDATE SET
    value_json = excluded.value_json,
    updated_at = excluded.updated_at`
	_, err := repository.connection.ExecContext(
		ctx,
		statement,
		document.Namespace,
		document.Key,
		document.ValueJSON,
		formatTime(updatedAt),
	)
	if err != nil {
		return oops.In("database").Code("put_document").Wrapf(err, "store runtime document")
	}

	return nil
}

// Delete removes one runtime document.
func (repository *DocumentRepository) Delete(ctx context.Context, namespace, key string) error {
	const statement = `DELETE FROM runtime_documents WHERE namespace = ? AND document_key = ?`
	_, err := repository.connection.ExecContext(ctx, statement, namespace, key)
	if err != nil {
		return oops.In("database").Code("delete_document").Wrapf(err, "delete runtime document")
	}

	return nil
}

func scanDocument(row rowScanner) (*DocumentEntity, error) {
	var updatedAtRaw string
	document := DocumentEntity{UpdatedAt: time.Time{}, Namespace: "", Key: "", ValueJSON: ""}
	if err := row.Scan(&document.Namespace, &document.Key, &document.ValueJSON, &updatedAtRaw); err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	document.UpdatedAt = updatedAt

	return &document, nil
}
