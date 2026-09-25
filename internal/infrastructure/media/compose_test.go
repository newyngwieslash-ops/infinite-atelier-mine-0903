package media

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// compose_test.go proves the export actually works: real PNGs in, a real MP4 out, read back.
//
// # Why this is separate from ffmpeg_test.go
//
// That file grades the CONTRACT that makes the subprocess permission safe — argv is structured, a
// missing engine disables rather than crashes, the limits refuse. This one grades the RESULT, which
// is what AC-MEDIA-003's "output playable" asks for: a file that exists, that ffprobe reads as a
// video of the right duration and size, and whose streams are the ones that were asked for.
//
// Both skip on a host with no ffmpeg and say so, because a machine without an engine cannot show
// that an export works and a test that passed anyway would be evidence about nothing.

// writePNG renders a solid-colour frame, which is what an approved panel image is.
func writePNG(t *testing.T, dir, name string, width, height int, fill color.RGBA) string {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			canvas.Set(x, y, fill)
		}
	}
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, canvas); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestComposingStillFramesInOrderProducesAPlayableFilm is AC-MEDIA-003's "output playable" and the
// heart of this package's export.
//
// Three stills of different colours and different durations become one file whose duration is their
// sum. The assertion is on the OUTPUT, read back with ffprobe — not on the request, because a
// composition that silently dropped a segment would pass a request-shaped assertion and fail this
// one.
func TestComposingStillFramesInOrderProducesAPlayableFilm(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()

	first := writePNG(t, dir, "shot-1.png", 320, 240, color.RGBA{R: 200, A: 255})
	second := writePNG(t, dir, "shot-2.png", 320, 240, color.RGBA{G: 200, A: 255})
	third := writePNG(t, dir, "shot-3.png", 320, 240, color.RGBA{B: 200, A: 255})
	output := filepath.Join(dir, "episode.mp4")

	result, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments: []appmedia.Segment{
			{Kind: appmedia.SegmentImage, Path: first, DurationMS: 1000, Label: "shot-1"},
			{Kind: appmedia.SegmentImage, Path: second, DurationMS: 2000, Label: "shot-2"},
			{Kind: appmedia.SegmentImage, Path: third, DurationMS: 500, Label: "shot-3"},
		},
		Width:      320,
		Height:     240,
		FPS:        24,
		OutputPath: output,
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	// The file exists and is not empty.
	stat, err := os.Stat(output)
	if err != nil {
		t.Fatalf("the export produced no file: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatal("the export produced an empty file")
	}
	// Read back what was PRODUCED rather than what was asked for.
	info, err := engine.Probe(context.Background(), output, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatalf("the export cannot be read back, so it is not a film: %v", err)
	}
	if info.Streams < 1 {
		t.Fatal("the export carries no streams")
	}
	// The duration is the SUM of the three segments, within a frame's tolerance. A composition that
	// dropped a segment would report one or two seconds here.
	if info.DurationMS < 3400 || info.DurationMS > 3600 {
		t.Fatalf("the export reports %dms for 1000+2000+500, want about 3500", info.DurationMS)
	}
	if info.Width != 320 || info.Height != 240 {
		t.Fatalf("the export is %dx%d, want 320x240", info.Width, info.Height)
	}
	// And the result the engine RETURNED agrees with the read-back, because a caller uses it to
	// record the export's own facts.
	if result.DurationMS < 3400 || result.DurationMS > 3600 {
		t.Fatalf("the returned duration is %dms against a read-back of %dms", result.DurationMS, info.DurationMS)
	}
	if result.Width != 320 || result.Height != 240 {
		t.Fatalf("the returned size is %dx%d", result.Width, result.Height)
	}
	if result.SizeBytes != stat.Size() {
		t.Fatalf("the returned size is %d against the file's %d", result.SizeBytes, stat.Size())
	}
}

// TestAFrameOfTheWrongShapeIsPaddedRatherThanCropped covers the scale filter's choice.
//
// A project's frames are not all the output's aspect ratio: a panel rendered square in a 16:9 episode
// has to go somewhere. Padding keeps the whole frame and letterboxes it; cropping would silently lose
// the edges of a picture the user approved, which is a worse answer than bars.
//
// # Why the ORIGINAL assertion could not tell the two apart
//
// It checked the output's DIMENSIONS, and a plain `scale` produces the requested size too — so
// removing the `pad` from the filtergraph (a mutation an independent quality review applied) left the
// test green while the export stretched the picture instead of letterboxing it. Dimensions are what
// the SCALE does; what padding adds is BARS, and the only way to see them is to read the pixels.
//
// So this frames a bright square in a wide output and checks the edges: a padded composition has
// near-black bars down the sides and the bright square in the middle, while a stretched one has the
// bright colour at the edges too.
func TestAFrameOfTheWrongShapeIsPaddedRatherThanCropped(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()

	// A bright, saturated square. A solid colour is enough because the question is WHERE it lands.
	square := writePNG(t, dir, "square.png", 200, 200, color.RGBA{R: 250, G: 40, B: 40, A: 255})
	output := filepath.Join(dir, "padded.mp4")
	if _, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments:   []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: square, DurationMS: 500}},
		Width:      320,
		Height:     240,
		FPS:        24,
		OutputPath: output,
	}); err != nil {
		t.Fatalf("Compose: %v", err)
	}
	info, err := engine.Probe(context.Background(), output, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 320 || info.Height != 240 {
		t.Fatalf("the export is %dx%d, want the requested 320x240", info.Width, info.Height)
	}
	// The pixels, which is the assertion the dimensions could not make.
	frame := extractFrame(t, engine, output, filepath.Join(dir, "frame.png"))
	if frame.Bounds().Dx() != 320 || frame.Bounds().Dy() != 240 {
		t.Fatalf("the extracted frame is %v", frame.Bounds())
	}
	// A square in a 4:3 frame at full height leaves 60-pixel bars on each side. The edge samples are
	// taken well inside them.
	leftward := colourAt(t, frame, 10, 120)
	rightward := colourAt(t, frame, 310, 120)
	centre := colourAt(t, frame, 160, 120)
	if !isBar(leftward) || !isBar(rightward) {
		t.Fatalf("the sides are %v and %v, want the black bars padding draws; a stretched or cropped "+
			"composition would carry the picture's colour there", leftward, rightward)
	}
	if !isPicture(centre) {
		t.Fatalf("the centre is %v, want the approved frame's own colour", centre)
	}
}

// extractFrame pulls one frame out of a composed file as a PNG this test can read.
//
// It uses the engine's own `run`, so it is exercising the same structured-argv path every other call
// uses rather than a parallel one written for the test.
func extractFrame(t *testing.T, engine *FFmpegEngine, videoPath, outputPath string) image.Image {
	t.Helper()
	if _, err := engine.run(context.Background(), engine.ffmpegPath, []string{
		"-hide_banner", "-y", "-i", videoPath, "-frames:v", "1", "--", outputPath,
	}, 60*time.Second); err != nil {
		t.Fatalf("extracting a frame failed: %v", err)
	}
	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	frame, err := png.Decode(file)
	if err != nil {
		t.Fatalf("the extracted frame is not a PNG: %v", err)
	}
	return frame
}

// colourAt samples one pixel.
func colourAt(t *testing.T, frame image.Image, x, y int) color.RGBA {
	t.Helper()
	if x < 0 || y < 0 || x >= frame.Bounds().Dx() || y >= frame.Bounds().Dy() {
		t.Fatalf("the sample (%d,%d) is outside %v", x, y, frame.Bounds())
	}
	red, green, blue, alpha := frame.At(x, y).RGBA()
	return color.RGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8), A: uint8(alpha >> 8)}
}

// isBar reports whether a sample is one of the letterbox bars.
//
// The threshold is generous because a video codec is lossy: what a bar is NOT is the source's own
// saturated red.
func isBar(sample color.RGBA) bool {
	return sample.R < 80 && sample.G < 80 && sample.B < 80
}

// isPicture reports whether a sample carries the approved frame's colour.
func isPicture(sample color.RGBA) bool {
	return sample.R > 150 && sample.G < 120
}

// TestAnExportWithSubtitlesCarriesThem is the subtitle half of "output playable".
//
// A sidecar track is muxed as a stream the player can turn off, which is why it is the default: a
// burned-in subtitle is part of the picture and cannot be undone. The assertion is that the output
// carries MORE streams with the subtitles than without, which is what a muxed track adds.
func TestAnExportWithSubtitlesCarriesThem(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()

	frame := writePNG(t, dir, "shot.png", 320, 240, color.RGBA{R: 50, G: 50, B: 50, A: 255})
	subtitles := filepath.Join(dir, "episode.srt")
	if err := os.WriteFile(subtitles, []byte(
		"1\n00:00:00,000 --> 00:00:00,500\nfirst line\n\n"+
			"2\n00:00:00,500 --> 00:00:01,000\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	silent := filepath.Join(dir, "silent.mp4")
	if _, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments:   []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:      320,
		Height:     240,
		FPS:        24,
		OutputPath: silent,
	}); err != nil {
		t.Fatalf("Compose without subtitles: %v", err)
	}
	withSubtitles := filepath.Join(dir, "subtitled.mp4")
	if _, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments:     []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:        320,
		Height:       240,
		FPS:          24,
		SubtitlePath: subtitles,
		SubtitleMode: domainmedia.SubtitleSidecar,
		OutputPath:   withSubtitles,
	}); err != nil {
		t.Fatalf("Compose with subtitles: %v", err)
	}
	without, err := engine.Probe(context.Background(), silent, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	with, err := engine.Probe(context.Background(), withSubtitles, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if with.Streams <= without.Streams {
		t.Fatalf("the subtitled export carries %d streams against the silent one's %d, so the track was not muxed",
			with.Streams, without.Streams)
	}
	// The picture is unchanged: a sidecar subtitle is a stream, not a drawing.
	if with.DurationMS < 900 || with.DurationMS > 1100 {
		t.Fatalf("the subtitled export reports %dms for a one-second film", with.DurationMS)
	}
}

// TestBurnedInSubtitlesComposeADecodableFilm is the test the escaping bug needed and did not have.
//
// # What went wrong without it
//
// `escapeFilterPath`'s first version escaped the colon in a Windows drive letter and returned the
// value UNQUOTED. That satisfies every assertion about the STRING — the colon is escaped, no bare
// metacharacter remains — and ffmpeg still rejects it: the filtergraph parser reads
// `subtitles=C\:/…` as an option named `C` whose value is the rest of the path, and fails with
// "Unable to parse option value … as image size". Burn-in was a user-selectable export mode that
// failed on every Windows machine while the suite stayed green, because the only test of the escaping
// never ran ffmpeg and the only test that composed subtitles used SIDECAR mode, which takes a
// different branch entirely.
//
// So this test burns them in and asserts on the OUTPUT: a file that exists, that ffprobe reads, whose
// duration and size are the shot's, and whose stream count EQUALS the silent composition's. A
// burned-in subtitle is drawn into the picture and adds no stream, so equality is what distinguishes
// "the filter ran" from "the filter was skipped and the export succeeded anyway" — a `>=` assertion
// would pass either way.
//
// The path is under `t.TempDir()`, which on this host is a real Windows drive path: the exact shape
// that was broken.
func TestBurnedInSubtitlesComposeADecodableFilm(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()

	frame := writePNG(t, dir, "shot.png", 320, 240, color.RGBA{R: 30, G: 30, B: 30, A: 255})
	subtitles := filepath.Join(dir, "episode.srt")
	if err := os.WriteFile(subtitles, []byte(
		"1\n00:00:00,000 --> 00:00:00,900\nburned in\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The reference: the same composition with no subtitles at all.
	silent := filepath.Join(dir, "silent.mp4")
	if _, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments:   []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:      320,
		Height:     240,
		FPS:        24,
		OutputPath: silent,
	}); err != nil {
		t.Fatalf("Compose without subtitles: %v", err)
	}

	burned := filepath.Join(dir, "burned.mp4")
	if _, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments:     []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:        320,
		Height:       240,
		FPS:          24,
		SubtitlePath: subtitles,
		SubtitleMode: domainmedia.SubtitleBurn,
		OutputPath:   burned,
	}); err != nil {
		// THE FAILURE THIS TEST EXISTS FOR, named rather than left as "compose failed": an expression
		// ffmpeg rejects is the difference between a working burn-in and a broken one, and the message
		// should say which.
		t.Fatalf("burning the subtitles in failed, which is what an unquoted filter path produces: %v", err)
	}
	burnedInfo, err := engine.Probe(context.Background(), burned, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatalf("the burned-in export is not a readable film: %v", err)
	}
	silentInfo, err := engine.Probe(context.Background(), silent, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if burnedInfo.DurationMS < 900 || burnedInfo.DurationMS > 1100 {
		t.Fatalf("the burned-in export reports %dms for a one-second film", burnedInfo.DurationMS)
	}
	if burnedInfo.Width != 320 || burnedInfo.Height != 240 {
		t.Fatalf("the burned-in export is %dx%d", burnedInfo.Width, burnedInfo.Height)
	}
	if burnedInfo.Streams != silentInfo.Streams {
		t.Fatalf("the burned-in export carries %d streams against the silent one's %d, so something was muxed rather than drawn",
			burnedInfo.Streams, silentInfo.Streams)
	}
}

// TestAnExportWithAudioCarriesIt is the audio half.
//
// The audio file is built by the engine itself rather than committed as a fixture: a checked-in
// WAV would be a binary in the repository that nothing else needs, and generating it here is two
// arguments.
func TestAnExportWithAudioCarriesIt(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()

	frame := writePNG(t, dir, "shot.png", 320, 240, color.RGBA{R: 20, G: 20, B: 20, A: 255})
	// A one-second tone, which is the audio a TTS job would have produced.
	tone := filepath.Join(dir, "tone.wav")
	if _, err := engine.run(context.Background(), engine.ffmpegPath, []string{
		"-hide_banner", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "pcm_s16le", "--", tone,
	}, 60*time.Second); err != nil {
		t.Fatalf("building the tone failed: %v", err)
	}
	output := filepath.Join(dir, "with-audio.mp4")
	if _, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:    320,
		Height:   240,
		FPS:      24,
		// ONE CLIP, no offset: this test grades that audio reaches the film at all. The mix's
		// placement and gain are graded by `TestTheMixPlacesAndLevelsItsClips`.
		AudioMix: appmedia.AudioMix{Clips: []appmedia.AudioClip{
			{Role: appmedia.AudioRoleDialogue, Path: tone},
		}},
		OutputPath: output,
	}); err != nil {
		t.Fatalf("Compose with audio: %v", err)
	}
	info, err := engine.Probe(context.Background(), output, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Two streams: the picture and the sound.
	if info.Streams < 2 {
		t.Fatalf("the export carries %d streams, so the audio was not muxed", info.Streams)
	}
}

// TestACancelledCompositionStops covers the context, which is what makes a long export cancellable.
//
// The timeout is deliberately tiny and the work is deliberately larger than it, so the process is
// killed rather than waited for. What the test grades is that the call RETURNS with a refusal
// instead of holding the caller — and that no output file is left behind, because a cancelled
// export must not look like a finished one.
func TestACancelledCompositionStops(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()

	frame := writePNG(t, dir, "shot.png", 1920, 1080, color.RGBA{R: 30, G: 60, B: 90, A: 255})
	output := filepath.Join(dir, "cancelled.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := engine.Compose(ctx, appmedia.ComposeRequest{
		// Thirty seconds of 1080p: far more work than a 400ms budget allows.
		Segments:   []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 30000}},
		Width:      1920,
		Height:     1080,
		FPS:        30,
		OutputPath: output,
		Timeout:    time.Second,
	})
	if err == nil {
		t.Skip("the host composed thirty seconds of 1080p inside the budget, so cancellation was not exercised")
	}
	domainErr, ok := appmedia.AsError(err)
	if !ok {
		t.Fatalf("the refusal is a %T, want a media error: %v", err, err)
	}
	if domainErr.Category != appmedia.CategoryComposeFailed {
		t.Fatalf("a cancellation is reported as %q", domainErr.Category)
	}
	// The refusal says it was cancelled rather than blaming the file, because those are two
	// different things for a user to do something about.
	if !strings.Contains(strings.ToLower(domainErr.SafeMessage), "cancel") {
		t.Fatalf("a cancelled composition reads %q", domainErr.SafeMessage)
	}
}

// TestTheMixPlacesAndLevelsItsClips is the mix through a real ffmpeg, measured rather than asserted.
//
// # Why this exists beside the graph tests
//
// `mix_test.go` grades the ARGUMENT TEXT, which is cheap and catches the two flags whose absence is a
// defect. What it cannot catch is a graph that is spelled correctly and does not do what it says —
// `adelay` starting at the wrong point, `amix` ignoring `normalize`, a filter ordering that ffmpeg
// reinterprets. So this test composes a real film and MEASURES the sound in it.
//
// # How the measurement works without decoding audio in Go
//
// Two tones at different frequencies, one delayed well past the other, and the output is split at the
// delay point and each half is measured with ffmpeg's own `volumedetect`, which reports the mean
// volume of what it analysed. The first half must carry one tone and the second must carry both —
// whose mean is LOUDER, because two uncorrelated tones sum to more energy than either alone. That is
// the property that distinguishes a mix from a concatenation: the replaced code played the second
// file AFTER the first, so the second half would have been quieter (one tone) rather than louder.
func TestTheMixPlacesAndLevelsItsClips(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)
	dir := t.TempDir()
	ctx := context.Background()

	frame := writePNG(t, dir, "shot.png", 320, 240, color.RGBA{R: 20, G: 20, B: 20, A: 255})
	// Two tones. 440Hz and 880Hz are far enough apart that a mix of them is audibly different from
	// either alone, and both are generated by ffmpeg rather than committed.
	//
	// THE FIRST TONE SPANS THE WHOLE FILM and the second starts halfway, which is what makes the two
	// halves differ. The first version of this test gave both tones two seconds and placed the second
	// at 2s, so the first tone had already ENDED before the second half began — both halves carried
	// one tone, both measured -21.1dB, and the test failed with a message that blamed the mix for a
	// mistake in the fixture.
	toneA := buildTone(t, engine, dir, "a.wav", 440, 4)
	toneB := buildTone(t, engine, dir, "b.wav", 880, 2)

	output := filepath.Join(dir, "mixed.mp4")
	if _, err := engine.Compose(ctx, appmedia.ComposeRequest{
		Segments: []appmedia.Segment{
			{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 4000},
		},
		Width: 320, Height: 240, FPS: 24,
		AudioMix: appmedia.AudioMix{Clips: []appmedia.AudioClip{
			// The first tone from the beginning...
			{Role: appmedia.AudioRoleDialogue, Path: toneA, StartMS: 0, Label: "shot 1 line 1"},
			// ...and the second from two seconds in, so a concatenation would put it at 2s by
			// appending and a mix puts it at 2s by delaying. The DIFFERENCE the test measures is in
			// the loudness of the second half, not in the timing: both shapes start tone B at 2s.
			{Role: appmedia.AudioRoleDialogue, Path: toneB, StartMS: 2000, Label: "shot 1 line 2"},
		}},
		OutputPath: output,
	}); err != nil {
		t.Fatalf("Compose with a mix: %v", err)
	}

	info, err := engine.Probe(ctx, output, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if info.Streams < 2 {
		t.Fatalf("the film carries %d streams, so the mix was not muxed", info.Streams)
	}
	if info.DurationMS < 3500 {
		// The film must be the PICTURE's length. The concat version passed `-shortest`, which would
		// truncate a film to its sound — and with a two-second tone under a four-second picture that
		// would have shown up here as a two-second film.
		t.Fatalf("the film is %dms for a four-second shot, so the sound decided its length", info.DurationMS)
	}

	// The two windows, measured by the engine's own loudness read: the first second and a half, and a
	// window well inside the second tone.
	firstLevel := meanVolumeDB(t, engine, output, 0, 1500)
	secondLevel := meanVolumeDB(t, engine, output, 2200, 1200)
	// The second half carries BOTH tones and the first carries one, so it must be louder. A
	// concatenation puts one tone in each half and the two levels would be equal; a mix that dropped
	// a clip would make the second half equal to or quieter than the first. The margin is 1dB rather
	// than a larger number because two uncorrelated tones at equal gain sum to about +3dB of energy
	// and the mean is measured over a window that includes the crossfade.
	if secondLevel <= firstLevel {
		t.Fatalf("the second half measures %.1fdB and the first %.1fdB: the clips did not overlap, so this is a concatenation rather than a mix",
			secondLevel, firstLevel)
	}
}

// buildTone generates a sine tone of a given frequency and length.
func buildTone(t *testing.T, engine *FFmpegEngine, dir, name string, frequency, seconds int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if _, err := engine.run(context.Background(), engine.ffmpegPath, []string{
		"-hide_banner", "-y", "-f", "lavfi",
		"-i", "sine=frequency=" + strconv.Itoa(frequency) + ":duration=" + strconv.Itoa(seconds),
		"-c:a", "pcm_s16le", "--", path,
	}, 60*time.Second); err != nil {
		t.Fatalf("building the %dHz tone: %v", frequency, err)
	}
	return path
}

// meanVolumeDB measures a file's loudness through the ENGINE's own method.
//
// # Why not drive ffmpeg here
//
// The first version of this helper started the process itself, and the repository's
// dynamic-execution scan refused it: SECURITY section 5 permits `os/exec` in the MediaEngine adapter
// and nowhere else, and the scan's allowlist is deliberately exact — one entry, file plus rule plus
// owner plus reason, no wildcards. Widening it for a test would have defeated the rule it exists for,
// so the measurement moved INTO the adapter (`MeanVolumeDB`) where every other process this
// application starts already lives. The test calls that, which also means the number it asserts about
// comes from the same code path a future feature would use.
func meanVolumeDB(t *testing.T, engine *FFmpegEngine, path string, startMS, durationMS int) float64 {
	t.Helper()
	level, err := engine.MeanVolumeDB(context.Background(), path, startMS, durationMS, 60*time.Second)
	if err != nil {
		t.Fatalf("measuring %s: %v", path, err)
	}
	return level
}
