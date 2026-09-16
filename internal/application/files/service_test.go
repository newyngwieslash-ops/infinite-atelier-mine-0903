package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeStore struct {
	object Object
	err    error
	name   string
}

func (f *fakeStore) Put(_ context.Context, name string, _ io.Reader) (Object, error) {
	f.name = name
	return f.object, f.err
}

func (f *fakeStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}

type fakeRepo struct {
	object Object
	err    error
}

func (f *fakeRepo) UpsertObject(_ context.Context, object Object) error {
	f.object = object
	return f.err
}

func TestImportPersistsObjectThenMetadata(t *testing.T) {
	store := &fakeStore{object: Object{Hash: strings.Repeat("a", 64), StorageKey: strings.Repeat("a", 64), MIME: "text/plain", Size: 4}}
	repo := &fakeRepo{}
	got, err := NewService(store, repo).Import(context.Background(), "note.txt", bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatal(err)
	}
	if store.name != "note.txt" || got != store.object || repo.object != got {
		t.Fatalf("import did not use store then repository: %#v %#v", got, repo.object)
	}
}

func TestImportStopsWhenStoreFails(t *testing.T) {
	store := &fakeStore{err: errors.New("put failed")}
	repo := &fakeRepo{}
	if _, err := NewService(store, repo).Import(context.Background(), "note.txt", bytes.NewReader(nil)); err == nil {
		t.Fatal("store failure ignored")
	}
	if repo.object.Hash != "" {
		t.Fatal("repository called after store failure")
	}
}
