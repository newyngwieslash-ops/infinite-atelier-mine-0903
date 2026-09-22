// Package media hosts the MediaEngine implementation: the ONE place in this repository that
// starts a subprocess.
//
// # Read this before changing anything here
//
// SECURITY section 5 says: "允许 `os/exec` 的唯一位置是经过审计的 MediaEngine/系统集成 Adapter，
// 参数必须结构化构造，禁止 Shell 字符串拼接." Section 19 lists "媒体命令通过 Shell 字符串拼接" as
// a release blocker. ADR-0015 section 1 records the ruling and its cost.
//
// Four properties keep that permission narrow, and every one of them is a thing a later change
// could break:
//
//  1. THIS IS THE ONLY FILE THAT IMPORTS `os/exec`, and `scripts/security-scan.mjs` fails the
//     build on a second one. The scanner's allowlist entry names this file, the rule and
//     ADR-0015, refuses wildcards, and treats a stale entry as a failure — so removing this file
//     without removing the entry also fails.
//  2. EVERY COMMAND IS STRUCTURED. `exec.CommandContext(ctx, path, args...)` where args is a
//     []string. Nothing is ever joined, quoted or interpolated into a command line, and no shell
//     is involved: there is no shell and there cannot be, because the command name is one of
//     two constants this file resolved itself.
//  3. USER TEXT NEVER REACHES ARGV. A prompt, a filename or a caption goes into a FILE this
//     adapter writes, and the file's own generated path is what travels as an argument. Where a
//     tool takes a path inside a filter expression — the `subtitles` filter is the one — the path
//     is escaped by a function here rather than trusted, because ffmpeg's filtergraph parser has
//     its own metacharacters and a Windows drive letter contains one of them.
//  4. IT FAILS SOFT. No ffmpeg means the engine reports itself unavailable with a diagnostic and
//     export is disabled. ARCHITECTURE: "媒体引擎不可用：禁用相关能力并显示诊断".
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
)

// The two programs this adapter can run, by name.
//
// They are CONSTANTS rather than configuration: a path from a settings field or a request would
// be a way to run an arbitrary program, and SECURITY section 11's "文件创建使用 exclusive" is about
// files rather than about executables but the reasoning is the same. PRD FR-180 lists an "FFmpeg
// 路径" setting; this build deliberately does not offer one, and STATUS section 0l records that.
const (
	ffmpegBinary  = "ffmpeg"
	ffprobeBinary = "ffprobe"
)

// Bounds. Each one exists because the thing it bounds is reachable from a file a user or a
// provider supplied.
const (
	// DefaultComposeTimeout bounds one composition. An episode's export is minutes rather than
	// seconds, and a file that would take longer is not one this build accepts.
	DefaultComposeTimeout = 20 * time.Minute
	// MaxEngineOutputBytes bounds what the adapter reads from the engine's own streams. ffmpeg on
	// a malformed input can emit megabytes of diagnostics, and this adapter reads them into
	// memory to report a failure.
	MaxEngineOutputBytes = 256 * 1024
	// probeTimeout bounds one ffprobe call. It reads metadata rather than decoding, so it is
	// quick or it is broken.
	probeTimeout = 30 * time.Second
)

// FFmpegEngine is the MediaEngine over a local ffmpeg.
type FFmpegEngine struct {
	// ffmpegPath and ffprobePath are resolved ONCE, by LookPath, at construction. An empty path
	// means the program is not installed.
	ffmpegPath  string
	ffprobePath string
	// tempDir is where intermediates are written. It is the application's own temporary
	// directory, passed in rather than discovered, and ADR-0015 section 1 records why it is not a
	// request field.
	tempDir string
	// version is read once, lazily, because it costs a subprocess and the manifest is the only
	// caller.
	versionOnce sync.Once
	version     string
}

// NewFFmpegEngine resolves the programs and builds the engine.
//
// It never fails: a machine without ffmpeg is a valid machine, and the engine reports itself
// unavailable rather than refusing to be constructed. That is what lets the composition root
// build the whole stack and the UI disable one button.
func NewFFmpegEngine(tempDir string) *FFmpegEngine {
	engine := &FFmpegEngine{tempDir: tempDir}
	if resolved, err := exec.LookPath(ffmpegBinary); err == nil {
		engine.ffmpegPath = resolved
	}
	if resolved, err := exec.LookPath(ffprobeBinary); err == nil {
		engine.ffprobePath = resolved
	}
	return engine
}

// Available reports whether both programs were found.
//
// Both, not either: composition needs ffmpeg and every read-back needs ffprobe, and an engine
// that could write a file it cannot verify would be one that reports a duration it never checked.
func (e *FFmpegEngine) Available() bool {
	return e != nil && e.ffmpegPath != "" && e.ffprobePath != "" && e.tempDir != ""
}

// Diagnostic explains why the engine is unavailable.
func (e *FFmpegEngine) Diagnostic() string {
	if e == nil {
		return "The media engine is not configured."
	}
	missing := []string{}
	if e.ffmpegPath == "" {
		missing = append(missing, ffmpegBinary)
	}
	if e.ffprobePath == "" {
		missing = append(missing, ffprobeBinary)
	}
	if len(missing) > 0 {
		return "Export needs " + strings.Join(missing, " and ") + " on this machine's PATH. " +
			"Install FFmpeg and restart the application."
	}
	if e.tempDir == "" {
		return "The media engine has no temporary directory to work in."
	}
	return ""
}

// Version names the engine and its version, for the export manifest.
//
// A failure to read the version is not a failure of the export: the manifest records that the
// version was unknown rather than refusing to write one, because a film that exists with an
// incomplete provenance is better than no film.
func (e *FFmpegEngine) Version(ctx context.Context) string {
	if !e.Available() {
		return ""
	}
	e.versionOnce.Do(func() {
		output, err := e.run(ctx, e.ffmpegPath, []string{"-version"}, probeTimeout)
		if err != nil {
			return
		}
		// The first line is "ffmpeg version 5.1.1-essentials_build-..."; the manifest wants the
		// name and the number rather than the build string.
		first := strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0]
		fields := strings.Fields(first)
		if len(fields) >= 3 {
			e.version = fields[0] + " " + fields[2]
			return
		}
		e.version = first
	})
	return e.version
}

// Probe reads what a file is, and refuses it if it is outside the limits.
//
// The order is SECURITY section 8.4's: probe first, then bound. A file is measured before
// anything decodes it, so a decompression bomb is a size rather than a process that ran.
func (e *FFmpegEngine) Probe(ctx context.Context, path string, limits appmedia.ProbeLimits) (appmedia.MediaInfo, error) {
	if !e.Available() {
		return appmedia.MediaInfo{}, appmedia.NotAvailableError(e.Diagnostic())
	}
	if err := checkPathArgument(path); err != nil {
		return appmedia.MediaInfo{}, err
	}
	if limits.MaxDurationMS <= 0 {
		limits = appmedia.DefaultProbeLimits()
	}
	// The arguments are the probe's own, all constants except the path — which was checked
	// above and is one argv element.
	arguments := []string{
		"-v", "error",
		"-show_entries", "format=duration,format_name,size",
		"-show_entries", "stream=codec_type,width,height",
		"-of", "default=noprint_wrappers=1",
		"--", path,
	}
	output, err := e.run(ctx, e.ffprobePath, arguments, probeTimeout)
	if err != nil {
		return appmedia.MediaInfo{}, appmedia.ComposeError("That file could not be read as media.", err)
	}
	info := parseProbeOutput(string(output))
	if stat, err := os.Stat(path); err == nil {
		info.SizeBytes = stat.Size()
	}
	if info.DurationMS > limits.MaxDurationMS {
		return appmedia.MediaInfo{}, appmedia.LimitError("That file is longer than an export accepts.")
	}
	if info.Width > limits.MaxWidth || info.Height > limits.MaxHeight {
		return appmedia.MediaInfo{}, appmedia.LimitError("That file is larger than an export accepts.")
	}
	if info.Streams > limits.MaxStreams {
		return appmedia.MediaInfo{}, appmedia.LimitError("That file carries more streams than an export accepts.")
	}
	if info.SizeBytes > limits.MaxBytes {
		return appmedia.MediaInfo{}, appmedia.LimitError("That file is bigger than an export accepts.")
	}
	return info, nil
}

// Compose builds the output.
//
// # Why it is several ffmpeg calls rather than one command with a filtergraph
//
// A single invocation would need a filter_complex string assembling every input, its scaling, its
// duration and its place in a concat — and that string is where user text would have to be
// interpolated, because a filtergraph cannot take separate arguments. Three simple passes need no
// such string: each input is normalised on its own into an intermediate file whose name THIS
// adapter chose, and the concat list is a file whose contents are paths it chose too.
//
// The cost is re-encoding, which the concat demuxer requires anyway — it accepts only inputs with
// identical codecs and parameters, and a project's frames and clips have neither in common.
func (e *FFmpegEngine) Compose(ctx context.Context, request appmedia.ComposeRequest) (appmedia.ComposeResult, error) {
	if !e.Available() {
		return appmedia.ComposeResult{}, appmedia.NotAvailableError(e.Diagnostic())
	}
	if err := validateCompose(request); err != nil {
		return appmedia.ComposeResult{}, err
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = DefaultComposeTimeout
	}
	composeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The scratch directory is this call's own, under a prefix no other part of the application
	// uses: the FileStore's own temp files are named `put-*` and share the parent, so a prefix is
	// what keeps a crash from confusing the two. It is removed on every path out.
	scratch, err := os.MkdirTemp(e.tempDir, "export-")
	if err != nil {
		return appmedia.ComposeResult{}, appmedia.StorageError("The export could not start.", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	// Pass one: one normalised clip per segment.
	normalised := make([]string, 0, len(request.Segments))
	for index, segment := range request.Segments {
		if err := checkPathArgument(segment.Path); err != nil {
			return appmedia.ComposeResult{}, err
		}
		clipPath := filepath.Join(scratch, "segment-"+strconv.Itoa(index)+".mp4")
		if err := e.normaliseSegment(composeCtx, segment, request, clipPath); err != nil {
			return appmedia.ComposeResult{}, err
		}
		normalised = append(normalised, clipPath)
	}
	// Pass two: the concat list, which is a FILE rather than an argument. Its contents are the
	// paths this adapter generated, escaped by the demuxer's own rule.
	listPath := filepath.Join(scratch, "segments.txt")
	if err := os.WriteFile(listPath, []byte(concatList(normalised)), 0o600); err != nil {
		return appmedia.ComposeResult{}, appmedia.StorageError("The export could not be assembled.", err)
	}
	joined := filepath.Join(scratch, "joined.mp4")
	joinArgs := []string{"-hide_banner", "-y", "-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy", "--", joined}
	if _, err := e.run(composeCtx, e.ffmpegPath, joinArgs, timeout); err != nil {
		if composeCtx.Err() != nil {
			return appmedia.ComposeResult{}, err
		}
		return appmedia.ComposeResult{}, appmedia.ComposeError("The export could not be assembled.", err)
	}

	// Pass three: the audio and the subtitles, on top of the joined picture. When there are
	// neither, this pass is skipped rather than run with nothing to do — an extra invocation is an
	// extra place to fail.
	finalPath := request.OutputPath
	if len(request.AudioPaths) > 0 || request.SubtitlePath != "" {
		if err := e.finishExport(composeCtx, joined, request, scratch, finalPath, timeout); err != nil {
			return appmedia.ComposeResult{}, err
		}
	} else if err := os.Rename(joined, finalPath); err != nil {
		// A cross-device rename cannot happen here — both paths are under this call's scratch and
		// the caller's output, which are the same volume — so a failure is a real one.
		return appmedia.ComposeResult{}, appmedia.StorageError("The export could not be written.", err)
	}

	// Read back what was produced rather than trusting what was asked for: a composition that
	// dropped a segment, or a still whose duration rounded, shows up in this measurement.
	result := appmedia.ComposeResult{OutputPath: finalPath}
	if info, err := e.Probe(composeCtx, finalPath, appmedia.DefaultProbeLimits()); err == nil {
		result.DurationMS = info.DurationMS
		result.Width = info.Width
		result.Height = info.Height
		result.SizeBytes = info.SizeBytes
	}
	return result, nil
}

// normaliseSegment renders one segment as a clip with the output's own parameters.
func (e *FFmpegEngine) normaliseSegment(ctx context.Context, segment appmedia.Segment, request appmedia.ComposeRequest, output string) error {
	// A CANCELLATION IS NOT A FAILURE of this segment, and reporting it as one would send a reader
	// looking at the file rather than at the button they pressed. The check is here rather than only
	// in run, because the wrapping below would otherwise replace the cancellation's own message.
	// The scale filter PADS rather than crops, so a frame whose aspect ratio differs from the
	// output's is letterboxed instead of losing its edges — and the pad expression is constants,
	// because the size came from the caller and was validated as two integers.
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
		request.Width, request.Height, request.Width, request.Height)
	arguments := []string{"-hide_banner", "-y"}
	if segment.Kind == appmedia.SegmentImage {
		// A still is looped for its own duration, which is why validation requires one.
		arguments = append(arguments, "-loop", "1", "-i", segment.Path,
			"-t", seconds(segment.DurationMS))
	} else {
		arguments = append(arguments, "-i", segment.Path)
		if segment.DurationMS > 0 {
			arguments = append(arguments, "-t", seconds(segment.DurationMS))
		}
	}
	arguments = append(arguments,
		"-vf", filter,
		"-r", strconv.Itoa(request.FPS),
		"-an",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"--", output)
	label := segment.Label
	if label == "" {
		label = filepath.Base(segment.Path)
	}
	if _, err := e.run(ctx, e.ffmpegPath, arguments, DefaultComposeTimeout); err != nil {
		if ctx.Err() != nil {
			return err
		}
		return appmedia.ComposeError("The segment "+label+" could not be rendered.", err)
	}
	return nil
}

// finishExport adds the audio and the subtitles to the joined picture.
func (e *FFmpegEngine) finishExport(ctx context.Context, joined string, request appmedia.ComposeRequest, scratch, output string, timeout time.Duration) error {
	arguments := []string{"-hide_banner", "-y", "-i", joined}
	for _, audioPath := range request.AudioPaths {
		if err := checkPathArgument(audioPath); err != nil {
			return err
		}
		arguments = append(arguments, "-i", audioPath)
	}
	burning := request.SubtitlePath != "" && request.SubtitleMode == appmedia.SubtitleBurn
	// A named subtitle file with no mode is a SIDECAR, which is the reversible default: a caller
	// that attached subtitles and said nothing about how got them as a track it can turn off.
	sidecar := request.SubtitlePath != "" && !burning
	if request.SubtitlePath != "" {
		if err := checkPathArgument(request.SubtitlePath); err != nil {
			return err
		}
		arguments = append(arguments, "-i", request.SubtitlePath)
	}
	// The codecs are copied for the picture, because it was encoded in the pass before and a
	// second encode would lose quality for nothing.
	arguments = append(arguments, "-c:v", "copy")

	if len(request.AudioPaths) > 0 {
		// One audio file is mapped directly; several are concatenated in order, which is what a
		// timeline of dialogue means before any offset work exists. FR-080 puts offsets and
		// mixing in V1, and doing it here would need a filtergraph — the shape this adapter avoids.
		if len(request.AudioPaths) == 1 {
			// BOTH streams are mapped explicitly. `-map 1:a:0` alone REPLACES the automatic
			// selection rather than adding to it, so the picture disappeared and the export became
			// an audio file — which the composition test caught as one stream where two were
			// expected. Naming the video as well is what makes "add a soundtrack" mean that.
			arguments = append(arguments, "-map", "0:v:0", "-map", "1:a:0", "-c:a", "aac", "-shortest")
		} else {
			inputs := make([]string, 0, len(request.AudioPaths))
			for index := range request.AudioPaths {
				inputs = append(inputs, "["+strconv.Itoa(index+1)+":a:0]")
			}
			// The filter is built from INPUT INDICES and constants only: no path, no user text.
			graph := strings.Join(inputs, "") + "concat=n=" + strconv.Itoa(len(inputs)) + ":v=0:a=1[a]"
			// The picture is mapped alongside the mixed audio, for the reason the single-file branch
			// states: a filter's output does not include the video, so naming only [a] would produce
			// the same audio-only file.
			arguments = append(arguments, "-filter_complex", graph,
				"-map", "0:v:0", "-map", "[a]", "-c:a", "aac", "-shortest")
		}
	}
	switch {
	case burning:
		// A burned-in subtitle is drawn into the picture, which means re-encoding it.
		//
		// THE ONE PLACE A PATH ENTERS A FILTER EXPRESSION, and therefore the one place escaping
		// matters: ffmpeg's filtergraph parser treats `:` as an option separator and `\` as its own
		// escape, and a Windows drive letter contains a colon. escapeFilterPath is what makes this
		// safe, and the file it names is one this application wrote.
		arguments = arguments[:len(arguments)-2] // drop the `-c:v copy`: the picture is re-encoded
		arguments = append(arguments,
			"-vf", "subtitles="+escapeFilterPath(request.SubtitlePath),
			"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p")
	case sidecar:
		arguments = append(arguments, "-c:s", "mov_text")
	}
	arguments = append(arguments, "--", output)
	if _, err := e.run(ctx, e.ffmpegPath, arguments, timeout); err != nil {
		if ctx.Err() != nil {
			return err
		}
		return appmedia.ComposeError("The export could not be finished.", err)
	}
	return nil
}

// run executes one engine call and returns its standard output.
//
// It is the ONLY place a process starts. Three things happen here and nowhere else: the path is
// checked to be one of the two resolved programs is NOT done here — the caller passes the field —
// the context carries the deadline, and both streams are bounded.
func (e *FFmpegEngine) run(ctx context.Context, program string, args []string, timeout time.Duration) ([]byte, error) {
	if program == "" {
		return nil, appmedia.NotAvailableError(e.Diagnostic())
	}
	for _, argument := range args {
		// A NUL cannot appear in an argv element on any platform, and its presence means a caller
		// built a string rather than passing a value.
		if strings.ContainsRune(argument, 0) {
			return nil, appmedia.InvalidError("A media argument contained an invalid character.")
		}
	}
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	// exec.CommandContext over a []string. No shell, no quoting, no joining: the operating system
	// receives the arguments as separate values, which is the property SECURITY section 5 is
	// about. A metacharacter in an argument is data here, and stops being data the moment someone
	// replaces this call with a shell invocation or joins the arguments into one string.
	command := exec.CommandContext(runCtx, program, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &limitedWriter{buffer: &stdout, limit: MaxEngineOutputBytes}
	command.Stderr = &limitedWriter{buffer: &stderr, limit: MaxEngineOutputBytes}
	command.Stdin = nil
	// A process that outlives its context is killed rather than waited for, so a cancelled export
	// stops rather than holding the file it was writing.
	command.WaitDelay = 5 * time.Second

	if err := command.Run(); err != nil {
		if runCtx.Err() != nil {
			return nil, appmedia.ComposeError("The export was cancelled.", runCtx.Err())
		}
		// The engine's own diagnostics travel as the CAUSE rather than as the message: an ffmpeg
		// error line can quote a path, and the message is what a user reads.
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, appmedia.ComposeError("The media engine refused the request.", errors.New(detail))
	}
	return stdout.Bytes(), nil
}

// limitedWriter keeps at most limit bytes and discards the rest.
//
// An engine reading a malformed file can emit megabytes of diagnostics, and this adapter buffers
// them to report a failure. Writing past the limit is not an error — the process must keep running
// so it can be waited for — but nothing past it is kept.
type limitedWriter struct {
	buffer *bytes.Buffer
	limit  int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			w.buffer.Write(p)
		} else {
			w.buffer.Write(p[:remaining])
		}
	}
	// The full length is reported: a short write would make the engine see a broken pipe and fail
	// for a reason that is this adapter's, not the file's.
	return len(p), nil
}

// checkPathArgument refuses a path this adapter will not pass as an argument.
//
// A leading `-` is THE injection shape in a program that parses its own argv: `-i` followed by a
// value is an option, so a "filename" of `-y` or a longer crafted string would be read as one. Every
// path here was produced by this application or resolved from the content-addressed store, so a
// leading dash means something upstream is wrong and the answer is to refuse rather than to
// sanitise — a path that needed rewriting is a path whose origin is not what this adapter thinks.
func checkPathArgument(path string) error {
	if strings.TrimSpace(path) == "" {
		return appmedia.InvalidError("A media path cannot be empty.")
	}
	if strings.HasPrefix(path, "-") {
		return appmedia.InvalidError("A media path cannot begin with a dash.")
	}
	if strings.ContainsRune(path, 0) {
		return appmedia.InvalidError("A media path contained an invalid character.")
	}
	return nil
}

// escapeFilterPath escapes a path for use inside an ffmpeg filtergraph expression.
//
// The filtergraph parser is a second language with its own metacharacters, and a path that is a
// perfectly good argv element can still be a broken or dangerous expression there. The rules this
// applies are the parser's: a backslash is the escape character, a colon separates options, and a
// single quote delimits a quoted section. The order matters — the backslash first, or the escapes
// added after it would themselves be escaped.
//
// It exists because a Windows drive letter contains a colon, so the ONE path that reaches a
// filtergraph in this adapter would break without it.
func escapeFilterPath(path string) string {
	replaced := strings.ReplaceAll(path, "\\", "/")
	replaced = strings.ReplaceAll(replaced, ":", "\\:")
	replaced = strings.ReplaceAll(replaced, "'", "\\'")
	replaced = strings.ReplaceAll(replaced, "[", "\\[")
	replaced = strings.ReplaceAll(replaced, "]", "\\]")
	return replaced
}

// validateCompose checks a request before any process starts.
func validateCompose(request appmedia.ComposeRequest) error {
	if len(request.Segments) == 0 {
		return appmedia.InvalidError("An export needs at least one segment.")
	}
	for _, segment := range request.Segments {
		if err := segment.Validate(); err != nil {
			return err
		}
	}
	if request.Width <= 0 || request.Height <= 0 {
		return appmedia.InvalidError("An export must state its frame size.")
	}
	// Even dimensions, because H.264 encodes in 2x2 blocks and an odd size fails at the encoder
	// rather than at validation — which would surface as a media error about a file rather than
	// about the number that caused it.
	if request.Width%2 != 0 || request.Height%2 != 0 {
		return appmedia.InvalidError("An export's frame size must be even in both directions.")
	}
	if request.FPS <= 0 || request.FPS > 120 {
		return appmedia.InvalidError("An export must state a plausible frame rate.")
	}
	if !appmedia.IsValidSubtitleMode(request.SubtitleMode) {
		return appmedia.InvalidError("The subtitle mode is not recognised.")
	}
	if request.SubtitlePath != "" && request.SubtitleMode == appmedia.SubtitleNone {
		return appmedia.InvalidError("An export names subtitles and asks for none.")
	}
	if strings.TrimSpace(request.OutputPath) == "" {
		return appmedia.InvalidError("An export must name its output file.")
	}
	return nil
}

// concatList renders the demuxer's list file.
//
// The format is one `file '<path>'` line per input, and the demuxer's own escaping is a single
// quote inside a quoted path written as `'\”`. The paths are this adapter's own generated
// intermediates, so the escaping is belt-and-braces rather than load-bearing — but it is the
// demuxer's rule and stating it here is cheaper than discovering it.
func concatList(paths []string) string {
	var builder strings.Builder
	for _, path := range paths {
		builder.WriteString("file '")
		builder.WriteString(strings.ReplaceAll(path, "'", `'\''`))
		builder.WriteString("'\n")
	}
	return builder.String()
}

// seconds renders milliseconds as the decimal seconds ffmpeg's `-t` takes.
func seconds(milliseconds int) string {
	return strconv.FormatFloat(float64(milliseconds)/1000, 'f', 3, 64)
}

// parseProbeOutput reads ffprobe's `default=noprint_wrappers=1` output.
//
// The format is one `key=value` per line with no nesting, which is why the probe asks for it: a
// JSON document would be a second parser here for four numbers.
func parseProbeOutput(output string) appmedia.MediaInfo {
	var info appmedia.MediaInfo
	seenVideo := false
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "duration":
			if seconds, err := strconv.ParseFloat(value, 64); err == nil {
				info.DurationMS = int(seconds * 1000)
			}
		case "format_name":
			info.MIMEType = value
		case "size":
			if size, err := strconv.ParseInt(value, 10, 64); err == nil {
				info.SizeBytes = size
			}
		case "codec_type":
			// Every stream reports one, so this counts streams: section 8.4's "限制…流数量".
			info.Streams++
			if value == "video" && !seenVideo {
				seenVideo = true
			}
		case "width":
			if width, err := strconv.Atoi(value); err == nil && seenVideo && info.Width == 0 {
				info.Width = width
			}
		case "height":
			if height, err := strconv.Atoi(value); err == nil && seenVideo && info.Height == 0 {
				info.Height = height
			}
		}
	}
	return info
}

// The compile-time proof that this satisfies the port.
//
// It is against the application's interface rather than a local copy, so a signature drift fails
// the build — the shape WP-10's review found missing in three other places.
var _ appmedia.MediaEngine = (*FFmpegEngine)(nil)
