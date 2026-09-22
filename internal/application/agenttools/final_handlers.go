package agenttools

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// final_handlers.go holds the two tools WP-11's `final_episode` agent reads with: what this machine
// can compose, and what the episode's timeline holds.
//
// # Why these are tools rather than the stage's own state
//
// The state layer of a prompt carries what the STAGE needs to know to run — its attempt, its
// artifact, the identifiers its write tools cite. These two answers are different in kind: they are
// facts about the MACHINE and about the assembled film, they are large, and a stage that read them
// from the prompt would carry a timeline of every shot into every attempt including the ones that
// never look at it.
//
// # Why the capability is a READ and not an error
//
// AGENT_CONTRACTS section 11.4's first clause is "所有必需 Shot 有批准视频", and a recipe written on a
// machine that cannot compose is a document nobody can execute. The capability tool is what lets the
// agent say so in its own report — "this machine has no ffmpeg, so here is what to install" — rather
// than producing a recipe and letting the export fail later with a message about a missing program.
// ARCHITECTURE's "媒体引擎不可用：禁用相关能力并显示诊断" is the same rule stated for the UI.

// MediaReader answers what the media stack can do and what an episode's timeline holds.
//
// It is a port rather than the two concrete services because this package needs three answers and
// nothing else, and because the composition root composes the media stack separately from the tool
// table: a narrower dependency is what lets the table be built before the media wiring exists, with
// the two tools refusing rather than the whole stack failing to compose.
type MediaReader interface {
	// Capability reports whether export is available and why not when it is not.
	Capability() (available bool, diagnostic string)
	// Timeline returns an episode's ordered shots with the media, audio and cues each carries.
	Timeline(ctx context.Context, episodeID, boardVersionID string) (appmedia.Timeline, error)
}

// Deps.Media is OPTIONAL, and the two tools below are what say so.
//
// Every other field of `Deps` is checked by `Available`, because a tool whose service is missing
// would be a handler that panics or invents. These two refuse instead, and the reason they may is
// that the media stack is composed LATER than the tool table in the composition root — the media
// stack needs the drama stack's script service, and the tool table needs the drama stack's five. A
// required field would make the two orders circular.
//
// The refusal is a sentence naming what is missing rather than an empty answer, which is the same
// shape `asset.read_gap_report` uses for an episode with no approved report: a caller must not be
// able to read "nothing" and conclude "nothing is wrong".

// bindReadMediaCapability reports what this machine can compose.
func bindReadMediaCapability(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		// No arguments: the answer is about the machine and the run, and a model that could name
		// another target would be asking about a machine it is not running on.
		if deps.Media == nil {
			return map[string]any{
				"available":  false,
				"diagnostic": "The media stack is not composed in this build.",
			}, nil
		}
		available, diagnostic := deps.Media.Capability()
		answer := map[string]any{"available": available}
		if diagnostic != "" {
			answer["diagnostic"] = diagnostic
		}
		return answer, nil
	}
}

// bindReadTimeline returns an episode's ordered shots, which is what a final recipe is written from.
//
// THE GAP IT CLOSES is the one the final stage would otherwise have: the board carries the rows and
// the durations, but not which shot has approved media, which has audio, or how many cues fall
// inside it. An agent asked to state an export recipe without those would be writing parameters for
// a film it could not see assembled — and the deterministic ruleset would then report the missing
// media it could have read.
func bindReadTimeline(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID      string `json:"episodeId"`
			BoardVersionID string `json:"boardVersionId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		if deps.Media == nil {
			return nil, agent.UnavailableError()
		}
		// The episode comes from the ARGUMENTS here rather than from the run, which is the opposite
		// of what every other tool does — and it is what the scope check below is for. A final stage
		// is asked about an episode that a caller named, and the check is that the named episode
		// belongs to the run's own project.
		episodeID := strings.TrimSpace(arguments.EpisodeID)
		if episodeID == "" {
			// An empty identifier falls back to the run's episode, which is the ordinary case: a
			// stage's own attempt carries the episode it is about.
			episodeID = strings.TrimSpace(request.EpisodeID)
		}
		if episodeID == "" {
			return nil, agent.InvalidError("A timeline read must name an episode.")
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		timeline, err := deps.Media.Timeline(ctx, episodeID, strings.TrimSpace(arguments.BoardVersionID))
		if err != nil {
			return nil, err
		}
		return timelineAnswer(timeline), nil
	}
}

// timelineAnswer renders a timeline for a model.
//
// It is a document rather than the struct, so the field names a model reads are the ones this
// function states: the domain's own names are for Go callers, and a JSON encoding of the struct
// would make the prompt's vocabulary depend on a struct tag someone could rename.
func timelineAnswer(timeline appmedia.Timeline) map[string]any {
	shots := make([]map[string]any, 0, len(timeline.Shots))
	for _, shot := range timeline.Shots {
		entry := map[string]any{
			"ordinal":    shot.Ordinal,
			"shotId":     shot.ShotID,
			"durationMs": shot.DurationMS,
			"hasMedia":   shot.MediaVersionID != "",
			"hasAudio":   shot.HasAudio,
			"cueCount":   shot.CueCount,
		}
		if shot.MediaVersionID != "" {
			entry["mediaVersionId"] = shot.MediaVersionID
			entry["mediaKind"] = shot.MediaKind
			entry["mediaHash"] = shot.MediaHash
		}
		shots = append(shots, entry)
	}
	return map[string]any{
		"episodeId":       timeline.EpisodeID,
		"boardVersionId":  timeline.BoardVersionID,
		"scriptVersionId": timeline.ScriptVersionID,
		"shots":           shots,
		"totalDurationMs": timeline.TotalDurationMS,
		"missingMedia":    timeline.MissingMedia,
		"cueCount":        timeline.CueCount,
		"missingLines":    timeline.MissingLines,
	}
}
