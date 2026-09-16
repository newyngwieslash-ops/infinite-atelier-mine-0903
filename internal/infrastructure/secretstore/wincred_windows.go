//go:build windows

// Package secretstore implements the secrets.Store port against the Windows
// Credential Manager via advapi32. Values are stored as generic credentials
// under the "InfiniteAtelier:provider:<id>" target namespace.
package secretstore

import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric   = 1       // CRED_TYPE_GENERIC
	credPersistLocal  = 2       // CRED_PERSIST_LOCAL_MACHINE
	maxCredentialBlob = 5 * 512 // CRED_MAX_CREDENTIAL_BLOB_SIZE (2560 bytes)
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// WindowsStore is the production SecretStore on Windows.
type WindowsStore struct {
	mu sync.Mutex
}

// NewWindowsStore builds the Credential Manager-backed store.
func NewWindowsStore() *WindowsStore {
	return &WindowsStore{}
}

// Available always reports true on Windows builds.
func (s *WindowsStore) Available() bool { return true }

// Put writes the secret as a generic credential. The value slice is not
// retained after the call returns.
func (s *WindowsStore) Put(_ context.Context, ref string, value []byte) error {
	if err := validateRef(ref); err != nil {
		return err
	}
	if len(value) == 0 {
		return provider.NewConfigurationError()
	}
	if len(value) > maxCredentialBlob {
		return provider.NewConfigurationError()
	}
	target, err := windows.UTF16PtrFromString(ref)
	if err != nil {
		return provider.NewConfigurationError()
	}
	blob := make([]byte, len(value))
	copy(blob, value)

	credential := &credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocal,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ret, _, _ := procCredWriteW.Call(uintptr(unsafe.Pointer(credential)), 0)
	if ret == 0 {
		return storeError("write")
	}
	return nil
}

// Resolve reads the secret value. It is only callable from Go infrastructure
// provider code; never expose it through Wails bindings.
func (s *WindowsStore) Resolve(_ context.Context, ref string) ([]byte, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	target, err := windows.UTF16PtrFromString(ref)
	if err != nil {
		return nil, provider.NewConfigurationError()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var credPtr *credentialW
	ret, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&credPtr)))
	if ret == 0 {
		if isNotFound(callErr) {
			return nil, provider.NewConfigurationError()
		}
		return nil, storeError("read")
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))

	blobSize := int(credPtr.CredentialBlobSize)
	if blobSize <= 0 || blobSize > maxCredentialBlob {
		return nil, storeError("read")
	}
	value := make([]byte, blobSize)
	source := unsafe.Slice(credPtr.CredentialBlob, blobSize)
	copy(value, source)
	return value, nil
}

// Delete removes the credential. Missing entries are not an error.
func (s *WindowsStore) Delete(_ context.Context, ref string) error {
	if err := validateRef(ref); err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(ref)
	if err != nil {
		return provider.NewConfigurationError()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ret, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if ret == 0 {
		if isNotFound(callErr) {
			return nil
		}
		return storeError("delete")
	}
	return nil
}

// Exists reports whether the credential is present.
func (s *WindowsStore) Exists(ctx context.Context, ref string) (bool, error) {
	_, err := s.Resolve(ctx, ref)
	if err == nil {
		return true, nil
	}
	var providerErr *provider.Error
	if errors.As(err, &providerErr) && providerErr.Category == provider.CategoryConfiguration {
		return false, nil
	}
	return false, err
}

// DisplayHint returns a "****" + last-4 suffix derived without persisting the
// value anywhere outside the store.
func (s *WindowsStore) DisplayHint(ctx context.Context, ref string) (string, error) {
	value, err := s.Resolve(ctx, ref)
	if err != nil {
		return "", err
	}
	if len(value) < 4 {
		return "****", nil
	}
	return "****" + string(value[len(value)-4:]), nil
}

// credentialW mirrors the Windows CREDENTIALW structure. Field order matters:
// Persist follows CredentialBlob, then AttributeCount/Attributes.
// Reference: https://learn.microsoft.com/windows/win32/api/wincred/ns-wincred-credentialw
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func validateRef(ref string) error {
	if ref == "" || len(ref) > 512 {
		return provider.NewConfigurationError()
	}
	return nil
}

// isNotFound maps a syscall error from the Credential API to ERROR_NOT_FOUND.
func isNotFound(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_FOUND)
}

// storeError returns a stable, non-leaking storage error. The operation name
// is intentionally not embedded: platform messages can echo target names.
func storeError(_ string) error {
	return &provider.Error{
		Category:    provider.CategoryStorage,
		SafeMessage: "Secure secret storage failed.",
	}
}
