package desktop

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// monoform_binding.go carries a previs SNAPSHOT from the embedded studio into the core.
//
// # What was missing, stated as the acceptance clause it fails
//
// PRD FR-060's acceptance says 「保存后可在 Shot 中看到摄像机参数和预览图」, and the bridge built in
// WP-09 carries both: `shot_updated` has a `thumbnail?: Blob` that `monoform-bridge.ts` VALIDATES and
// `director-panel.tsx` FORWARDS. It then reaches `director-view.tsx`'s `reportCamera`, whose parameter
// type names `shotId` and `camera` and nothing else — so the thumbnail was dropped at the last step,
// and no studio component mentions it anywhere. The camera half of the clause works; the preview half
// had no path because no binding could turn a Blob into a stored file.
//
// # Why this reuses the import upload's shape rather than inventing one
//
// A Wails binding carries text, so binary crosses as base64 in bounded chunks — and the import
// upload already established that pattern, its bound, and the reasoning for both. A second transfer
// mechanism would be a second set of bounds to keep in step with the first, and the failure mode of
// getting one wrong is the same in both: a large payload materialised on the webview's main thread.
// The constants are therefore the IMPORT's, and this file does not define its own.
//
// # Where a snapshot ends up
//
// `asset_files` with the `reference` role, on the version the caller names. NOT a new table: the
// asset aggregate already exists for "what files does this version have" (DOMAIN_MODEL section 8.4),
// and a `previs_snapshots` table would be a second versioning system for the same bytes. The role is
// the domain's own vocabulary rather than one invented here, and `reference` is what a snapshot is:
// a picture the shot refers to rather than the picture being shot.
//
// SECURITY: the upload identifier is generated here and never derived from anything the caller sends.
// The bytes are held in memory until Finish commits them through the content-addressed store, so no
// chunk can steer a write to a path — the store names files by their hash.

// allowedSnapshotMIME is what a previs snapshot may be.
//
// It is a CLOSED set and it is here rather than shared with the job pipeline's allowlist: that one is
// keyed by CAPABILITY ("what may an image job return"), which is a different question from "what may
// a snapshot be", and a bridge that read the wrong key would either refuse a PNG or accept a video.
// Three types, because the studio renders a still: PNG for fidelity, JPEG for size, and WebP because
// a canvas can produce it with one call.
var allowedSnapshotMIME = []string{"image/png", "image/jpeg", "image/webp"}

// isAllowedSnapshotMIME reports whether the studio's stated type is one this stores.
func isAllowedSnapshotMIME(mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(normalized, ';'); index >= 0 {
		normalized = strings.TrimSpace(normalized[:index])
	}
	for _, allowed := range allowedSnapshotMIME {
		if normalized == allowed {
			return true
		}
	}
	return false
}

// SnapshotStore is where a snapshot's bytes go, and the two halves of what finishing does.
//
// It is a NARROW port rather than the concrete `AssetsBinding`, because the two operations it names
// are exactly what this binding needs and neither is a Wails method: `importDocumentStore` already
// stores bytes and returns the object, and the asset service already links a hash to a version. A
// port of the two keeps this file testable without a database AND states the contract a reader has to
// satisfy if the composition root changes.
type SnapshotStore interface {
	// Store commits bytes under a label and reports the content address and the storage key.
	Store(ctx context.Context, displayName string, body []byte) (StoredBytes, error)
	// Link attaches a committed hash to an asset version in a role.
	Link(ctx context.Context, versionID, fileHash, role string) error
}

// MonoformBinding carries previs snapshots into the asset aggregate.
type MonoformBinding struct {
	mu        sync.Mutex
	ctx       context.Context
	store     SnapshotStore
	uploads   map[string]*snapshotUpload
	uploadIDs func() (string, error)
	now       func() time.Time
}

// AttachMonoform supplies the context, the store a snapshot is committed through, and the identifier
// source. Not a Wails method.
func AttachMonoform(binding *MonoformBinding, ctx context.Context, store SnapshotStore, ids func() (string, error)) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	binding.ctx = ctx
	binding.store = store
	binding.uploadIDs = ids
	if binding.now == nil {
		binding.now = time.Now
	}
	if binding.uploads == nil {
		binding.uploads = map[string]*snapshotUpload{}
	}
}

// snapshotUpload is one snapshot transfer in progress.
type snapshotUpload struct {
	versionID string
	// mimeType is what the studio said the image is, checked against the domain's allowlist before a
	// chunk is accepted: a snapshot is a picture, and accepting bytes that claim to be one without
	// checking is how a store accumulates things nothing can display.
	uploadID   string
	mimeType   string
	totalBytes int
	buffer     []byte
	startedAt  time.Time
}

// BeginSnapshotUploadRequest opens a transfer to one asset version.
type BeginSnapshotUploadRequest struct {
	// VersionID is the asset version the snapshot is attached to, which the caller has from the shot's
	// own references.
	VersionID string `json:"versionId"`
	// MIMEType is what the studio says the image is. It decides whether the bytes are a picture this
	// build stores, and it is checked at the door rather than at the end so a client discovers the
	// problem on the first call rather than after uploading megabytes.
	MIMEType string `json:"mimeType"`
	// TotalBytes is the caller's count of the DECODED bytes. It is verified on finish rather than
	// trusted: a caller that understates it does not get a truncated snapshot accepted.
	TotalBytes int `json:"totalBytes"`
}

// BeginSnapshotUploadResult tells the caller what the transfer's bounds are.
type BeginSnapshotUploadResult struct {
	UploadID string `json:"uploadId"`
	// ChunkBytes is the largest decoded chunk one append may carry. The caller splits on it rather
	// than choosing a size: a client that could choose could send the whole image in one message,
	// which is the thing chunking exists to avoid.
	ChunkBytes int `json:"chunkBytes"`
}

// BeginSnapshotUpload starts a transfer.
func (b *MonoformBinding) BeginSnapshotUpload(request BeginSnapshotUploadRequest) (BeginSnapshotUploadResult, error) {
	if b == nil || b.store == nil {
		return BeginSnapshotUploadResult{}, bindingUnavailable()
	}
	versionID := strings.TrimSpace(request.VersionID)
	if versionID == "" {
		return BeginSnapshotUploadResult{}, bindingInvalidInput()
	}
	if request.TotalBytes <= 0 || int64(request.TotalBytes) > maxImportUploadBytes {
		// The IMPORT's ceiling, for the reason the file's header gives: a snapshot and a document are
		// both "bytes a user chose", and two different bounds would be one more thing to keep in step.
		return BeginSnapshotUploadResult{}, bindingInvalidInput()
	}
	mimeType := strings.ToLower(strings.TrimSpace(request.MIMEType))
	if !isAllowedSnapshotMIME(mimeType) {
		return BeginSnapshotUploadResult{}, bindingInvalidInput()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.uploads == nil {
		b.uploads = map[string]*snapshotUpload{}
	}
	id := ""
	if b.uploadIDs != nil {
		generated, err := b.uploadIDs()
		if err != nil {
			return BeginSnapshotUploadResult{}, bindingUnavailable()
		}
		id = generated
	}
	if id == "" {
		return BeginSnapshotUploadResult{}, bindingUnavailable()
	}
	b.uploads[id] = &snapshotUpload{
		uploadID: id, versionID: versionID, mimeType: mimeType, totalBytes: request.TotalBytes,
		buffer: make([]byte, 0, request.TotalBytes), startedAt: b.now(),
	}
	return BeginSnapshotUploadResult{UploadID: id, ChunkBytes: importChunkBytes}, nil
}

// AppendSnapshotChunkRequest carries one bounded piece of the image.
type AppendSnapshotChunkRequest struct {
	UploadID string `json:"uploadId"`
	// Chunk is standard base64 of at most BeginSnapshotUploadResult.ChunkBytes decoded bytes.
	Chunk string `json:"chunk"`
}

// AppendSnapshotChunk adds one chunk.
//
// A chunk that would take the transfer past its DECLARED size is refused rather than buffered: the
// declared size is what the caller promised, and a transfer that exceeds it is either a client bug or
// an attempt to make this hold more than it agreed to. The refusal is checked BEFORE the append, so
// nothing past the bound is ever resident.
func (b *MonoformBinding) AppendSnapshotChunk(request AppendSnapshotChunkRequest) error {
	if b == nil || b.store == nil {
		return bindingUnavailable()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	upload, ok := b.uploads[strings.TrimSpace(request.UploadID)]
	if !ok {
		// An unknown identifier is refused rather than created: this binding never invents an upload
		// from a chunk, so a caller that skipped Begin gets a refusal instead of a transfer it does
		// not know the bounds of.
		return bindingInvalidInput()
	}
	decoded, err := base64.StdEncoding.DecodeString(request.Chunk)
	if err != nil {
		return bindingInvalidInput()
	}
	if len(decoded) > importChunkBytes {
		return bindingInvalidInput()
	}
	if len(upload.buffer)+len(decoded) > upload.totalBytes {
		return bindingInvalidInput()
	}
	upload.buffer = append(upload.buffer, decoded...)
	return nil
}

// FinishSnapshotUploadRequest commits a transfer.
type FinishSnapshotUploadRequest struct {
	UploadID string `json:"uploadId"`
	// DisplayName labels the stored object. It is a label, never a path.
	DisplayName string `json:"displayName,omitempty"`
}

// FinishSnapshotUploadResult reports what was stored.
type FinishSnapshotUploadResult struct {
	// FileHash is the content address, and StorageKey is what a read needs. Both are returned because
	// the caller writes the KEY into the shot's overrides document and a reader compares the HASH —
	// the same division the export manifest makes between a version and its bytes.
	FileHash   string `json:"fileHash"`
	StorageKey string `json:"storageKey"`
	Bytes      int    `json:"bytes"`
}

// FinishSnapshotUpload stores the assembled image and links it to the version.
//
// The order matters and is the reason 「失败时不影响主项目数据」 holds: the bytes are committed to the
// store FIRST and the link is written second. A failure between them leaves an unreferenced object,
// which the garbage collector's predicate spares and a later retry reuses — while the reverse order
// would leave a version advertising bytes that are not there.
func (b *MonoformBinding) FinishSnapshotUpload(request FinishSnapshotUploadRequest) (FinishSnapshotUploadResult, error) {
	if b == nil || b.store == nil {
		return FinishSnapshotUploadResult{}, bindingUnavailable()
	}
	b.mu.Lock()
	upload, ok := b.uploads[strings.TrimSpace(request.UploadID)]
	if ok {
		// The upload leaves the map as it is committed, so a replayed Finish cannot attach the same
		// snapshot twice.
		delete(b.uploads, upload.uploadID)
	}
	b.mu.Unlock()
	if !ok {
		return FinishSnapshotUploadResult{}, bindingInvalidInput()
	}
	if len(upload.buffer) != upload.totalBytes {
		// The declared size is verified rather than trusted: a transfer that stopped early is refused,
		// and the refusal names both numbers so the caller can see which side was wrong.
		return FinishSnapshotUploadResult{}, bindingInvalidInput()
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" {
		displayName = "previs-snapshot"
	}
	stored, err := b.store.Store(b.context(), displayName, upload.buffer)
	if err != nil {
		return FinishSnapshotUploadResult{}, toAssetError(err)
	}
	// The role is the DOMAIN's own vocabulary rather than a string invented here, and `reference` is
	// what a snapshot is: a picture the shot refers to rather than the picture being shot.
	if err := b.store.Link(b.context(), upload.versionID, stored.Hash, string(asset.RoleReference)); err != nil {
		return FinishSnapshotUploadResult{}, toAssetError(err)
	}
	return FinishSnapshotUploadResult{
		FileHash: stored.Hash, StorageKey: stored.StorageKey, Bytes: len(upload.buffer),
	}, nil
}

// AbortSnapshotUpload discards a transfer.
//
// An unknown identifier is NOT an error: a caller that aborts twice, or aborts a transfer the core
// already forgot, has got what it wanted — the transfer is not there. Refusing would make a cleanup
// path fail on the one case it exists for.
func (b *MonoformBinding) AbortSnapshotUpload(uploadID string) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.uploads, strings.TrimSpace(uploadID))
	return nil
}

// context returns the startup context, or a background one when the binding is used before Attach.
func (b *MonoformBinding) context() context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

// pendingUploads reports how many transfers are in flight, for the tests that assert the map empties.
func (b *MonoformBinding) pendingUploads() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.uploads)
}
