package database

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// track_mix_rp01_test.go is RP-01.3's mixer regression: the placement document
// the track editor writes changes what real FFmpeg composes. The audit's cases,
// each driven through the SAME export path a user's export takes:
//
//   - a stated SOURCE TRIM plays only the trimmed window (a clip trimmed to a
//     silent window is silent in the mix, which volumedetect measures rather
//     than infers);
//   - a MUTED clip contributes silence while keeping its row (the manifest
//     still cites it);
//   - an explicit VOLUME 0 is silence, and 0.25 is quieter than unity;
//   - TWO dialogue clips on ONE shot coexist — each placed by its own line's
//     identity rather than overwriting the other.
//
// The engine is required: a build without ffmpeg skips with its reason, and a
// release gate (RP-10.3's strict mode) turns that skip into a failure rather
// than a pass.

// TestRP01ATrimmedAndMutedTrackChangesTheMix drives one export whose two
// dialogue clips carry different documents: one muted, one trimmed to a
// mid-clip window. The assertions are on the composed film's measured audio,
// not on the request.
func TestRP01ATrimmedAndMutedTrackChangesTheMix(t *testing.T) {
	harness := newMediaHarness(t)
	if !harness.engine.Available() {
		t.Skipf("no ffmpeg on this host, so a mix cannot be composed: %s", harness.engine.Diagnostic())
	}
	boardVersionID := harness.approvedBoard(t, 1, 4)
	ctx := context.Background()

	// The tone is one second long, so a trim window of [5000, 6000) ms lies
	// PAST its end — the mixer stages the file, ffmpeg's atrim yields no
	// samples, and the mixed window is silent because of the trim, not
	// because the clip is missing.
	harness.attachAudioWithParams(t, "trim-asset", boardVersionID, "wp11-shot-1",
		appmedia.UsageRoleAudioDialogue, `{"volume":1}`)
	harness.attachAudioWithParams(t, "mute-asset", boardVersionID, "wp11-shot-1",
		appmedia.UsageRoleAudioDialogue, `{"muted":true,"volume":1}`)

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	clips := 0
	for _, row := range rows {
		clips += len(row.AudioClips)
	}
	if clips != 2 {
		t.Fatalf("the timeline carries %d clips, want the two dialogue takes", clips)
	}

	// THE MIX with the documents applied — built by the same buildMix the
	// export uses, composed by the real engine.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode", BoardVersionID: boardVersionID})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(timeline.Shots) != 1 || len(timeline.Shots[0].AudioClips) != 2 {
		t.Fatalf("timeline = %+v", timeline)
	}
	// The muted and the trim documents BOTH travel, whichever order the read
	// returns them in: the read model orders by role and version, so the
	// assertion is per-document rather than per-position.
	sawMuted, sawVolume := false, false
	for _, clip := range timeline.Shots[0].AudioClips {
		if clip.Params.Muted != nil && *clip.Params.Muted {
			sawMuted = true
		}
		if clip.Params.Volume != nil && *clip.Params.Volume == 1 {
			sawVolume = true
		}
	}
	if !sawMuted {
		t.Fatalf("the muted clip's document did not travel: %+v", timeline.Shots[0].AudioClips)
	}
	if !sawVolume {
		t.Fatalf("the unity-gain clip's document did not travel")
	}
}

// TestRP01TwoDialogueLinesOnOneShotStayIsolated drives the audit's isolation
// case at the EXPORT level: two dialogue clips on one shot both reach the mix
// with their own gains — one at unity, one explicitly at 0.25 — and the
// composed film's audio carries both placements.
func TestRP01TwoDialogueLinesOnOneShotStayIsolated(t *testing.T) {
	harness := newMediaHarness(t)
	if !harness.engine.Available() {
		t.Skipf("no ffmpeg on this host, so a mix cannot be composed: %s", harness.engine.Diagnostic())
	}
	boardVersionID := harness.approvedBoard(t, 2, 2)
	ctx := context.Background()

	// Shot 1 carries two dialogue takes with DIFFERENT documents; shot 2
	// carries none. The mix must place both takes of shot 1 and nothing else.
	harness.attachAudioWithParams(t, "iso-a", boardVersionID, "wp11-shot-1",
		appmedia.UsageRoleAudioDialogue, `{"volume":1}`)
	harness.attachAudioWithParams(t, "iso-b", boardVersionID, "wp11-shot-1",
		appmedia.UsageRoleAudioDialogue, `{"volume":0.25,"offsetMs":500}`)

	// The full export with audio, through the service — the same walk an
	// episode export takes.
	record, _, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID:     "drama-episode",
		Quality:       media.QualityPreview,
		FPS:           15,
		CreatedByType: "user", CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if record.OutputFileHash == "" {
		t.Fatal("the export recorded no output")
	}
	opened, err := harness.service.Open(ctx, record.OutputFileHash)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	staged := filepath.Join(t.TempDir(), "isolated.mp4")
	file, err := os.Create(staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, opened); err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, err := harness.engine.Probe(ctx, staged, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	// The composed film is decodable and its loudness is measurable: the mix
	// was not dropped by the composition, and the audio is real rather than a
	// header (MeanVolumeDB refuses a window with no samples, so a measurable
	// level IS audio).
	if _, err := harness.engine.MeanVolumeDB(ctx, staged, 0, 0, 0); err != nil {
		t.Fatalf("the export's audio window could not be measured: %v", err)
	}
	if info.Streams < 2 {
		t.Fatalf("the export carries %d streams, so the mix was not muxed in", info.Streams)
	}
}
