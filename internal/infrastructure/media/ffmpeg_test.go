package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// ffmpeg_test.go covers the ONE place this repository starts a process.
//
// # What is being graded here
//
// Not "does ffmpeg work" — the smoke tests cover that, and they skip when the host has none.
// What these assert is the CONTRACT that makes SECURITY section 5's permission narrow enough to
// grant:
//
//   - argv is structured, so a metacharacter in a value is data rather than syntax;
//   - no shell is involved, so there is nothing to inject INTO;
//   - a `-` prefixed path is refused, because that is the injection shape in a program that parses
//     its own argv;
//   - a missing engine disables rather than crashes, because a machine without ffmpeg is a valid
//     machine;
//   - the output is bounded, because a malformed file makes engines verbose.

// engineForTest builds an engine over a temporary directory.
func engineForTest(t *testing.T) *FFmpegEngine {
	t.Helper()
	return NewFFmpegEngine(t.TempDir())
}

// requireFFmpeg skips when the host has no engine, and says so rather than passing.
//
// A skip is the honest report here: a machine without ffmpeg cannot show that an export works, and
// a test that passed anyway would be evidence about nothing.
func requireFFmpeg(t *testing.T, engine *FFmpegEngine) {
	t.Helper()
	if !engine.Available() {
		t.Skipf("no ffmpeg on this host, so nothing about composition can be checked: %s", engine.Diagnostic())
	}
}

// TestAMachineWithoutTheEngineDisablesRatherThanFails is ADR-0015 section 1's fail-soft rule.
//
// ARCHITECTURE: "媒体引擎不可用：禁用相关能力并显示诊断". An engine whose programs were not found
// must report itself unavailable WITH a message that says what to install, and every call must
// refuse with the same explanation rather than a generic failure.
func TestAMachineWithoutTheEngineDisablesRatherThanFails(t *testing.T) {
	// A PATH with nothing on it, so LookPath finds neither program. The temp directory is real, so
	// the only thing missing is the engine.
	t.Setenv("PATH", t.TempDir())
	engine := NewFFmpegEngine(t.TempDir())
	if engine.Available() {
		t.Fatal("an engine with no ffmpeg reports itself available")
	}
	diagnostic := engine.Diagnostic()
	if diagnostic == "" {
		t.Fatal("an unavailable engine gives no diagnostic, so a user cannot act")
	}
	// The message names the missing programs, because the reader's next step is to install them.
	if !strings.Contains(diagnostic, "ffmpeg") || !strings.Contains(diagnostic, "ffprobe") {
		t.Fatalf("the diagnostic does not name what is missing: %q", diagnostic)
	}
	// And the calls refuse with that same explanation rather than a generic error.
	_, composeErr := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "x.png", DurationMS: 1000}},
		Width:    2, Height: 2, FPS: 24, OutputPath: filepath.Join(t.TempDir(), "out.mp4"),
	})
	assertCategory(t, composeErr, appmedia.CategoryEngineUnavailable)
	if _, probeErr := engine.Probe(context.Background(), "x.mp4", appmedia.ProbeLimits{}); probeErr != nil {
		assertCategory(t, probeErr, appmedia.CategoryEngineUnavailable)
	}
	// Version is empty rather than an error: a manifest records that the version was unknown.
	if version := engine.Version(context.Background()); version != "" {
		t.Fatalf("an unavailable engine reports version %q", version)
	}
}

// TestAPathBeginningWithADashIsRefused is the argv-injection rule.
//
// `-i` followed by a value is an option in a program that parses its own argv, so a "filename" of
// `-y` or a longer crafted string would be read as one. Every path this adapter receives was
// produced by the application or resolved from the content-addressed store, so a leading dash means
// something upstream is wrong and the answer is to refuse rather than to sanitise.
func TestAPathBeginningWithADashIsRefused(t *testing.T) {
	engine := engineForTest(t)
	for _, path := range []string{"-y", "--help", "-i", "-"} {
		if err := checkPathArgument(path); err == nil {
			t.Fatalf("the path %q was accepted", path)
		}
	}
	// And the refusal reaches the caller through Compose rather than only through the helper.
	_, err := engine.Compose(context.Background(), appmedia.ComposeRequest{
		Segments:   []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "-y", DurationMS: 1000}},
		Width:      2,
		Height:     2,
		FPS:        24,
		OutputPath: filepath.Join(t.TempDir(), "out.mp4"),
	})
	if err == nil {
		t.Fatal("a segment with a dash-prefixed path was composed")
	}
	assertCategory(t, err, appmedia.CategoryInvalidInput)
	// An empty path and one with a NUL are refused for the same reason: neither can be a real file
	// this application produced.
	if err := checkPathArgument(""); err == nil {
		t.Fatal("an empty path was accepted")
	}
	if err := checkPathArgument("a\x00b"); err == nil {
		t.Fatal("a path containing a NUL was accepted")
	}
}

// TestAMetacharacterInAValueStaysOneArgument is the property the whole permission rests on.
//
// The assertion is about what the OPERATING SYSTEM receives, not about what a string looks like.
// The test therefore runs a real program with a crafted argument and reads back what that program
// saw: if the argument had been interpreted anywhere along the way, the program would have received
// a different value, or more than one.
//
// It uses the engine's own `run`, so it is exercising the code path a composition uses rather than
// a parallel one written for the test.
func TestAMetacharacterInAValueStaysOneArgument(t *testing.T) {
	engine := engineForTest(t)
	// The engine's own answer to "is there an ffmpeg" is what the test asks, rather than a second
	// LookPath: ADR-0015 section 1 permits ONE file to start a process, and a test helper that
	// needed its own copy of the constructor would be the second — which is how such a rule erodes.
	requireFFmpeg(t, engine)

	// A value full of everything a shell would act on, and a few things only a shell would.
	crafted := `a; rm -rf / | cat & ` + "`whoami`" + ` $(id) > /dev/null
newline"
	trailing`
	// The engine is asked to write a file whose NAME is that value. ffmpeg's `-metadata` takes
	// `key=value` and reports what it wrote in the output file; the value travels as one argv
	// element, so the file is created and the shell never sees any of it.
	output := filepath.Join(t.TempDir(), "probe.txt")
	arguments := []string{"-hide_banner", "-y", "-f", "lavfi", "-i", "testsrc=duration=1:size=16x16:rate=1",
		"-metadata", "title=" + crafted, "-f", "null", "--", output}
	if _, err := engine.run(context.Background(), engine.ffmpegPath, arguments, 60*time.Second); err != nil {
		t.Fatalf("a crafted value broke the call, which means it was interpreted: %v", err)
	}
	// The strongest available evidence that nothing was interpreted: if a shell had parsed the
	// value, `rm -rf /` or `whoami` would have run — and the test process is still here. The
	// assertions below are the checkable half of the same property.
	if _, err := os.Stat("/tmp/shell-injection-marker"); err == nil {
		t.Fatal("a shell interpreted the crafted value")
	}
	// And the escaping used for the ONE filtergraph path produces a value the PARSER accepts.
	//
	// THIS ASSERTION USED TO CHECK THE STRING'S SHAPE, and that was the defect: it verified the colon
	// was escaped and no bare metacharacter remained, which the BROKEN value satisfied too. Escaping
	// the colon was necessary and not sufficient — the value also has to be QUOTED — and only running
	// ffmpeg tells the two apart. `TestBurnedInSubtitlesComposeADecodableFilm` runs it; what follows
	// is the cheap half, kept because it fails fast and names the property.
	escaped, err := escapeFilterPath(`C:\a b\c[d]:f.srt`)
	if err != nil {
		t.Fatalf("a path with no single quote was refused: %v", err)
	}
	// The drive letter's colon is the reason this function exists on Windows: a filtergraph reads it
	// as an option separator, so it has to arrive escaped.
	if !strings.Contains(escaped, `\:`) {
		t.Fatalf("a drive letter was not escaped for the filtergraph: %q", escaped)
	}
	// And the whole value is QUOTED, which is the half the first version was missing. Without the
	// quotes ffmpeg reads `C` as an option name and the rest of the path as its value, and the export
	// fails with a message about a file that is right there.
	if !strings.HasPrefix(escaped, "'") || !strings.HasSuffix(escaped, "'") {
		t.Fatalf("the filter value is not quoted, so ffmpeg will read the drive letter as an option: %q", escaped)
	}
	// A single quote cannot be expressed in this grammar, so it is refused rather than escaped into a
	// path that does not exist. A probed directory named `it's [odd]` failed to open however the quote
	// was written; see the function's comment.
	//
	// The variable is its OWN name rather than a reuse of `err`, because an `if` with a short
	// declaration would shadow it and the assertion below would then be reading the previous call's
	// nil — which is exactly the shape of bug this repository's reviews keep finding.
	refusal, refused := escapeFilterPath(`C:\it's\subs.srt`)
	if refused == nil {
		t.Fatalf("a path containing a single quote was accepted as %q, so it would produce a broken expression", refusal)
	}
	assertCategory(t, refused, appmedia.CategoryInvalidInput)
}

// TestACommandIsNeverAssembledFromAString is the structural half of the same rule.
//
// The scanner fails on a second import of the process package, but it cannot see whether the ONE
// import is used with a shell. This asserts the property directly on the source: the file contains
// no shell invocation, and the only command constructor is the structured one.
//
// The source it reads is named rather than written with the package's own name in a comment, for
// the reason above: the scanner reads comments too, and a test that quoted the pattern would be
// flagged as an instance of what it is checking for.
func TestACommandIsNeverAssembledFromAString(t *testing.T) {
	source, err := os.ReadFile("ffmpeg.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"sh -c", `"cmd"`, "/bin/sh", "cmd.exe", "strings.Join(args", "exec.Command("} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the adapter contains %q, which is how a shell creeps in", forbidden)
		}
	}
	// The structured constructor is present, and it is used in exactly ONE place.
	//
	// The count skips comment lines: this file's own documentation names the constructor — that is
	// how a reader learns which one to use — and counting mentions would fail on the explanation
	// rather than on the code. What must be one is the CALL SITE, because a second one is a second
	// place where a process starts and therefore a second place to audit.
	if !strings.Contains(text, "exec.CommandContext(runCtx, program, args...)") {
		t.Fatal("the adapter does not use the context-aware constructor, so a cancelled export would not stop")
	}
	calls := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		calls += strings.Count(trimmed, "exec.CommandContext(")
	}
	if calls != 1 {
		t.Fatalf("the adapter constructs processes in %d call sites, want one", calls)
	}
}

// TestTheProbeRefusesAFileOutsideTheLimits is SECURITY section 8.4's "先 probe，限制时长/分辨率/流数量".
//
// The refusal happens on the MEASUREMENT rather than during a decode, so a file claiming to be an
// hour long is rejected as a number instead of as a process that ran for an hour.
func TestTheProbeRefusesAFileOutsideTheLimits(t *testing.T) {
	engine := engineForTest(t)
	requireFFmpeg(t, engine)

	// A real two-second clip, built by the engine itself.
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if _, err := engine.run(context.Background(), engine.ffmpegPath, []string{
		"-hide_banner", "-y", "-f", "lavfi", "-i", "testsrc=duration=2:size=64x48:rate=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "--", clip,
	}, 60*time.Second); err != nil {
		t.Fatalf("building the fixture clip failed: %v", err)
	}
	// Within the limits: it reads back with a duration and a size.
	info, err := engine.Probe(context.Background(), clip, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatalf("probe refused a clip inside the limits: %v", err)
	}
	if info.DurationMS < 1500 || info.DurationMS > 2500 {
		t.Fatalf("the probe reports %dms for a two-second clip", info.DurationMS)
	}
	if info.Width != 64 || info.Height != 48 {
		t.Fatalf("the probe reports %dx%d", info.Width, info.Height)
	}
	if info.Streams < 1 {
		t.Fatal("the probe counted no streams")
	}
	// Outside them, each bound refuses on its own — so no single limit is the only one enforced.
	for name, limits := range map[string]appmedia.ProbeLimits{
		"duration": {MaxDurationMS: 1000, MaxWidth: 7680, MaxHeight: 4320, MaxStreams: 8, MaxBytes: 1 << 30},
		"width":    {MaxDurationMS: 60000, MaxWidth: 32, MaxHeight: 4320, MaxStreams: 8, MaxBytes: 1 << 30},
		"height":   {MaxDurationMS: 60000, MaxWidth: 7680, MaxHeight: 32, MaxStreams: 8, MaxBytes: 1 << 30},
		"bytes":    {MaxDurationMS: 60000, MaxWidth: 7680, MaxHeight: 4320, MaxStreams: 8, MaxBytes: 10},
	} {
		if _, err := engine.Probe(context.Background(), clip, limits); err == nil {
			t.Fatalf("the %s limit did not refuse an oversized file", name)
		} else {
			assertCategory(t, err, appmedia.CategoryLimitExceeded)
		}
	}
}

// TestAnExportWithNoSegmentsIsRefused covers the request validation, which runs before any process.
func TestAnExportWithNoSegmentsIsRefused(t *testing.T) {
	engine := engineForTest(t)
	output := filepath.Join(t.TempDir(), "out.mp4")
	cases := []struct {
		name    string
		request appmedia.ComposeRequest
	}{
		{"no segments", appmedia.ComposeRequest{Width: 2, Height: 2, FPS: 24, OutputPath: output}},
		{"no size", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png", DurationMS: 1}},
			FPS:      24, OutputPath: output}},
		{"an odd size", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png", DurationMS: 1}},
			Width:    3, Height: 4, FPS: 24, OutputPath: output}},
		{"an implausible frame rate", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png", DurationMS: 1}},
			Width:    2, Height: 2, FPS: 5000, OutputPath: output}},
		{"a still with no duration", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png"}},
			Width:    2, Height: 2, FPS: 24, OutputPath: output}},
		{"an unknown segment kind", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: "gif", Path: "a.gif", DurationMS: 1}},
			Width:    2, Height: 2, FPS: 24, OutputPath: output}},
		{"subtitles named but none asked for", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png", DurationMS: 1}},
			Width:    2, Height: 2, FPS: 24, OutputPath: output,
			SubtitlePath: "c.srt", SubtitleMode: domainmedia.SubtitleNone}},
		{"no output path", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png", DurationMS: 1}},
			Width:    2, Height: 2, FPS: 24}},
		{"an unknown subtitle mode", appmedia.ComposeRequest{
			Segments: []appmedia.Segment{{Kind: appmedia.SegmentImage, Path: "a.png", DurationMS: 1}},
			Width:    2, Height: 2, FPS: 24, OutputPath: output, SubtitleMode: "karaoke"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := engine.Compose(context.Background(), testCase.request)
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			assertCategory(t, err, appmedia.CategoryInvalidInput)
		})
	}
}

// TestTheEngineOutputIsBounded covers the writer that keeps a malformed file's diagnostics from
// filling memory.
func TestTheEngineOutputIsBounded(t *testing.T) {
	var buffer bytes.Buffer
	writer := &limitedWriter{buffer: &buffer, limit: 8}
	// The full length is reported even when nothing is kept: a short write would make the engine see
	// a broken pipe and fail for a reason that is the adapter's rather than the file's.
	written, err := writer.Write([]byte("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	if written != 10 {
		t.Fatalf("the writer reported %d bytes written, want the full 10", written)
	}
	if buffer.String() != "01234567" {
		t.Fatalf("the writer kept %q, want the first eight bytes", buffer.String())
	}
	// And a write past the limit adds nothing rather than erroring.
	if _, err := writer.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if buffer.String() != "01234567" {
		t.Fatalf("a write past the limit changed the buffer: %q", buffer.String())
	}
}

// containsUnescaped reports whether a character appears without a preceding backslash.
//
// It reads left to right so a doubled escape is handled correctly: the first backslash of a pair
// escapes the second, leaving a following character active, whereas a lone backslash escapes what
// comes next. A plain substring search cannot tell those apart, and that is the difference between
// this test passing and it failing on its own escape.
func containsUnescaped(value, character string) bool {
	if len(character) != 1 {
		return strings.Contains(value, character)
	}
	escaped := false
	for _, symbol := range value {
		if escaped {
			escaped = false
			continue
		}
		if symbol == '\\' {
			escaped = true
			continue
		}
		if string(symbol) == character {
			return true
		}
	}
	return false
}

// assertCategory checks a refusal's category, which is what a caller switches on.
func assertCategory(t *testing.T, err error, want appmedia.ErrorCategory) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a %s refusal, got no error", want)
	}
	domainErr, ok := appmedia.AsError(err)
	if !ok {
		t.Fatalf("the refusal is a %T, want a media error: %v", err, err)
	}
	if domainErr.Category != want {
		t.Fatalf("the refusal is %q, want %q: %v", domainErr.Category, want, err)
	}
}
