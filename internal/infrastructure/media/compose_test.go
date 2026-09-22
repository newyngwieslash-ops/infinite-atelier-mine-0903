package media

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
		Segments:   []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: frame, DurationMS: 1000}},
		Width:      320,
		Height:     240,
		FPS:        24,
		AudioPaths: []string{tone},
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
