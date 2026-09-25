package desktop

import (
	"bytes"
	"context"
	"io"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)

// import_store.go adapts the file store to the port the import service declares.
//
// The two are close but not identical, and the difference is deliberate rather
// than an accident. The store STREAMS: Put takes an io.Reader and Open returns an
// io.ReadCloser, because it also carries media that must never be read into
// memory whole. The import service needs bytes: it hashes them, compares the hash
// against what the project already holds, and then decodes them, so a stream it
// could only read once would have to be buffered anyway.
//
// So this adapter buffers, and it is honest about the bound. An import refuses a
// document larger than MaxInputBytes before it reads one, and this refuses one
// larger than that at the store boundary too. The ceiling is a domain constant
// rather than a limit invented here, so the two cannot disagree.

// importDocumentStore adapts a content-addressed file store to the document store
// the import service needs.
type importDocumentStore struct {
	store *filestore.Store
}

// NewDocumentStore builds the adapter the import service is composed with.
func NewDocumentStore(store *filestore.Store) *importDocumentStore {
	return &importDocumentStore{store: store}
}

// StoredBytes is what a caller outside this package needs to know about a stored object: its content
// address and the key a read uses. It is a TYPE rather than the application layer's object because the
// composition root's adapter hands it to another package, and that package must not depend on the
// import service's own shapes.
type StoredBytes struct {
	Hash       string
	StorageKey string
	Size       int64
}

// DocumentStoring is the narrow surface a caller outside this package uses to commit bytes.
//
// It is a TYPE ALIAS-free wrapper over the same `Import` the import service uses, because a second
// store path would be a second place the size bound lives.
type DocumentStoring struct {
	store *importDocumentStore
}

// NewDocumentStoreForSnapshots exposes the same adapter under the shape a non-import caller needs.
func NewDocumentStoreForSnapshots(store *filestore.Store) *DocumentStoring {
	return &DocumentStoring{store: &importDocumentStore{store: store}}
}

// ImportBytes commits bytes and reports their address.
func (d *DocumentStoring) ImportBytes(ctx context.Context, displayName string, body []byte) (StoredBytes, error) {
	if d == nil || d.store == nil {
		return StoredBytes{}, importdomain.StorageError("The document store is unavailable.", nil)
	}
	object, err := d.store.Import(ctx, displayName, body)
	if err != nil {
		return StoredBytes{}, err
	}
	return StoredBytes{Hash: object.Hash, StorageKey: object.StorageKey, Size: object.Size}, nil
}

// Import stores bytes under a display name and returns the stored object.
//
// An object is content-addressed, so importing the same bytes twice is one stored
// object and a second metadata upsert. That is what makes a re-import after a
// failed row write cheap rather than a duplicate.
func (a *importDocumentStore) Import(ctx context.Context, displayName string, body []byte) (appfiles.Object, error) {
	if a == nil || a.store == nil {
		return appfiles.Object{}, importdomain.StorageError("The document store is unavailable.", nil)
	}
	// Checked before buffering rather than after: the caller already holds the
	// bytes, but a refusal here keeps the rule at the boundary it belongs to, so
	// a future caller that streams cannot bypass it.
	if int64(len(body)) > importdomain.MaxInputBytes {
		return appfiles.Object{}, importdomain.InvalidError("The document is larger than an import accepts.")
	}
	object, err := a.store.Put(ctx, displayName, bytes.NewReader(body))
	if err != nil {
		return appfiles.Object{}, importdomain.StorageError("The document could not be stored.", err)
	}
	return object, nil
}

// Open returns a stored object's bytes.
//
// The read is bounded, so an object that grew past what an import accepts is
// refused rather than read into memory. The extra byte is what distinguishes a
// read that exactly fills the limit from one that overruns it; without it a
// document of exactly the limit and one of any larger size would be
// indistinguishable.
func (a *importDocumentStore) Open(ctx context.Context, storageKey string) ([]byte, error) {
	if a == nil || a.store == nil {
		return nil, importdomain.StorageError("The document store is unavailable.", nil)
	}
	stream, err := a.store.Open(ctx, storageKey)
	if err != nil {
		return nil, importdomain.StorageError("The stored document could not be read.", err)
	}
	defer stream.Close()
	content, err := io.ReadAll(io.LimitReader(stream, importdomain.MaxInputBytes+1))
	if err != nil {
		return nil, importdomain.StorageError("The stored document could not be read.", err)
	}
	if int64(len(content)) > importdomain.MaxInputBytes {
		return nil, importdomain.InvalidError("The stored document is larger than an import accepts.")
	}
	return content, nil
}
