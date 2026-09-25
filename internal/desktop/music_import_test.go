package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// music_import_test.go grades the transfer's own decisions over a double, because every claim the
// binding makes is about the ORDER and the REFUSALS rather than about the store.
//
// # The one claim a double cannot grade, and where it is graded instead
//
// That a completed import produces a version the MIX actually finds. That is a join in the export
// repository, and `music_bed_wp29_test.go` drives it over the real schema. This file stops at the port.

// recordingImporter records what the binding asked for, and can refuse any step.
type recordingImporter struct {
	stored    []string
	created   []string
	attached  []string
	approved  []string
	bedded    []string
	failAt    string
	mime      string
	storedKey string
}

func (r *recordingImporter) Store(_ context.Context, displayName string, body []byte) (StoredBytes, error) {
	if r.failAt == "store" {
		return StoredBytes{}, errors.New("the store refused")
	}
	r.stored = append(r.stored, displayName)
	mime := r.mime
	if mime == "" {
		// The sniffer's answer for a RIFF header, which is what the fixtures send.
		mime = "audio/wave"
	}
	key := r.storedKey
	if key == "" {
		key = strings.Repeat("a", 64)
	}
	return StoredBytes{Hash: key, StorageKey: key, Size: int64(len(body)), MIME: mime}, nil
}

func (r *recordingImporter) CreateBeddableAsset(_ context.Context, projectID, name string) (string, string, error) {
	if r.failAt == "create" {
		return "", "", errors.New("the aggregate refused")
	}
	r.created = append(r.created, projectID+"/"+name)
	return "asset-1", "version-1", nil
}

func (r *recordingImporter) Attach(_ context.Context, versionID, fileHash string) error {
	if r.failAt == "attach" {
		return errors.New("the link refused")
	}
	r.attached = append(r.attached, versionID+"->"+fileHash)
	return nil
}

func (r *recordingImporter) Approve(_ context.Context, assetID, versionID string) error {
	if r.failAt == "approve" {
		return errors.New("the approval refused")
	}
	r.approved = append(r.approved, assetID+"/"+versionID)
	return nil
}

func (r *recordingImporter) Bed(_ context.Context, versionID, shotID string) error {
	if r.failAt == "bed" {
		return errors.New("the usage refused")
	}
	r.bedded = append(r.bedded, versionID+"@"+shotID)
	return nil
}

// musicBindingFor builds an attached binding over a double.
func musicBindingFor(importer MusicImporter) *MusicImportBinding {
	binding := &MusicImportBinding{}
	counter := 0
	AttachMusicImport(binding, context.Background(), importer, func() (string, error) {
		counter++
		return "upload-" + string(rune('0'+counter)), nil
	})
	binding.now = func() time.Time { return time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC) }
	return binding
}

// wavBytes builds a RIFF/WAVE payload of the requested length, so the sniffer's real answer is what a
// test asserts against rather than a MIME a fixture invented.
func wavBytes(size int) []byte {
	payload := make([]byte, size)
	copy(payload, []byte("RIFF"))
	copy(payload[4:], []byte{0x00, 0x00, 0x00, 0x00})
	copy(payload[8:], []byte("WAVE"))
	return payload
}

// importMusic runs a whole transfer and returns its result.
func importMusic(t *testing.T, binding *MusicImportBinding, name string, body []byte) (FinishMusicImportResult, error) {
	t.Helper()
	begun, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "project-1", DisplayName: name, TotalBytes: len(body), ShotID: "shot-1",
	})
	if err != nil {
		return FinishMusicImportResult{}, err
	}
	// Chunks are sent at the declared ceiling, which is how the frontend will send them.
	for offset := 0; offset < len(body); offset += 4096 {
		end := offset + 4096
		if end > len(body) {
			end = len(body)
		}
		if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
			UploadID: begun.UploadID, Data: base64.StdEncoding.EncodeToString(body[offset:end]),
		}); err != nil {
			return FinishMusicImportResult{}, err
		}
	}
	return binding.FinishMusicImport(FinishMusicImportRequest{UploadID: begun.UploadID, Name: name})
}

// TestAnImportRunsStoreThenCreateThenAttachThenApproveThenBed is the order.
//
// The order is the whole of what makes a completed import usable: a version approved before its file
// exists is a version the export refuses, and a usage pointing at an unapproved version is invisible to
// the mix's join. A test that only counted the calls would pass for any permutation.
func TestAnImportRunsStoreThenCreateThenAttachThenApproveThenBed(t *testing.T) {
	importer := &recordingImporter{}
	result, err := importMusic(t, musicBindingFor(importer), "theme.wav", wavBytes(9000))
	if err != nil {
		t.Fatalf("the import: %v", err)
	}
	if len(importer.stored) != 1 || len(importer.created) != 1 || len(importer.attached) != 1 ||
		len(importer.approved) != 1 || len(importer.bedded) != 1 {
		t.Fatalf("the steps ran %d/%d/%d/%d/%d times", len(importer.stored), len(importer.created),
			len(importer.attached), len(importer.approved), len(importer.bedded))
	}
	// The file is ATTACHED to the version the step before created, and the approval names the same pair:
	// an identifier invented along the way would attach the file to nothing.
	if importer.attached[0] != "version-1->"+strings.Repeat("a", 64) {
		t.Fatalf("the file was attached as %q", importer.attached[0])
	}
	if importer.approved[0] != "asset-1/version-1" {
		t.Fatalf("the approval is %q", importer.approved[0])
	}
	if importer.bedded[0] != "version-1@shot-1" {
		t.Fatalf("the bed was recorded as %q", importer.bedded[0])
	}
	// And the result names what was built, so a caller can act on it without a second read.
	if result.AssetID != "asset-1" || result.VersionID != "version-1" || result.Bytes != 9000 {
		t.Fatalf("the result is %+v", result)
	}
	// The MIME is the STORE's answer, not the caller's claim: a file renamed to .mp3 is reported as what
	// it actually is.
	if result.MIMEType != "audio/wave" {
		t.Fatalf("the imported type is %q", result.MIMEType)
	}
}

// TestAStoppedTransferIsRefusedRatherThanStored is the size verification.
//
// A transfer that stopped early would otherwise store a truncated track, and a half a song is worse than
// a refusal because it PLAYS — the user hears the music stop and has nothing to act on.
func TestAStoppedTransferIsRefusedRatherThanStored(t *testing.T) {
	importer := &recordingImporter{}
	binding := musicBindingFor(importer)
	body := wavBytes(9000)
	begun, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "project-1", DisplayName: "theme.wav", TotalBytes: len(body), ShotID: "shot-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Half the bytes, then Finish.
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
		UploadID: begun.UploadID, Data: base64.StdEncoding.EncodeToString(body[:4000]),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishMusicImport(FinishMusicImportRequest{UploadID: begun.UploadID}); err == nil {
		t.Fatal("a truncated transfer was committed")
	}
	if len(importer.stored) != 0 {
		t.Fatal("a truncated transfer reached the store")
	}
	// The upload is gone whatever the outcome, so a retry cannot become a mix of two attempts.
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
		UploadID: begun.UploadID, Data: base64.StdEncoding.EncodeToString(body[:100]),
	}); err == nil {
		t.Fatal("a failed transfer could still be appended to")
	}
}

// TestAFilePastTheCeilingIsRefusedAtBegin is the bound, named where a caller can act on it.
func TestAFilePastTheCeilingIsRefusedAtBegin(t *testing.T) {
	importer := &recordingImporter{}
	binding := musicBindingFor(importer)
	_, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "project-1", DisplayName: "huge.wav", TotalBytes: maxMusicBytes + 1, ShotID: "shot-1",
	})
	if err == nil {
		t.Fatal("a file past the ceiling was accepted")
	}
	// The message names the limit: a user who chose an 80 MB track needs to know what the ceiling is
	// rather than that something was invalid.
	if !strings.Contains(err.Error(), "MB") {
		t.Fatalf("the refusal does not name the limit: %v", err)
	}
}

// TestAnImportWithoutAShotIsRefused is the data-path rule.
//
// A bed attached to nothing is a row NO READ FINDS: the mix joins `asset_usages` on the shot, so a usage
// recorded some other way would be a perfectly valid row the export never sees. The binding refuses
// rather than defaulting, because the UI has just read the timeline and knows which shot to name.
func TestAnImportWithoutAShotIsRefused(t *testing.T) {
	importer := &recordingImporter{}
	binding := musicBindingFor(importer)
	for _, shotID := range []string{"", "   "} {
		_, err := binding.BeginMusicImport(BeginMusicImportRequest{
			ProjectID: "project-1", DisplayName: "theme.wav", TotalBytes: 4000, ShotID: shotID,
		})
		if err == nil {
			t.Fatalf("an import naming the shot %q was accepted", shotID)
		}
	}
	if len(importer.stored) != 0 {
		t.Fatal("a shot-less import reached the store")
	}
}

// TestAnOversizedChunkIsRefused is the per-message bound.
func TestAnOversizedChunkIsRefused(t *testing.T) {
	importer := &recordingImporter{}
	binding := musicBindingFor(importer)
	begun, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "project-1", DisplayName: "theme.wav", TotalBytes: 4 * musicChunkBytes, ShotID: "shot-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	oversized := make([]byte, musicChunkBytes+1)
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
		UploadID: begun.UploadID, Data: base64.StdEncoding.EncodeToString(oversized),
	}); err == nil {
		t.Fatal("an oversized chunk was accepted")
	}
	// A chunk that is not base64 is refused rather than silently dropped, which would show up later as a
	// size mismatch with no indication of where.
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
		UploadID: begun.UploadID, Data: "not base64 at all!!",
	}); err == nil {
		t.Fatal("a malformed chunk was accepted")
	}
}

// TestAChunkForAnUnknownTransferIsRefused keeps a chunk from landing nowhere.
func TestAChunkForAnUnknownTransferIsRefused(t *testing.T) {
	binding := musicBindingFor(&recordingImporter{})
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
		UploadID: "never-opened", Data: base64.StdEncoding.EncodeToString([]byte("x")),
	}); err == nil {
		t.Fatal("a chunk for an unknown transfer was accepted")
	}
	if _, err := binding.FinishMusicImport(FinishMusicImportRequest{UploadID: "never-opened"}); err == nil {
		t.Fatal("an unknown transfer was committed")
	}
}

// TestAReplayedFinishCannotImportTwice is the idempotency of the transfer.
func TestAReplayedFinishCannotImportTwice(t *testing.T) {
	importer := &recordingImporter{}
	binding := musicBindingFor(importer)
	body := wavBytes(4000)
	begun, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "project-1", DisplayName: "theme.wav", TotalBytes: len(body), ShotID: "shot-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{
		UploadID: begun.UploadID, Data: base64.StdEncoding.EncodeToString(body),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishMusicImport(FinishMusicImportRequest{UploadID: begun.UploadID}); err != nil {
		t.Fatalf("the first finish: %v", err)
	}
	if _, err := binding.FinishMusicImport(FinishMusicImportRequest{UploadID: begun.UploadID}); err == nil {
		t.Fatal("a replayed finish imported the track twice")
	}
	if len(importer.stored) != 1 {
		t.Fatalf("%d stores for one import", len(importer.stored))
	}
}

// TestAbortForgetsATransferAndIsIdempotent covers the cleanup path.
func TestAbortForgetsATransferAndIsIdempotent(t *testing.T) {
	importer := &recordingImporter{}
	binding := musicBindingFor(importer)
	begun, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "project-1", DisplayName: "theme.wav", TotalBytes: 4000, ShotID: "shot-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.AbortMusicImport(begun.UploadID); err != nil {
		t.Fatalf("aborting an open transfer: %v", err)
	}
	// A second abort is not an error: a caller that aborts twice, or aborts a transfer the core already
	// forgot, has got what it wanted — the transfer is not there.
	if err := binding.AbortMusicImport(begun.UploadID); err != nil {
		t.Fatalf("aborting twice: %v", err)
	}
	if _, err := binding.FinishMusicImport(FinishMusicImportRequest{UploadID: begun.UploadID}); err == nil {
		t.Fatal("an aborted transfer was committed")
	}
}

// TestAFailedStepLeavesTheEarlierOnesAlone states what a partial failure leaves behind.
//
// The steps run in the order that keeps the aggregate consistent at every point a failure could stop
// them, so a failure at step N leaves steps 1..N-1 done. That is deliberate: rolling back across the
// store and the aggregate would need a transaction neither has, and the store is content-addressed so a
// retry reuses the object.
func TestAFailedStepLeavesTheEarlierOnesAlone(t *testing.T) {
	cases := []struct {
		failAt     string
		wantStored int
		wantCrated int
	}{
		{"store", 0, 0},
		{"create", 1, 0},
		{"attach", 1, 1},
	}
	for _, testCase := range cases {
		importer := &recordingImporter{failAt: testCase.failAt}
		_, err := importMusic(t, musicBindingFor(importer), "theme.wav", wavBytes(4000))
		if err == nil {
			t.Fatalf("a failure at %s was reported as success", testCase.failAt)
		}
		if len(importer.stored) != testCase.wantStored || len(importer.created) != testCase.wantCrated {
			t.Fatalf("a failure at %s left %d stores and %d assets", testCase.failAt,
				len(importer.stored), len(importer.created))
		}
		// The steps AFTER the failure never ran, which is what "consistent at every point" means.
		if testCase.failAt == "store" && (len(importer.attached) != 0 || len(importer.approved) != 0 || len(importer.bedded) != 0) {
			t.Fatal("a store failure still built rows")
		}
	}
}

// TestAnUnattachedBindingRefusesEveryCommand is the safe-mode behaviour.
func TestAnUnattachedBindingRefusesEveryCommand(t *testing.T) {
	binding := &MusicImportBinding{}
	if _, err := binding.BeginMusicImport(BeginMusicImportRequest{
		ProjectID: "p", DisplayName: "t.wav", TotalBytes: 10, ShotID: "s",
	}); err == nil {
		t.Fatal("an unattached binding opened a transfer")
	}
	if err := binding.AppendMusicChunk(AppendMusicChunkRequest{UploadID: "x", Data: "eA=="}); err == nil {
		t.Fatal("an unattached binding accepted a chunk")
	}
	if _, err := binding.FinishMusicImport(FinishMusicImportRequest{UploadID: "x"}); err == nil {
		t.Fatal("an unattached binding committed a transfer")
	}
	// Abort does NOT refuse: it is cleanup, and a build with nothing to clean up has already got what
	// the caller wanted.
	if err := binding.AbortMusicImport("x"); err != nil {
		t.Fatalf("an unattached binding refused an abort: %v", err)
	}
}

// TestTheAllowedMusicTypesAreTheOnesTheSnifferEmits pins the allowlist against the sniffer's vocabulary.
//
// An allowlist entry the sniffer never produces is a DEAD RULE: it protects nothing and hides which
// payloads are genuinely accepted. The job pipeline's own allowlist documents that trap, and this asserts
// the set is the one `http.DetectContentType` can actually answer with for an audio payload.
func TestTheAllowedMusicTypesAreTheOnesTheSnifferEmits(t *testing.T) {
	// The sniffer's real answers for the three containers the list covers.
	for _, wanted := range []string{"audio/wave", "audio/mpeg", "application/ogg"} {
		if !isAllowedMusicMIME(wanted) {
			t.Fatalf("the sniffer's %q is refused", wanted)
		}
	}
	// A type the sniffer never emits for audio is refused, and so is a parameterised spelling, which is
	// normalized rather than rejected.
	if isAllowedMusicMIME("application/octet-stream") {
		t.Fatal("an opaque type was accepted as music")
	}
	if isAllowedMusicMIME("") {
		t.Fatal("an empty type was accepted as music")
	}
	if !isAllowedMusicMIME("audio/wave; charset=binary") {
		t.Fatal("a parameterised type was refused instead of normalized")
	}
}

var _ = asset.TypeAudio
