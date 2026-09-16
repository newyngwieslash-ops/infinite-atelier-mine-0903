package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

var storageKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var windowsDevices = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// Store is a content-addressed local file store.
type Store struct {
	filesDir string
	tempDir  string
}

// New creates the managed files and temp directories.
func New(filesDir, tempDir string) (*Store, error) {
	for _, dir := range []string{filesDir, tempDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
		}
	}
	return &Store{filesDir: filesDir, tempDir: tempDir}, nil
}

// Put validates the display name, stores bytes under their content hash, and never uses the name as a path.
func (s *Store) Put(ctx context.Context, displayName string, body io.Reader) (files.Object, error) {
	if err := validateDisplayName(displayName); err != nil {
		return files.Object{}, err
	}
	temp, err := createOwnedTemp(s.tempDir)
	if err != nil {
		return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
	}
	object, err := s.writeObject(ctx, temp, body)
	if err != nil {
		name := temp.Name()
		_ = temp.Close()
		_ = os.Remove(name)
		return files.Object{}, err
	}
	return object, nil
}

func (s *Store) writeObject(ctx context.Context, temp *os.File, body io.Reader) (files.Object, error) {
	hasher := sha256.New()
	header := make([]byte, 0, 512)
	buf := make([]byte, 32*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
		}
		n, readErr := body.Read(buf)
		if err := ctx.Err(); err != nil {
			return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
		}
		if n > 0 {
			chunk := buf[:n]
			if len(header) < 512 {
				need := 512 - len(header)
				if need > n {
					need = n
				}
				header = append(header, chunk[:need]...)
			}
			if _, err := temp.Write(chunk); err != nil {
				return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
			}
			_, _ = hasher.Write(chunk)
			size += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", readErr)
		}
	}
	if err := temp.Sync(); err != nil {
		return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
	}
	if err := temp.Close(); err != nil {
		return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	dest, err := s.objectPath(hash)
	if err != nil {
		return files.Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
	}
	if info, err := os.Lstat(dest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", errors.New("symlink"))
		}
		if err := os.Remove(temp.Name()); err != nil {
			return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
		}
		return files.Object{Hash: hash, StorageKey: hash, MIME: http.DetectContentType(header), Size: size}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
	}
	if err := os.Rename(temp.Name(), dest); err != nil {
		return files.Object{}, fileError("FILE_WRITE_FAILED", "The file could not be stored.", err)
	}
	return files.Object{Hash: hash, StorageKey: hash, MIME: http.DetectContentType(header), Size: size}, nil
}

// Open accepts only a lowercase 64-hex storage key and does not return managed paths.
func (s *Store) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	path, err := s.objectPath(storageKey)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fileError("FILE_NOT_FOUND", "The requested file could not be found.", err)
	}
	if err != nil {
		return nil, fileError("FILE_NOT_FOUND", "The requested file could not be found.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fileError("FILE_NOT_FOUND", "The requested file could not be found.", errors.New("symlink"))
	}
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fileError("FILE_NOT_FOUND", "The requested file could not be found.", err)
	}
	return file, nil
}

func (s *Store) objectPath(storageKey string) (string, error) {
	if !storageKeyPattern.MatchString(storageKey) {
		return "", fileError("FILE_NOT_FOUND", "The requested file could not be found.", errors.New("invalid storage key"))
	}
	root, err := filepath.Abs(s.filesDir)
	if err != nil {
		return "", fileError("FILE_NOT_FOUND", "The requested file could not be found.", err)
	}
	path, err := filepath.Abs(filepath.Join(s.filesDir, storageKey[:2], storageKey))
	if err != nil {
		return "", fileError("FILE_NOT_FOUND", "The requested file could not be found.", err)
	}
	sep := string(os.PathSeparator)
	if path != root && !strings.HasPrefix(path, root+sep) {
		return "", fileError("FILE_NOT_FOUND", "The requested file could not be found.", errors.New("path escape"))
	}
	return path, nil
}

func createOwnedTemp(tempDir string) (*os.File, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return nil, err
	}
	return os.CreateTemp(tempDir, "put-"+hex.EncodeToString(suffix[:])+"-")
}

func validateDisplayName(name string) error {
	invalid := fileError("FILE_INVALID_NAME", "The file name is not allowed.", errors.New("invalid display name"))
	if name == "" || strings.ContainsRune(name, 0) {
		return invalid
	}
	if strings.ContainsAny(name, `/\:`) || filepath.IsAbs(name) {
		return invalid
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return invalid
	}
	base := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		base = name[:i]
	}
	if _, ok := windowsDevices[strings.ToUpper(base)]; ok {
		return invalid
	}
	return nil
}

func fileError(code, message string, cause error) error {
	return apperror.New(code, "storage", false, message, cause)
}
