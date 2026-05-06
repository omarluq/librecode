package database

import "context"

// DocumentSource adapts runtime_documents to packages that read JSON documents.
type DocumentSource struct {
	documents *DocumentRepository
	namespace string
	key       string
}

// NewDocumentSource creates a read-only document source.
func NewDocumentSource(repository *DocumentRepository, namespace, key string) *DocumentSource {
	return &DocumentSource{documents: repository, namespace: namespace, key: key}
}

// Read returns the document contents when present.
func (source *DocumentSource) Read() (content []byte, found bool, err error) {
	document, found, err := source.documents.Get(context.Background(), source.namespace, source.key)
	if err != nil || !found {
		return nil, found, err
	}

	return []byte(document.ValueJSON), true, nil
}
