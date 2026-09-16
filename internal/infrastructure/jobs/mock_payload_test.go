package jobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// TestMockPayloadsPassTheRealAllowlist is the regression test for a claim the
// mocks silently broke: the deterministic video/audio payloads were rejected by
// the production content allowlist, because Go's sniffer reports
// "application/octet-stream" for a malformed MP4 brand layout and "audio/wave"
// for WAV.
//
// The earlier tests missed it by checking their own reimplementation of the
// magic bytes instead of running the real FileStore and the real allowlist. This
// test uses both: the payload travels through filestore.Store (which sniffs it)
// and then through MIMEAllowed.
func TestMockPayloadsPassTheRealAllowlist(t *testing.T) {
	root := t.TempDir()
	store, err := filestore.New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Video.
	videoAdapter := providers.NewMockVideoAdapter()
	remote, err := videoAdapter.Submit(ctx, providers.NewMockVideoRequest("job-v", "prov-1"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Poll until the mock reports completion.
	for attempt := 0; attempt < 5; attempt++ {
		status, pollErr := videoAdapter.Poll(ctx, remote)
		if pollErr != nil {
			t.Fatalf("Poll: %v", pollErr)
		}
		if status.Done {
			break
		}
	}
	media, err := videoAdapter.Fetch(ctx, remote)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	payload, err := base64.StdEncoding.DecodeString(media.Data)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Put(ctx, "mock-video.mp4", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("storing the mock video payload failed: %v", err)
	}
	if !MIMEAllowed("video", stored.MIME) {
		t.Fatalf("the mock video payload sniffs as %q, which the video allowlist rejects; the mock cannot complete a job", stored.MIME)
	}
	if stored.MIME != media.MIMEType {
		t.Fatalf("the mock declares %q but Go sniffs %q; the declared value must match what the pipeline sees", media.MIMEType, stored.MIME)
	}

	// Audio.
	audioAdapter := providers.NewMockAudioAdapter()
	outcome, err := audioAdapter.GenerateAudio(ctx, providers.NewMockAudioRequest("job-a", "prov-1"))
	if err != nil {
		t.Fatalf("GenerateAudio: %v", err)
	}
	audioPayload, err := base64.StdEncoding.DecodeString(outcome.Data)
	if err != nil {
		t.Fatal(err)
	}
	audioStored, err := store.Put(ctx, "mock-audio.wav", bytes.NewReader(audioPayload))
	if err != nil {
		t.Fatalf("storing the mock audio payload failed: %v", err)
	}
	if !MIMEAllowed("audio", audioStored.MIME) {
		t.Fatalf("the mock audio payload sniffs as %q, which the audio allowlist rejects", audioStored.MIME)
	}
	if audioStored.MIME != outcome.MIMEType {
		t.Fatalf("the mock declares %q but Go sniffs %q", outcome.MIMEType, audioStored.MIME)
	}
}

// TestAllowedMIMECoversSnifferNames pins the allowlist against the names Go's
// content sniffer actually produces for the signatures the pipeline accepts.
//
// Both directions matter: a sniffer name that is missing rejects a legitimate
// result (the WAV bug), and an allowlist entry the sniffer never emits is a dead
// rule that hides the first problem. Every entry is therefore checked against a
// real payload that is sniffed by net/http here, not against a list of names.
func TestAllowedMIMECoversSnifferNames(t *testing.T) {
	// Payloads that exercise each signature the media allowlists claim to accept.
	payloads := []struct {
		capability string
		name       string
		body       []byte
	}{
		{"audio", "RIFF/WAVE", []byte("RIFF\x00\x00\x00\x00WAVEfmt ")},
		{"image", "PNG", []byte("\x89PNG\x0D\x0A\x1A\x0A")},
		{"image", "JPEG", []byte("\xFF\xD8\xFF\xE0")},
		{"image", "GIF", []byte("GIF89a")},
		{"image", "WebP", []byte("RIFF\x00\x00\x00\x00WEBPVP")},
		{"video", "EBML/WebM", []byte("\x1A\x45\xDF\xA3")},
		{"audio", "OggS/Opus", []byte("OggS\x00\x02")},
		{"audio", "MP3/ID3", []byte("ID3\x04\x00")},
		// The exact mock layout: 24-byte ftyp box whose first non-major brand is
		// "mp41", which is what makes the sniffer report video/mp4.
		{"video", "MP4", []byte{
			0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
			'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00,
			'm', 'p', '4', '1', 'i', 's', 'o', 'm',
		}},
	}
	for _, payload := range payloads {
		sniffed := http.DetectContentType(payload.body)
		if !MIMEAllowed(payload.capability, sniffed) {
			t.Errorf("%s (%s) sniffs as %q, which the %s allowlist rejects", payload.name, payload.capability, sniffed, payload.capability)
		}
	}
	// Every allowlist entry must be producible: it must be the sniffed type of at
	// least one payload above, or the IANA spelling of a name the sniffer uses
	// (audio/wav is what adapters declare, audio/wave is what Go sniffs).
	ianaAliases := map[string]bool{"audio/wav": true}
	for _, capability := range []string{"image", "video", "audio"} {
		for _, entry := range AllowedMIME(capability) {
			if ianaAliases[entry] {
				continue
			}
			producible := false
			for _, payload := range payloads {
				if http.DetectContentType(payload.body) == entry {
					producible = true
					break
				}
			}
			if !producible {
				t.Errorf("allowlist entry %q for %q is never produced by any accepted payload; it is a dead rule", entry, capability)
			}
		}
	}
	// Unknown types stay rejected for every capability.
	for _, capability := range []string{"image", "video", "audio"} {
		if MIMEAllowed(capability, "application/octet-stream") {
			t.Fatalf("capability %q accepts an opaque payload", capability)
		}
		if MIMEAllowed(capability, "text/html") {
			t.Fatalf("capability %q accepts HTML", capability)
		}
	}
}
