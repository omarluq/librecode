package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

// DocumentRepository persists runtime documents that upstream persists as JSON files.
type DocumentRepository struct {
	sql ksql.Provider
	now func() time.Time
}

// NewDocumentRepository creates a document repository.
func NewDocumentRepository(connection *sql.DB) (*DocumentRepository, error) {
	provider, err := newSQLProvider(connection)
	if err != nil {
		return nil, err
	}

	return NewDocumentRepositoryWithProvider(provider)
}

// NewDocumentRepositoryWithProvider creates a document repository with an explicit SQL provider.
func NewDocumentRepositoryWithProvider(provider ksql.Provider) (*DocumentRepository, error) {
	if isNilProvider(provider) {
		return nil, nilProviderError()
	}

	return &DocumentRepository{sql: provider, now: time.Now}, nil
}

type documentRow struct {
	Namespace string `ksql:"namespace"`
	Key       string `ksql:"document_key"`
	ValueJSON string `ksql:"value_json"`
	UpdatedAt string `ksql:"updated_at"`
}

func documentFromRow(row *documentRow) (*DocumentEntity, error) {
	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &DocumentEntity{UpdatedAt: updatedAt, Namespace: row.Namespace, Key: row.Key, ValueJSON: row.ValueJSON}, nil
}

// Get loads one document by namespace and key.
func (repository *DocumentRepository) Get(ctx context.Context, namespace, key string) (*DocumentEntity, bool, error) {
	const query = `
SELECT namespace, document_key, value_json, updated_at
FROM runtime_documents
WHERE namespace = ? AND document_key = ?`

	var row documentRow
	if err := repository.sql.QueryOne(ctx, &row, query, namespace, key); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("get_document").Wrapf(err, "load runtime document")
	}

	document, err := documentFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_document").Wrapf(err, "scan runtime document")
	}

	return document, true, nil
}

// Put stores or replaces one runtime document.
func (repository *DocumentRepository) Put(ctx context.Context, document *DocumentEntity) error {
	if document == nil {
		return oops.In("database").Code("nil_document").Errorf("runtime document is required")
	}

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

	_, err := repository.sql.Exec(
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

	_, err := repository.sql.Exec(ctx, statement, namespace, key)
	if err != nil {
		return oops.In("database").Code("delete_document").Wrapf(err, "delete runtime document")
	}

	return nil
}
