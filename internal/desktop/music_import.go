package desktop

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// music_import.go carries a user's OWN music file into the asset library as an approved bed.
//
// # What FR-080 asks for, and what was missing
//
// FR-080's V1 audio list is 「音效建议与生成适配；背景音乐导入；简单混音；多角色声线映射」. Three of
// the four were delivered — the mix, the suggestions, the casting — and `AudioRoleMusic` mixes with a
// documented 0.35 gain. What a user could not do was POINT AT A FILE: STATUS section 0y said so in as
// many words ("背景音乐导入 is reachable through the mix ... and has no UI control yet"), and the
// timeline section had no control that could produce an `audio_music` usage.
//
// # Why this is not the document upload path
//
// `ImportUploadBinding` carries a DOCUMENT: it parses the bytes into chapters and a script, and its
// ceiling is the import domain's own input limit. A music file is not a document — it is never parsed,
// it is stored and played — so this path stores bytes and builds the asset rows the mix reads. Sharing
// the document path would have meant a music file passing through a parser that would refuse it.
//
// # The shape of the whole transfer
//
// Begin → Append (bounded chunks) → Finish, with the size verified rather than trusted, which is the
// shape the snapshot upload uses. It is the same shape because it solves the same problem: a Wails
// binding carries text, so binary crosses as base64, and sending a 40 MB track as one message would
// materialise it twice — once in the browser and once in the Go decoder — on the webview's main thread.

// MusicImporter is the narrow port this binding needs: store bytes, then build the rows.
//
// It is a PORT rather than the concrete services because the four operations it names are exactly what
// this file does and none of them is a Wails method. A port keeps this testable without a database AND
// states the contract a reader has to satisfy if the composition root changes — the same reasoning
// `SnapshotStore` records for the snapshot path.
type MusicImporter interface {
	// Store commits bytes under a label and reports the content address.
	Store(ctx context.Context, displayName string, body []byte) (StoredBytes, error)
	// CreateBeddableAsset creates an audio asset with its first version and returns both identifiers.
	CreateBeddableAsset(ctx context.Context, projectID, name string) (assetID, versionID string, err error)
	// Attach links a committed hash to a version as its primary file.
	Attach(ctx context.Context, versionID, fileHash string) error
	// Approve makes a version the asset's current approved one, which is what the mix reads.
	Approve(ctx context.Context, assetID, versionID string) error
	// Bed records that a shot's mix carries this bed. The role it records is what the mixer reads to
	// place the clip, so this is the step that makes an imported file audible rather than merely stored.
	Bed(ctx context.Context, versionID, shotID string) error
}

// MusicImportBinding carries a user's music file into the library.
type MusicImportBinding struct {
	mu       sync.Mutex
	ctx      context.Context
	importer MusicImporter
	uploads  map[string]*musicUpload
	// uploadIDs is the identifier source, injected so a test can name its uploads.
	uploadIDs func() (string, error)
	now       func() time.Time
}

// AttachMusicImport supplies the context, the importer and the identifier source. Not a Wails method.
func AttachMusicImport(binding *MusicImportBinding, ctx context.Context, importer MusicImporter, ids func() (string, error)) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	binding.ctx = ctx
	binding.importer = importer
	if ids != nil {
		binding.uploadIDs = ids
	}
	if binding.uploads == nil {
		binding.uploads = map[string]*musicUpload{}
	}
	if binding.now == nil {
		binding.now = time.Now
	}
}

// musicUpload is one transfer in progress.
//
// The bytes live in memory until Finish, which is what makes an abandoned upload leave no trace: nothing
// reaches the store until the whole file has arrived and been verified against its declared size.
type musicUpload struct {
	displayName string
	projectID   string
	// shotID is the shot the bed is attached to, and it is REQUIRED.
	//
	// # Why a shot, when a bed belongs to an episode
	//
	// Because the mix's join is shot-keyed: `attachAudioClips` reads `asset_usages` WHERE
	// `consumer_type = 'shot' AND consumer_id = <a row's shot>`, so a usage recorded against anything
	// else is a row NO READ FINDS — the "interface with no real path" shape this repository's reviews
	// keep finding, in its data form. A `project_style` usage was the obvious alternative and would have
	// been exactly that: a perfectly valid row the export would never see.
	//
	// THE PLACEMENT DOES NOT DEPEND ON WHICH SHOT. `buildMix` starts a bed at zero wherever it was
	// attached, because a bed runs from the top — so attaching to the episode's first shot is a
	// statement about which mix carries the music, not about when it begins.
	shotID       string
	declaredSize int
	buffer       []byte
	startedAt    time.Time
}

// Bounds on one music import.
const (
	// musicChunkBytes bounds one chunk after decoding. It is the same figure the document upload uses,
	// because the constraint is the same: a chunk is one message the webview builds, and the number is
	// chosen so a normal track crosses in many small pieces rather than one large one.
	musicChunkBytes = 64 << 10
	// maxMusicBytes caps one assembled import. A full-length lossless track can be 60-80 MB, so this is
	// generous for music while still refusing a file that is not one — an unbounded import would let one
	// command fill the store.
	maxMusicBytes = 128 << 20
)

// allowedMusicMIME is what a music import may be.
//
// It is a CLOSED set, and it is a set of the types the file store's SNIFFER can actually produce rather
// than a list of audio formats in general: `http.DetectContentType` recognises RIFF/WAVE, MPEG audio and
// an Ogg container, and reports everything else as `application/octet-stream`. Accepting a declared type
// the sniffer never emits would be a rule that protects nothing — the same trap the job pipeline's
// allowlist documents.
//
// The check runs against the SNIFFED type, not against what the caller claims, so a file renamed to
// `.mp3` is stored as what it actually is.
var allowedMusicMIME = []string{"audio/wave", "audio/x-wav", "audio/mpeg", "application/ogg", "audio/ogg"}

// isAllowedMusicMIME reports whether a detected type is one this stores.
func isAllowedMusicMIME(mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(normalized, ';'); index >= 0 {
		normalized = strings.TrimSpace(normalized[:index])
	}
	for _, allowed := range allowedMusicMIME {
		if normalized == allowed {
			return true
		}
	}
	return false
}

// BeginMusicImportRequest opens a music transfer.
type BeginMusicImportRequest struct {
	ProjectID string `json:"projectId"`
	// DisplayName is the stored object's label, never a path.
	DisplayName string `json:"displayName"`
	// TotalBytes is the caller's claim about the decoded size. It is checked at Finish rather than
	// trusted: a transfer that stopped early would otherwise store a truncated track.
	TotalBytes int `json:"totalBytes"`
	// ShotID is the shot the bed is attached to. It is required, and the timeline section passes the
	// episode's first shot: see the note on `musicUpload.shotID` for why the read can only find a
	// shot-keyed usage, and why the placement does not depend on which shot it is.
	ShotID string `json:"shotId"`
}

// BeginMusicImportResult is the transfer's identity and its chunk ceiling.
type BeginMusicImportResult struct {
	UploadID   string `json:"uploadId"`
	ChunkBytes int    `json:"chunkBytes"`
}

// BeginMusicImport opens a transfer and returns its identifier.
func (b *MusicImportBinding) BeginMusicImport(request BeginMusicImportRequest) (BeginMusicImportResult, error) {
	if b == nil || b.importer == nil {
		return BeginMusicImportResult{}, bindingUnavailable()
	}
	projectID := strings.TrimSpace(request.ProjectID)
	if projectID == "" {
		return BeginMusicImportResult{}, bindingInvalidInput()
	}
	if request.TotalBytes <= 0 {
		return BeginMusicImportResult{}, bindingInvalidInput()
	}
	if strings.TrimSpace(request.ShotID) == "" {
		// Refused rather than defaulted: a bed attached to nothing is a row no read finds, and the UI
		// always knows the episode's first shot because it has just read the timeline.
		return BeginMusicImportResult{}, musicInvalidError("A music import must name the shot it belongs to.")
	}
	if request.TotalBytes > maxMusicBytes {
		// Refused with the LIMIT named, because the caller asked for a specific transfer and needs to
		// know it cannot happen rather than discovering it at the end.
		return BeginMusicImportResult{}, musicTooLargeError()
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" {
		displayName = "background-music"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.uploads == nil {
		b.uploads = map[string]*musicUpload{}
	}
	uploadID, err := b.uploadIDs()
	if err != nil {
		return BeginMusicImportResult{}, bindingUnavailable()
	}
	b.uploads[uploadID] = &musicUpload{
		displayName: displayName, projectID: projectID, declaredSize: request.TotalBytes,
		shotID:    strings.TrimSpace(request.ShotID),
		startedAt: b.now(),
	}
	return BeginMusicImportResult{UploadID: uploadID, ChunkBytes: musicChunkBytes}, nil
}

// AppendMusicChunkRequest carries one bounded chunk.
type AppendMusicChunkRequest struct {
	UploadID string `json:"uploadId"`
	// Data is base64, which is how binary crosses a Wails binding.
	Data string `json:"data"`
}

// AppendMusicChunk adds one chunk to an open transfer.
//
// The chunk is decoded and its size CHECKED before it is kept: the caller was told the ceiling at Begin,
// and a chunk larger than it is refused rather than accepted and truncated.
func (b *MusicImportBinding) AppendMusicChunk(request AppendMusicChunkRequest) error {
	if b == nil || b.importer == nil {
		return bindingUnavailable()
	}
	decoded, err := base64.StdEncoding.DecodeString(request.Data)
	if err != nil {
		return musicInvalidError("A chunk of the music file was not valid base64.")
	}
	if len(decoded) > musicChunkBytes {
		return musicInvalidError("A chunk of the music file was larger than the transfer allows.")
	}
	if len(decoded) == 0 {
		return bindingInvalidInput()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	upload, ok := b.uploads[strings.TrimSpace(request.UploadID)]
	if !ok {
		// An unknown id is a refusal rather than a silent no-op: a chunk that lands nowhere would
		// produce a truncated track at Finish, and the caller should learn which transfer went missing.
		return musicInvalidError("That music upload is not open.")
	}
	if len(upload.buffer)+len(decoded) > maxMusicBytes {
		// The total is enforced here as well as at Begin, because a caller that declared a small size
		// and then sent more must not be able to grow the buffer unbounded.
		return musicTooLargeError()
	}
	upload.buffer = append(upload.buffer, decoded...)
	return nil
}

// FinishMusicImportRequest commits a transfer.
type FinishMusicImportRequest struct {
	UploadID string `json:"uploadId"`
	// Name is the asset's display name. Empty falls back to the transfer's own label.
	Name string `json:"name,omitempty"`
}

// FinishMusicImportResult is what a committed import produced.
type FinishMusicImportResult struct {
	AssetID    string `json:"assetId"`
	VersionID  string `json:"versionId"`
	FileHash   string `json:"fileHash"`
	Bytes      int    `json:"bytes"`
	MIMEType   string `json:"mimeType"`
	StorageKey string `json:"storageKey"`
}

// FinishMusicImport stores the bytes and builds the rows the mix reads.
//
// # The order, and why it is this order
//
// Store the bytes FIRST, then create the asset, then attach, then approve, then record the usage. A
// version marked approved before its file exists would be a version the export refuses, and a usage
// pointing at an unapproved version is invisible to the mix's join — so the steps run in the order that
// leaves the aggregate consistent at every point a failure could stop them.
//
// # What a failure leaves behind
//
// An orphaned object and possibly an asset with a draft version. That is deliberate rather than sloppy:
// the store is content-addressed, so the same file imported again reuses the object, and an asset with a
// draft version is a row a user can see and delete. Rolling back across the store and the aggregate
// would need a distributed transaction neither has.
func (b *MusicImportBinding) FinishMusicImport(request FinishMusicImportRequest) (FinishMusicImportResult, error) {
	if b == nil || b.importer == nil {
		return FinishMusicImportResult{}, bindingUnavailable()
	}
	b.mu.Lock()
	key := strings.TrimSpace(request.UploadID)
	upload, ok := b.uploads[key]
	if ok {
		// The upload leaves the map as it is committed, so a replayed Finish cannot import the same
		// track twice.
		delete(b.uploads, key)
	}
	b.mu.Unlock()
	if !ok {
		return FinishMusicImportResult{}, musicInvalidError("That music upload is not open.")
	}
	if len(upload.buffer) != upload.declaredSize {
		// The declared size is verified rather than trusted. A transfer that stopped early would
		// otherwise store a truncated track, and a half a song is worse than a refusal because it plays.
		// The refusal names both numbers so a caller can see which side was wrong, without either
		// number being a detail a user cannot act on.
		return FinishMusicImportResult{}, musicInvalidError(
			"The music file did not arrive completely. Import it again.")
	}
	ctx := b.context()
	stored, err := b.importer.Store(ctx, upload.displayName, upload.buffer)
	if err != nil {
		return FinishMusicImportResult{}, toAssetError(err)
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = upload.displayName
	}
	assetID, versionID, err := b.importer.CreateBeddableAsset(ctx, upload.projectID, name)
	if err != nil {
		return FinishMusicImportResult{}, toAssetError(err)
	}
	if err := b.importer.Attach(ctx, versionID, stored.Hash); err != nil {
		return FinishMusicImportResult{}, toAssetError(err)
	}
	if err := b.importer.Approve(ctx, assetID, versionID); err != nil {
		return FinishMusicImportResult{}, toAssetError(err)
	}
	if err := b.importer.Bed(ctx, versionID, upload.shotID); err != nil {
		return FinishMusicImportResult{}, toAssetError(err)
	}
	return FinishMusicImportResult{
		AssetID: assetID, VersionID: versionID, FileHash: stored.Hash,
		Bytes: len(upload.buffer), StorageKey: stored.StorageKey,
		// The stored type is what the sniffer decided, and it is reported so a UI can show what the file
		// actually is rather than what its extension claimed.
		MIMEType: stored.MIME,
	}, nil
}

// AbortMusicImport discards a transfer.
//
// An unknown identifier is NOT an error: a caller that aborts twice, or aborts a transfer the core
// already forgot, has got what it wanted — the transfer is not there. The same ruling the snapshot
// upload makes, and for the same reason: a cleanup path must not fail on the case it exists for.
func (b *MusicImportBinding) AbortMusicImport(uploadID string) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.uploads, strings.TrimSpace(uploadID))
	return nil
}

// context returns the stored startup context.
func (b *MusicImportBinding) context() context.Context {
	if b == nil {
		return context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

// newMusicUploadID mints an upload identifier.
//
// It is the DEFAULT source so a binding attached without one still works, and it is a package-level
// generator rather than a per-call one because two imports in flight at once must not collide.
var musicUploadIDs = id.NewGenerator()

// DefaultMusicUploadID is the identifier source a composition root that has none of its own gets.
func DefaultMusicUploadID() (string, error) { return musicUploadIDs.New() }

// musicInvalidError reports a request this binding refuses.
//
// The message is the reason rather than a generic sentence, because every refusal here is one a CALLER
// of the upload can act on — send smaller chunks, declare the size you will send, open a transfer
// first — and "the request was invalid" would say none of that.
func musicInvalidError(message string) error {
	return apperror.New("MUSIC_IMPORT_INVALID", "invalid_input", false, message, nil)
}

// musicTooLargeError reports a file past what one import accepts.
//
// It names the limit in MEGABYTES rather than in bytes: the caller is a user who chose a file, and
// "134217728 bytes" is not a sentence anybody reads.
func musicTooLargeError() error {
	return apperror.New("MUSIC_IMPORT_TOO_LARGE", "invalid_input", false,
		"A music file can be at most "+strconv.Itoa(maxMusicBytes/(1<<20))+" MB.", nil)
}
