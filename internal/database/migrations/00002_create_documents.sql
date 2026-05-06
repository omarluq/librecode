-- +goose Up
CREATE TABLE IF NOT EXISTS runtime_documents (
    namespace TEXT NOT NULL,
    document_key TEXT NOT NULL,
    value_json TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (namespace, document_key)
);

-- +goose Down
DROP TABLE IF EXISTS runtime_documents;
