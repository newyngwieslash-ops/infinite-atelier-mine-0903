package main

import (
	"context"
	"io"
	"os"
	"time"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/media"
)

// content_analyzer_adapter.go is RP-07.2's composition: the FFmpeg engine's
// AnalyzeContent becomes the consistency package's ContentAnalyzer port.
//
// # Why an adapter, and what it owns
//
// The ruleset speaks hashes and findings; the engine speaks paths and
// processes. This adapter is the only place that translates: it resolves a
// managed hash to bytes through the file service, writes them to a SCRATCH
// file it owns, runs the bounded analysis, and removes the file — a cleanup
// that runs on every exit path, so an analysis cannot leak temp files.
//
// # The deadline
//
// The adapter's ONE deadline covers materialise + analyse together, which is
// the plan's "multiple subprocesses sharing a total deadline" rule: two
// per-step timeouts added together would make a hostile file's budget twice
// what the configuration says.
type contentAnalyzerAdapter struct {
	store    *filestore.Store
	engine   *media.FFmpegEngine
	scratch  string
	timeout  time.Duration
	deadline func() time.Time
}

// newContentAnalyzer builds the port adapter, or nil when the engine or the
// store is absent — the composition's honest "no content analysis here".
func newContentAnalyzer(store *filestore.Store, engine *media.FFmpegEngine, scratch string, timeout time.Duration) appconsistency.ContentAnalyzer {
	if store == nil || engine == nil || !engine.Available() {
		return nil
	}
	return &contentAnalyzerAdapter{store: store, engine: engine, scratch: scratch, timeout: timeout}
}

// AnalyzeFile implements appconsistency.ContentAnalyzer.
func (a *contentAnalyzerAdapter) AnalyzeFile(ctx context.Context, fileHash string) (appconsistency.ContentReport, bool, error) {
	if a == nil || a.store == nil || a.engine == nil {
		return appconsistency.ContentReport{}, false, nil
	}
	deadline := time.Now().Add(a.timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	source, err := a.store.Open(ctx, fileHash)
	if err != nil {
		// A hash with no bytes is the "file missing" state the review
		// already reports through its own rule; the adapter answers
		// not-found rather than a process error.
		return appconsistency.ContentReport{}, false, nil
	}
	defer source.Close()

	if err := os.MkdirAll(a.scratch, 0o700); err != nil {
		return appconsistency.ContentReport{}, false, err
	}
	temp, err := os.CreateTemp(a.scratch, "analyze-*.bin")
	if err != nil {
		return appconsistency.ContentReport{}, false, err
	}
	tempPath := temp.Name()
	defer func() {
		temp.Close()
		_ = os.Remove(tempPath)
	}()
	if _, err := io.Copy(temp, source); err != nil {
		return appconsistency.ContentReport{}, false, err
	}
	if err := temp.Close(); err != nil {
		return appconsistency.ContentReport{}, false, err
	}

	analysis, err := a.engine.AnalyzeContent(ctx, tempPath, a.timeout)
	if err != nil {
		// An analysis failure on a real file is an error the review reports
		// as a per-file finding; cancel is not an error worth a finding.
		if ctx.Err() != nil {
			return appconsistency.ContentReport{}, false, nil
		}
		return appconsistency.ContentReport{}, false, err
	}
	return appconsistency.ContentReport{
		BlackRanges:  analysis.BlackRanges,
		SilentRanges: analysis.SilentRanges,
	}, true, nil
}

// compile-time proof the adapter satisfies the port.
var _ appconsistency.ContentAnalyzer = (*contentAnalyzerAdapter)(nil)
