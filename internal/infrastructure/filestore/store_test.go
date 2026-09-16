package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3}

func testStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	store, err := New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestPutHashesMIMEAndReadsBack(t *testing.T) {
	store := testStore(t)
	object, err := store.Put(context.Background(), "shot.png", bytes.NewReader(pngHeader))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(pngHeader)
	want := hex.EncodeToString(sum[:])
	if object.Hash != want || object.StorageKey != want || object.Size != int64(len(pngHeader)) || object.MIME != "image/png" {
		t.Fatalf("object=%#v want hash=%s", object, want)
	}
	body, err := store.Open(context.Background(), object.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil || !bytes.Equal(got, pngHeader) {
		t.Fatalf("read back %q: %v", got, err)
	}
}

func TestPutDeduplicatesIdenticalContent(t *testing.T) {
	store := testStore(t)
	first, err := store.Put(context.Background(), "a.png", bytes.NewReader(pngHeader))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(context.Background(), "b.png", bytes.NewReader(pngHeader))
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash {
		t.Fatal("duplicate content produced a new key")
	}
	if countFiles(t, store.filesDir) != 1 {
		t.Fatal("duplicate content left extra objects")
	}
}

func TestPutRejectsUnsafeDisplayNames(t *testing.T) {
	store := testStore(t)
	names := []string{"../x", "/abs", `C:\abs`, "dir/name", `dir\name`, "foo:bar", "CON", "NUL.txt", "", "a\x00b"}
	for _, name := range names {
		if _, err := store.Put(context.Background(), name, bytes.NewReader(pngHeader)); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if countFiles(t, store.filesDir)+countFiles(t, store.tempDir) != 0 {
		t.Fatal("rejected names left files")
	}
}

func TestPutCanceledCleansTemp(t *testing.T) {
	store := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		_, err := store.Put(ctx, "clip.bin", reader)
		errCh <- err
	}()
	if _, err := writer.Write(bytes.Repeat([]byte{1}, 4096)); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = writer.Close()
	if err := <-errCh; err == nil {
		t.Fatal("canceled put succeeded")
	}
	if countFiles(t, store.tempDir)+countFiles(t, store.filesDir) != 0 {
		t.Fatal("canceled put left files")
	}
}

func TestPutFailingReaderCleansTemp(t *testing.T) {
	store := testStore(t)
	_, err := store.Put(context.Background(), "clip.bin", &errAfter{n: 8, err: errors.New("boom")})
	if err == nil {
		t.Fatal("failing reader succeeded")
	}
	if countFiles(t, store.tempDir)+countFiles(t, store.filesDir) != 0 {
		t.Fatal("failing reader left files")
	}
}

func TestOpenMissingAndInvalidKeys(t *testing.T) {
	store := testStore(t)
	missing := strings.Repeat("a", 64)
	_, err := store.Open(context.Background(), missing)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != "FILE_NOT_FOUND" || appErr.Diagnostic == "" {
		t.Fatalf("missing key: %v", err)
	}
	if strings.Contains(err.Error(), store.filesDir) {
		t.Fatal("safe error leaked path")
	}
	if _, err := store.Open(context.Background(), "ZZ"); err == nil {
		t.Fatal("invalid key accepted")
	}
	if _, err := store.Open(context.Background(), "../"+missing); err == nil {
		t.Fatal("traversal key accepted")
	}
}

type errAfter struct {
	n   int
	err error
}

func (r *errAfter) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, r.err
	}
	if len(p) > r.n {
		p = p[:r.n]
	}
	for i := range p {
		p[i] = 1
	}
	r.n -= len(p)
	return len(p), nil
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}
