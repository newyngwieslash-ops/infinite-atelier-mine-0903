package media

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
)

// content_analysis_t16.go extends the media engine with the CONTENT analysis
// the 2026-09-26 audit's T16 asks for: black-frame and silence detection over
// a real file, bounded and cancellable, as an OPTIONAL pass the final ruleset
// uses when an engine is present.
//
// # Why optional, and what "optional" means
//
// The deterministic checks already answer the metadata half (size, MIME,
// duration). Decoding pixels and audio costs CPU proportional to the file,
// which a machine without ffmpeg cannot pay at all — so the analysis is a
// method on the ENGINE (nil-safe, available-checked), and a caller that has
// no engine keeps the metadata-only answer. A caller that HAS an engine gets
// the content answer: a file whose container is legal but whose frames are
// black is located to its time range instead of passing.
//
// # The bounds
//
// Both analyses run over the file with a timeout and the engine's own output
// cap (runReadingStderr), so a hostile file cannot pin the process. The
// report carries the LOCATED ranges rather than a boolean, which is what
// makes a finding actionable and a legitimate black frame (an intentional
// fade-out, a silent pause) distinguishable by WHERE it sits.

// ContentAnalysis is one file's content-level report.
type ContentAnalysis struct {
	// BlackRanges are the [startMS, endMS) windows whose frames were (almost)
	// entirely black.
	BlackRanges [][2]int
	// SilentRanges are the [startMS, endMS) windows whose audio was silent.
	SilentRanges [][2]int
}

// HasBlackFrames reports whether any black range was found.
func (c ContentAnalysis) HasBlackFrames() bool { return len(c.BlackRanges) > 0 }

// HasSilence reports whether any silent range was found.
func (c ContentAnalysis) HasSilence() bool { return len(c.SilentRanges) > 0 }

// AnalyzeContent runs blackdetect and silencedetect over one file.
//
// The timeout bounds the whole analysis; the audit's "取消/超时/尺寸限制" is
// the ctx cancellation plus this timeout plus runReadingStderr's output cap.
func (e *FFmpegEngine) AnalyzeContent(ctx context.Context, path string, timeout time.Duration) (ContentAnalysis, error) {
	if e == nil || !e.Available() {
		return ContentAnalysis{}, appmedia.NotAvailableError(e.Diagnostic())
	}
	if err := checkPathArgument(path); err != nil {
		return ContentAnalysis{}, err
	}
	analysis := ContentAnalysis{}

	// BLACK FRAMES: blackdetect reports pic_th windows whose pixels are
	// (almost) black. The thresholds are the filter's own defaults, which are
	// what a reviewer can compare against ffmpeg's documentation.
	blackArgs := []string{"-hide_banner", "-y", "-i", path,
		"-vf", "blackdetect=d=0.5:pix_th=0.10", "-an", "-f", "null", "--", os.DevNull}
	stderr, err := e.runReadingStderr(ctx, e.ffmpegPath, blackArgs, timeout)
	if err != nil {
		return ContentAnalysis{}, err
	}
	analysis.BlackRanges = parseBlackRanges(stderr)

	// SILENCE: silencedetect reports noise-floor windows. -50 dB is the
	// filter's default floor and the same convention the mix's own volume
	// formatting uses for "silence".
	silenceArgs := []string{"-hide_banner", "-y", "-i", path,
		"-af", "silencedetect=noise=-50dB:d=1", "-vn", "-f", "null", "--", os.DevNull}
	stderr, err = e.runReadingStderr(ctx, e.ffmpegPath, silenceArgs, timeout)
	if err != nil {
		return ContentAnalysis{}, err
	}
	analysis.SilentRanges = parseSilenceRanges(stderr)

	return analysis, nil
}

// parseBlackRanges reads blackdetect's stderr lines:
//
//	[blackdetect @ …] black_start:1.2 black_end:3.4 black_duration:2.2
//
// Times are seconds with fraction; the report converts to milliseconds so the
// caller's vocabulary is one unit.
func parseBlackRanges(output string) [][2]int {
	ranges := [][2]int{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "black_start:") {
			continue
		}
		startMS, ok1 := extractSecondField(line, "black_start:")
		endMS, ok2 := extractSecondField(line, "black_end:")
		if ok1 && ok2 {
			ranges = append(ranges, [2]int{startMS, endMS})
		}
	}
	return ranges
}

// parseSilenceRanges reads silencedetect's two-line pairs:
//
//	[silencedetect @ …] silence_start: 4.5
//	[silencedetect @ …] silence_end: 7.2 | silence_duration: 2.7
//
// A start without its end (a file ending mid-silence) reports to the file's
// end is unknowable here, so the unterminated pair is dropped rather than
// guessed — an analysis that invented an endpoint would be the kind of
// confident wrong answer the audit's T16 exists to prevent.
func parseSilenceRanges(output string) [][2]int {
	ranges := [][2]int{}
	pendingStart := -1
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "silence_start:") {
			if ms, ok := extractSecondField(line, "silence_start:"); ok {
				pendingStart = ms
			}
			continue
		}
		if strings.Contains(line, "silence_end:") && pendingStart >= 0 {
			if ms, ok := extractSecondField(line, "silence_end:"); ok {
				ranges = append(ranges, [2]int{pendingStart, ms})
				pendingStart = -1
			}
		}
	}
	return ranges
}

// extractSecondField finds `marker` in the line and parses the number after
// it as seconds-with-fraction, returning milliseconds.
func extractSecondField(line, marker string) (int, bool) {
	index := strings.Index(line, marker)
	if index < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(line[index+len(marker):])
	field := rest
	if space := strings.IndexAny(rest, " |"); space >= 0 {
		field = strings.TrimSpace(rest[:space])
	}
	seconds, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, false
	}
	return int(seconds * 1000), true
}
