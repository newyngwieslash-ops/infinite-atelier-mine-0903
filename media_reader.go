package main

import (
	"context"
	"strings"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// mediaReader is the `agenttools.MediaReader` adapter over the composed media stack.
//
// # Why the adapter lives here rather than in either package
//
// `agenttools` declares the port because the table's handlers are what call it, and the media
// services live in `application/media` and cannot import the tool table. The composition root is
// where this repository puts the seams between layers that must not know about each other — the same
// arrangement as `memory_bridge.go` and `media_wiring.go`.
//
// # Why an unattached reader refuses rather than returning nothing
//
// The media stack is composed AFTER the agent stack, because its timeline joins tables the drama
// stack owns and the tool table is built from the drama stack's services. So there is a window — and
// a build — in which the reader exists with no services behind it. The two tools' port allows that,
// and this adapter is where it is answered: `Capability` says the stack is not composed, and
// `Timeline` refuses rather than returning zero shots, which a model would read as "the episode has
// no shots" instead of "nothing was read".
type mediaReader struct {
	stack *mediaWiring
}

// Capability reports whether export is available and why not when it is not.
func (r mediaReader) Capability() (bool, string) {
	if r.stack == nil || r.stack.engine == nil {
		return false, "The media stack is not composed in this build."
	}
	if !r.stack.engine.Available() {
		// The engine's own sentence, passed through unchanged: it names the program to install, and
		// a report that paraphrased it would lose the one thing a person can act on.
		return false, r.stack.engine.Diagnostic()
	}
	return true, ""
}

// Timeline returns an episode's ordered shots.
func (r mediaReader) Timeline(ctx context.Context, episodeID, boardVersionID string) (appmedia.Timeline, error) {
	if r.stack == nil || r.stack.timeline == nil {
		return appmedia.Timeline{}, agent.UnavailableError()
	}
	return r.stack.timeline.Read(ctx, appmedia.TimelineRequest{
		EpisodeID:      strings.TrimSpace(episodeID),
		BoardVersionID: strings.TrimSpace(boardVersionID),
	})
}

// Compile-time proof that this satisfies the tool table's port.
//
// It is a real assertion against a locally declared interface rather than a function value: the first
// version of the analogous check in `internal/infrastructure/database` was `var _ = fn`, which
// compiles whatever the signature is, and an independent review caught it. The interface is declared
// here rather than naming `agenttools.MediaReader` directly, so the drift this catches is between
// THIS adapter and the shape the table needs — a field the table gained would fail here rather than
// at the call site, where the error would read as a wiring mistake.
type mediaReaderPort interface {
	Capability() (available bool, diagnostic string)
	Timeline(ctx context.Context, episodeID, boardVersionID string) (appmedia.Timeline, error)
}

var _ mediaReaderPort = mediaReader{}
