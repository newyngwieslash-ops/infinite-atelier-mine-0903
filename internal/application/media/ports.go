// Package media is the media pipeline's application layer: the engine port, the subtitle
// service, the export service and the timeline read model.
//
// # The boundary this package exists to hold
//
// DOMAIN_MODEL section 15.2's chain ends "… → Video/Audio → Timeline/Export", and the last two
// nodes are the only ones in it that are not artifacts a model writes: an export is a FILE the
// application composes out of other artifacts, using a program the user already has installed.
// That difference is why this package has its own vocabulary rather than reusing the pipeline's.
//
// # What it does not do
//
// It does not generate media. Video and audio generation are Jobs — AGENT_CONTRACTS section 19
// says so outright, and ADR-0011 section 6 ruled that they are deliberately not agent tools.
// This package CONSUMES what the job pipeline produced: it reads approved panel images and
// approved media versions, composes them, and hands the result to the user.
package media

import (
	"context"
	"time"
)

// SegmentKind says what a segment's file is.
type SegmentKind string

const (
	// SegmentImage is a still that lasts for the segment's duration.
	SegmentImage SegmentKind = "image"
	// SegmentVideo is a clip whose own length is used, so a duration of zero means "whatever the
	// file says".
	SegmentVideo SegmentKind = "video"
)

// IsValidSegmentKind reports whether a kind may be composed.
func IsValidSegmentKind(value SegmentKind) bool {
	return value == SegmentImage || value == SegmentVideo
}

// Segment is one piece of an export, in order.
//
// # Why one type covers both a frame and a clip
//
// ffmpeg's two paths differ by one flag (`-loop 1` before an image input, nothing before a
// video), and everything else about them is the same: an input, a duration, a place in the
// concat list. Two types would be two orderings, two validation rules and two places for the
// concat list to be built wrongly — so there is one, and `Kind` decides the flag.
//
// Path is a filesystem path the ADAPTER generated or the caller resolved from the file store.
// It is never a request field: ADR-0015 section 1 records that user text travels in a file
// rather than in argv, and this struct is the boundary where that rule is visible.
type Segment struct {
	Kind SegmentKind
	Path string
	// DurationMS is how long the segment lasts in the output. Required for an image — a still
	// with no duration is a frame nobody can see — and optional for a video, where zero means
	// the clip's own length.
	DurationMS int
	// Label names the segment in ffmpeg's error output, for a failure a reader can act on. It
	// is the shot identifier, not a filename.
	Label string
}

// Validate checks one segment before it is composed.
func (s Segment) Validate() error {
	if !IsValidSegmentKind(s.Kind) {
		return InvalidError("A segment must be a still or a clip.")
	}
	if s.Path == "" {
		return InvalidError("A segment must name the file it draws from.")
	}
	if s.Kind == SegmentImage && s.DurationMS <= 0 {
		return InvalidError("A still needs a duration, or nothing would be shown for it.")
	}
	if s.DurationMS < 0 {
		return InvalidError("A segment cannot have a negative duration.")
	}
	return nil
}

// ComposeRequest is one export to build.
type ComposeRequest struct {
	// Segments are the pieces, in the order they appear. The order is the CALLER's and is the
	// only statement of it: this package does not sort, because "the order" for a film is the
	// storyboard's shot order and a second opinion here would be a second answer.
	Segments []Segment
	// Width and Height are the output's frame size. Both are required: an export with no stated
	// size would take its size from whichever segment happened to be first, which is a decision
	// nobody made.
	Width  int
	Height int
	// FPS is the output frame rate.
	FPS int
	// AudioPaths are audio files mixed over the whole export, in order. Empty means a silent
	// film, which is legal and is what an episode with no TTS yet produces.
	AudioPaths []string
	// SubtitlePath is an SRT or VTT file to mux in, or empty for none.
	SubtitlePath string
	// SubtitleMode is "sidecar", "burn" or "none". A sidecar track is the default when subtitles
	// ARE named, because it is reversible: a burned-in subtitle is part of the picture and cannot
	// be turned off.
	//
	// The ZERO VALUE is none, and that is deliberate rather than an oversight. Most exports have no
	// subtitles — the field is empty and the export is a silent picture — so a caller that says
	// nothing about subtitles should get a film without them, not a refusal about a mode it never
	// chose. The first version of this type made the zero value invalid, and every caller had to
	// restate a default that was obviously "none".
	SubtitleMode SubtitleMode
	// OutputPath is where the adapter writes. It comes from the caller, which got it from the
	// application's own temporary directory — never from a request. ADR-0015 section 1.
	OutputPath string
	// Timeout bounds the whole composition. Zero uses DefaultComposeTimeout.
	Timeout time.Duration
}

// SubtitleMode is how a subtitle track travels into the export.
type SubtitleMode string

const (
	// SubtitleNone exports without subtitles. It is the ZERO VALUE, so an empty field means this.
	SubtitleNone SubtitleMode = ""
	// SubtitleSidecar muxes the subtitles as a stream the player can turn off.
	SubtitleSidecar SubtitleMode = "sidecar"
	// SubtitleBurn draws them into the picture.
	SubtitleBurn SubtitleMode = "burn"
)

// SubtitleModes lists the documented modes in the schema's order.
var SubtitleModes = []SubtitleMode{SubtitleNone, SubtitleSidecar, SubtitleBurn}

// IsValidSubtitleMode reports whether a mode may be requested.
func IsValidSubtitleMode(value SubtitleMode) bool {
	for _, candidate := range SubtitleModes {
		if candidate == value {
			return true
		}
	}
	return false
}

// ComposeResult is what a composition produced.
type ComposeResult struct {
	OutputPath string
	// DurationMS is what the OUTPUT says it is, read back with ffprobe — not what the segments
	// asked for. The difference is the point: a concatenation that dropped a segment, or a still
	// whose duration was rounded, shows up here rather than in a frame count nobody checks.
	DurationMS int
	Width      int
	Height     int
	SizeBytes  int64
}

// MediaInfo is what a probe reports about one file.
type MediaInfo struct {
	MIMEType   string
	DurationMS int
	Width      int
	Height     int
	// Streams counts input streams, which is section 8.4's "限制…流数量" made checkable: a file
	// claiming to be a one-second clip and carrying forty streams is not one.
	Streams   int
	SizeBytes int64
}

// ProbeLimits bounds what an input may be, before anything decodes it.
//
// SECURITY section 8.4 asks for exactly this order: "先 probe，限制时长/分辨率/流数量". The
// numbers are generous for an episode and small enough that a hostile file is refused rather
// than processed.
type ProbeLimits struct {
	MaxDurationMS int
	MaxWidth      int
	MaxHeight     int
	MaxStreams    int
	MaxBytes      int64
}

// DefaultProbeLimits is what an export accepts without a caller's own limits.
func DefaultProbeLimits() ProbeLimits {
	return ProbeLimits{
		// An hour: longer than any episode this build produces and far shorter than the
		// decompression bomb a hostile file aims at.
		MaxDurationMS: 60 * 60 * 1000,
		MaxWidth:      7680,
		MaxHeight:     4320,
		MaxStreams:    8,
		MaxBytes:      2 << 30,
	}
}

// MediaEngine turns stored files into one composed file.
//
// It is a PORT rather than a direct call because the implementation starts a subprocess, and a
// port is what keeps that in one audited place: ADR-0015 section 1, and SECURITY section 5's
// "允许 `os/exec` 的唯一位置是经过审计的 MediaEngine/系统集成 Adapter".
//
// Every method takes a context. An export of a real episode takes minutes, and the user who
// cancels it must be able to.
type MediaEngine interface {
	// Available reports whether the engine can run at all: an ffmpeg binary was found.
	//
	// It is a separate question from "does this call succeed", because the answer is about the
	// MACHINE rather than about the request — ARCHITECTURE's "媒体引擎不可用：禁用相关能力并显示
	// 诊断". A build with no ffmpeg must disable export and say so, not fail an export.
	Available() bool

	// Diagnostic explains why the engine is unavailable, for the UI's message. Empty when it is
	// available.
	Diagnostic() string

	// Probe reads what a file is, refusing anything outside the limits.
	Probe(ctx context.Context, path string, limits ProbeLimits) (MediaInfo, error)

	// Compose builds the output.
	Compose(ctx context.Context, request ComposeRequest) (ComposeResult, error)

	// Version names the engine and its version, for the export manifest. It is part of the
	// manifest because a film's reproducibility depends on what produced it, exactly as a
	// generation's depends on the model that produced it.
	Version(ctx context.Context) string
}
