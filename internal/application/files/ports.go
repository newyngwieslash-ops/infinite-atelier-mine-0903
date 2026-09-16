package files

import (
	"context"
	"io"
)

// Object is the application DTO for a stored file. It does not include filesystem paths.
type Object struct {
	Hash       string
	StorageKey string
	MIME       string
	Size       int64
}

// Store persists file bytes without exposing managed paths.
type Store interface {
	Put(context.Context, string, io.Reader) (Object, error)
	Open(context.Context, string) (io.ReadCloser, error)
}

// Repository records file metadata in the source-of-truth database.
type Repository interface {
	UpsertObject(context.Context, Object) error
}
