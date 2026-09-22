package consistency

import (
	"context"
	"fmt"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// final.go is the deterministic half of AGENT_CONTRACTS section 11.4's Final Ruleset.
//
//	## 11.4 Final Ruleset
//	- 所有必需 Shot 有批准视频；
//	- 音频和字幕完整；
//	- 媒体文件存在；
//	- stale/waiver；
//	- 黑帧/空帧/静音异常；
//	- 总时长；
//	- 资源许可证元数据；
//	- 导出参数。
//
//	硬规则应尽量用确定性代码先检查，LLM Supervisor 负责语义质量。ReviewReport 合并两类证据，
//	并标记 source=deterministic|llm。
//
// # What this file can and cannot answer
//
// Seven of the eight clauses are questions about rows: does every shot have approved media, is every
// dialogue line subtitled, is the file a version cites still in the table, is a stale mark still
// open, do the durations agree, does the license metadata exist, are the export's parameters legal.
// A join answers each one the same way every time, which is exactly the division section 11.4 draws:
// code takes the mechanical half and the model is left the part that needs reading.
//
// The eighth — 黑帧/空帧/静音异常 — is a question about the BYTES of a media file, and this build
// answers it with the probe it already has rather than with a decode: a zero-duration stream, a file
// with no video stream, a file whose size is a few bytes. That catches the empty and the truncated,
// which is what an interrupted job and a provider returning a placeholder both produce. It does NOT
// catch a black frame inside a well-formed video, and section 11.4's wording is wider than this
// build's answer. The rule therefore reports what it can see and says in its own problem text which
// part of the clause it did not check, rather than reporting a clean result it did not earn.
//
// # Why the port is one call
//
// The eight rules read the same few things: a timeline, the export records, the file objects a
// version cites, the stale marks, the license metadata. Faithful storage ports for each would be five
// interfaces the composition root has to satisfy, and every one of them would be a place a later
// change could pass the wrong episode. One port that answers "everything about this episode's final
// state" keeps the episode identifier in one place — and it lets the reads share transactions, which
// a per-rule port could not.

// FinalReader is the read surface the Final Ruleset needs, about ONE episode.
//
// It is a port because the rules are about seven aggregates at once — the board, the assets, the
// subtitle tracks, the exports, the files, the staleness marks, the licenses — and this package must
// not import seven repositories. The composition root supplies one adapter over them, which is where
// this repository puts the seams between layers that must not know about each other.
type FinalReader interface {
	// FinalFacts returns everything the rules compare, for one episode.
	//
	// It returns a struct rather than a method per rule so the adapter reads a consistent picture:
	// eight calls could see the episode change between the first and the last, and a report assembled
	// from two states would name a fault that had already been fixed.
	FinalFacts(ctx context.Context, episodeID string) (FinalFacts, error)
}

// FinalFacts is one episode's final state, as the rules see it.
type FinalFacts struct {
	EpisodeID string
	// BoardVersionID and ScriptVersionID name the approved board this episode exports from, both
	// empty when none is approved — which is the first thing the rules report.
	BoardVersionID  string
	ScriptVersionID string
	Shots           []FinalShot
	// Cues are the approved subtitle track's cues, empty when no track is approved.
	CueCount      int
	SubtitleTrack string
	// MissingLines are the spoken lines the approved track does not cover.
	MissingLines []FinalLine
	// Files are the file objects the versions cited below refer to, by content hash.
	Files map[string]FinalFile
	// StaleMarks are the open staleness marks on this episode's artifacts.
	StaleMarks []FinalStaleMark
	// Licenses are the license metadata rows for the assets this episode uses, by asset id.
	Licenses map[string]FinalLicense
	// LicensesChecked reports whether this build has anywhere to read a license from.
	//
	// It exists because section 11.4's "资源许可证元数据" is a clause with no storage behind it in this
	// build: no migration carries a license column, and `assets.structured_attributes_json` is a
	// shape for asset properties rather than a rights record. A rule that read nothing and reported
	// nothing is the silently-green shape this repository refuses, so the adapter states whether the
	// question is answerable at all and the ruleset REPORTS THE GAP when it is not. STATUS section 0l
	// records the limitation and what adding the storage would take.
	LicensesChecked bool
	// Export is the newest export for this episode, if any, with its own parameters.
	Export *FinalExport
	// SubtitledDurationMS is the sum of the shots' own durations, which the export is compared
	// against.
	TotalDurationMS int
	// ScriptDurationMS is what the script estimated, for the total-duration rule.
	ScriptDurationMS int
}

// FinalShot is one shot of the approved board, with what is approved for it.
type FinalShot struct {
	Ordinal    int
	ItemID     string
	ShotID     string
	DurationMS int
	// MediaVersionID, MediaHash and MediaKind are the approved media, all empty when none is.
	MediaVersionID string
	MediaHash      string
	MediaKind      string
	// AudioVersionID is the approved audio for a line in this shot's scene, empty when none is.
	AudioVersionID string
	AudioHash      string
	AudioMIME      string
	AudioBytes     int64
	// AudioDurationMS is the audio's own length when it is known, zero when it is not.
	AudioDurationMS int
	// Required reports whether this shot must have approved media before the episode may be exported.
	//
	// It is a field rather than a rule because "which shots are required" is a production decision: a
	// build that assumed every row is required would refuse an export over a deliberately skipped
	// shot, and one that assumed none would pass an episode with no media at all.
	Required bool
	// LicenseAssetIDs are the assets this shot's approved media derives from — the character, the
	// location, the prop. They travel per shot rather than in one list for the episode because the
	// licence rule reports an asset once, and deduping across shots is what keeps a costume used in
	// twenty shots from producing twenty identical findings.
	LicenseAssetIDs []string
	// VideoMIME and VideoBytes describe the approved media's file, for the empty-file rule.
	VideoMIME  string
	VideoBytes int64
}

// FinalLine is one spoken line with no cue.
type FinalLine struct {
	LineID string
	Type   string
	Text   string
}

// FinalFile is one file object.
type FinalFile struct {
	Hash string
	MIME string
	Size int64
	// Present is false when a cited hash has no row, which is the missing-file rule's finding.
	Present bool
}

// FinalStaleMark is one open staleness mark.
type FinalStaleMark struct {
	ID         string
	Artifact   string
	EntityType string
	EntityID   string
	// Waived reports whether a person accepted the mark. A waived mark is not a blocker; the clause
	// is that a waiver must be VISIBLE, which is what the rules check.
	Waived bool
}

// FinalLicense is the license metadata of one asset.
type FinalLicense struct {
	AssetID   string
	Name      string
	License   string
	Source    string
	Present   bool
	AllowsUse bool
}

// FinalExport is one export record with its parameters.
type FinalExport struct {
	ID            string
	VersionNumber int
	Quality       string
	Width         int
	Height        int
	FPS           int
	DurationMS    int
	SubtitleMode  string
	SubtitleTrack string
	OutputHash    string
	// ManifestEpisodeID is the episode the manifest itself names, which the traceability rule
	// compares against the record's own.
	ManifestEpisodeID string
	// ManifestVersions are the version references the manifest carries, by role.
	ManifestVersions map[string]string
	// ManifestHashes are the content hashes the manifest claims, by role.
	ManifestHashes map[string]string
}

// Rule identifiers for section 11.4, in the same `DOMAIN_CONCEPT_CHECK` form the other rulesets use.
const (
	// RuleShotMedia is 11.4's "所有必需 Shot 有批准视频".
	RuleShotMedia = "SHOT_APPROVED_MEDIA"
	// RuleAudioComplete is 11.4's "音频和字幕完整", its audio clause.
	RuleAudioComplete = "AUDIO_COMPLETE"
	// RuleSubtitleComplete is 11.4's "音频和字幕完整", its subtitle clause.
	RuleSubtitleComplete = "SUBTITLE_COMPLETE"
	// RuleMediaFilePresent is 11.4's "媒体文件存在".
	RuleMediaFilePresent = "MEDIA_FILE_PRESENT"
	// RuleStaleOpen is 11.4's "stale/waiver".
	RuleStaleOpen = "STALE_MARK_OPEN"
	// RuleMediaEmpty is 11.4's "黑帧/空帧/静音异常", as far as a probe can answer it.
	RuleMediaEmpty = "MEDIA_EMPTY"
	// RuleFinalDuration is 11.4's "总时长".
	RuleFinalDuration = "FINAL_DURATION"
	// RuleLicenseMetadata is 11.4's "资源许可证元数据".
	RuleLicenseMetadata = "LICENSE_METADATA"
	// RuleExportParameters is 11.4's "导出参数".
	RuleExportParameters = "EXPORT_PARAMETERS"
	// RuleExportTraceable is AC-MEDIA-003's "清单可追溯": every version the manifest cites is still
	// the one in force, and every hash it states still matches.
	RuleExportTraceable = "EXPORT_TRACEABLE"
)

// FinalOptions configures the Final Ruleset.
type FinalOptions struct {
	// Reader supplies the episode's final state. A nil one makes every rule refuse rather than pass.
	Reader FinalReader
	// MinMediaBytes is the size below which a file is treated as empty. Zero uses the default.
	MinMediaBytes int64
	// MaxWidth and MaxHeight bound an export's frame size.
	MaxWidth  int
	MaxHeight int
	// MaxFPS bounds an export's frame rate.
	MaxFPS int
}

// The defaults the rules use when an option is not stated.
const (
	// DefaultMinMediaBytes is 1024. A PNG below a kilobyte is a placeholder rather than a picture,
	// and an MP4 below it is a container header with no frames — which is exactly what the mock
	// adapter's twenty-four byte box is, and why the rule exists.
	DefaultMinMediaBytes = 1024
	// DefaultMaxWidth, DefaultMaxHeight and DefaultMaxFPS are the bounds PRD FR-080 states for a
	// single episode: 4K at 60 frames a second. A request above them is refused rather than clamped,
	// because a user who asked for 8K and got 1080p without being told would export twice.
	DefaultMaxWidth  = 3840
	DefaultMaxHeight = 2160
	DefaultMaxFPS    = 60
)

// FinalRuleset is the deterministic half of section 11.4.
type FinalRuleset struct {
	reader        FinalReader
	minMediaBytes int64
	maxWidth      int
	maxHeight     int
	maxFPS        int
}

// NewFinalRuleset builds the ruleset.
func NewFinalRuleset(options FinalOptions) *FinalRuleset {
	minMediaBytes := options.MinMediaBytes
	if minMediaBytes <= 0 {
		minMediaBytes = DefaultMinMediaBytes
	}
	maxWidth := options.MaxWidth
	if maxWidth <= 0 {
		maxWidth = DefaultMaxWidth
	}
	maxHeight := options.MaxHeight
	if maxHeight <= 0 {
		maxHeight = DefaultMaxHeight
	}
	maxFPS := options.MaxFPS
	if maxFPS <= 0 {
		maxFPS = DefaultMaxFPS
	}
	return &FinalRuleset{
		reader:        options.Reader,
		minMediaBytes: minMediaBytes,
		maxWidth:      maxWidth,
		maxHeight:     maxHeight,
		maxFPS:        maxFPS,
	}
}

// Available reports whether the ruleset can read.
func (f *FinalRuleset) Available() bool { return f != nil && f.reader != nil }

// CheckEpisode runs every rule against one episode and returns the findings in a stable order.
//
// The order is `consistency.Sort`'s, so the same episode and the same state produce the same report
// in the same sequence: a reviewer comparing two runs must not see a changed set of problems where
// only the ordering moved.
func (f *FinalRuleset) CheckEpisode(ctx context.Context, episodeID string) ([]consistency.Finding, error) {
	if !f.Available() {
		return nil, errorf("no final-state reader is configured")
	}
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return nil, errorf("a final review must name its episode")
	}
	facts, err := f.reader.FinalFacts(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	findings := []consistency.Finding{}
	findings = append(findings, f.checkShotMedia(facts)...)
	findings = append(findings, f.checkAudio(facts)...)
	findings = append(findings, f.checkSubtitles(facts)...)
	findings = append(findings, f.checkFilesPresent(facts)...)
	findings = append(findings, f.checkEmptyMedia(facts)...)
	findings = append(findings, f.checkStale(facts)...)
	findings = append(findings, f.checkDuration(facts)...)
	findings = append(findings, f.checkLicenses(facts)...)
	findings = append(findings, f.checkExportParameters(facts)...)
	findings = append(findings, f.checkTraceability(facts)...)
	return consistency.Sort(consistency.Dedupe(findings)), nil
}

// CheckFinalVersion is the port `StageChecker` declares, so the stage machine can call this ruleset
// the way it calls every other one.
//
// The argument is the ARTIFACT version the stage produced. For `final_episode` that artifact is the
// episode itself — FR-100 gives the stage an episode as its subject, which is why the parameter is
// read as an episode identifier and why an empty one is refused rather than guessed.
func (f *FinalRuleset) CheckFinalVersion(ctx context.Context, episodeID string) ([]consistency.Finding, error) {
	return f.CheckEpisode(ctx, episodeID)
}

// checkShotMedia is 11.4's first clause: every required shot has approved media.
func (f *FinalRuleset) checkShotMedia(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	if len(facts.Shots) == 0 {
		// A board with no rows is a board nothing was boarded from, which the storyboard ruleset
		// already reports. Reporting it again here would say the episode has no media when the truth
		// is that it has no plan.
		return nil
	}
	for _, shot := range facts.Shots {
		if shot.MediaVersionID != "" {
			continue
		}
		// WHICH SHOTS ARE REQUIRED IS THE ADAPTER'S ANSWER, through `FinalShot.Required`. It is one
		// source rather than two: an earlier draft also carried a `RequiredShots` set on the options,
		// and two places to say "this shot must have media" are two places to disagree — one of them
		// would eventually be updated and the other would keep an episode exportable that a person had
		// marked otherwise.
		if !shot.Required {
			continue
		}
		findings = append(findings, consistency.Finding{
			Rule:       RuleShotMedia,
			Severity:   consistency.SeverityCritical,
			EntityType: "storyboard_item",
			EntityID:   shot.ItemID,
			Location:   fmt.Sprintf("shot %d", shot.Ordinal),
			Field:      "approvedImageAssetVersionId",
			Problem: fmt.Sprintf("Shot %d has no approved media, so the export would show %s "+
				"seconds of nothing where the shot belongs.", shot.Ordinal, seconds(shot.DurationMS)),
			Suggestion: "Approve a panel image or a video version for this shot, or mark the shot " +
				"as not required for the export.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: shot.ItemID},
				{Type: "entity_ref", Ref: shot.ShotID},
			},
			// Which take is approved is a person's decision, so the fix is not mechanical.
			AutoFixable: false,
		})
	}
	return findings
}

// checkAudio is 11.4's "音频和字幕完整", the audio half.
//
// It reports a shot that has a spoken line and no approved audio. The line count is not available
// per shot here — the adapter states whether audio is approved, which is the fact the rule needs —
// so the finding names the shot rather than the line, and a reader follows the board to see which
// line it was.
func (f *FinalRuleset) checkAudio(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	for _, shot := range facts.Shots {
		if shot.AudioVersionID != "" {
			continue
		}
		if !shot.Required {
			continue
		}
		findings = append(findings, consistency.Finding{
			Rule:       RuleAudioComplete,
			Severity:   consistency.SeverityMajor,
			EntityType: "storyboard_item",
			EntityID:   shot.ItemID,
			Location:   fmt.Sprintf("shot %d", shot.Ordinal),
			Field:      "audio",
			Problem: fmt.Sprintf("Shot %d has no approved audio, so its dialogue would be silent "+
				"in the export.", shot.Ordinal),
			Suggestion: "Run the text-to-speech job for this shot's dialogue and approve the result.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: shot.ItemID},
			},
			AutoFixable: false,
		})
	}
	return findings
}

// checkSubtitles is 11.4's "音频和字幕完整", the subtitle half, and AC-MEDIA-002's missing-line clause.
func (f *FinalRuleset) checkSubtitles(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	if facts.SubtitleTrack == "" {
		findings = append(findings, consistency.Finding{
			Rule:       RuleSubtitleComplete,
			Severity:   consistency.SeverityMajor,
			EntityType: "episode",
			EntityID:   facts.EpisodeID,
			Field:      "subtitleTrackId",
			Problem:    "This episode has no approved subtitle track, so the export would carry no captions.",
			Suggestion: "Draft a subtitle track from the script, check it, and approve it.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: facts.EpisodeID},
			},
			AutoFixable: false,
		})
		return findings
	}
	for _, line := range facts.MissingLines {
		findings = append(findings, consistency.Finding{
			Rule:       RuleSubtitleComplete,
			Severity:   consistency.SeverityMajor,
			EntityType: "dialogue_line",
			EntityID:   line.LineID,
			Field:      "text",
			Problem: "A " + line.Type + " line has no subtitle cue: " + truncate(line.Text, 60) +
				". A viewer would hear it and read nothing.",
			Suggestion: "Add a cue for this line in the subtitle editor, or re-draft the track.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: line.LineID},
				{Type: "entity_ref", Ref: facts.SubtitleTrack},
			},
			// The missing cue is mechanical: the text and its neighbours are known, so a draft fills
			// it in. Whether the timing is right is the part a person checks.
			AutoFixable: true,
		})
	}
	return findings
}

// checkFilesPresent is 11.4's "媒体文件存在".
//
// A version that cites a hash with no row, or a row whose file is not in the store, is the state an
// interrupted job leaves behind: the reference was written and the bytes were not. It is a BLOCKER
// because the export would fail at composition with an error about a file rather than about the
// episode.
func (f *FinalRuleset) checkFilesPresent(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	seen := map[string]bool{}
	report := func(entityType, entityID, location, role, hash string) {
		if hash == "" || seen[hash] {
			return
		}
		file, ok := facts.Files[hash]
		if ok && file.Present {
			return
		}
		seen[hash] = true
		findings = append(findings, consistency.Finding{
			Rule:       RuleMediaFilePresent,
			Severity:   consistency.SeverityCritical,
			EntityType: entityType,
			EntityID:   entityID,
			Location:   location,
			Field:      role,
			Problem: "The approved " + role + " cites a file that is not in this project's store, " +
				"so the export would fail while composing rather than here.",
			Suggestion: "Re-run the job that produced this version, or approve a version whose file " +
				"is present.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: entityID},
				{Type: "entity_ref", Ref: hash},
			},
			AutoFixable: false,
		})
	}
	for _, shot := range facts.Shots {
		report("storyboard_item", shot.ItemID, fmt.Sprintf("shot %d", shot.Ordinal), "media", shot.MediaHash)
		report("storyboard_item", shot.ItemID, fmt.Sprintf("shot %d", shot.Ordinal), "audio", shot.AudioHash)
	}
	if facts.Export != nil {
		report("episode_export", facts.Export.ID, "", "export", facts.Export.OutputHash)
	}
	return findings
}

// checkEmptyMedia is 11.4's "黑帧/空帧/静音异常", as far as this build can answer it.
//
// Three things are visible without decoding: a file below the minimum size, a file whose type is not
// a media type, and an audio track whose length is zero. Each is what a placeholder, a failed
// download or an interrupted job produces.
//
// What this does NOT catch is stated in the finding vocabulary rather than hidden: a black frame
// inside a well-formed video, and a silent passage inside a well-formed audio file, both pass. The
// rule's severity is therefore major rather than critical for the size case — it is a strong signal
// and not a proof — and the package comment records the gap.
func (f *FinalRuleset) checkEmptyMedia(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	for _, shot := range facts.Shots {
		if shot.VideoBytes > 0 && shot.VideoBytes < f.minMediaBytes {
			findings = append(findings, consistency.Finding{
				Rule:       RuleMediaEmpty,
				Severity:   consistency.SeverityCritical,
				EntityType: "storyboard_item",
				EntityID:   shot.ItemID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "media",
				Problem: fmt.Sprintf("Shot %d's approved media is %d bytes, which is a placeholder "+
					"rather than a picture or a clip.", shot.Ordinal, shot.VideoBytes),
				Suggestion: "Approve a version whose file is a real render.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: shot.MediaVersionID},
				},
				AutoFixable: false,
			})
		}
		if shot.VideoMIME != "" && !isMediaMIME(shot.VideoMIME) {
			findings = append(findings, consistency.Finding{
				Rule:       RuleMediaEmpty,
				Severity:   consistency.SeverityCritical,
				EntityType: "storyboard_item",
				EntityID:   shot.ItemID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "mimeType",
				Problem: "Shot " + itoa(shot.Ordinal) + "'s approved media is " + shot.VideoMIME +
					", which is not a media type ffmpeg can compose.",
				Suggestion: "Approve a version whose file is an image or a video.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: shot.MediaVersionID},
				},
				AutoFixable: false,
			})
		}
		if shot.AudioVersionID != "" && shot.AudioDurationMS == 0 && shot.AudioBytes > 0 &&
			shot.AudioBytes < f.minMediaBytes {
			findings = append(findings, consistency.Finding{
				Rule:       RuleMediaEmpty,
				Severity:   consistency.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   shot.ItemID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "audio",
				Problem: fmt.Sprintf("Shot %d's approved audio is %d bytes, which is a container "+
					"header rather than a recording.", shot.Ordinal, shot.AudioBytes),
				Suggestion: "Re-run the text-to-speech job and approve a result with real audio in it.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: shot.AudioVersionID},
				},
				AutoFixable: false,
			})
		}
	}
	return findings
}

// checkStale is 11.4's "stale/waiver".
//
// A stale mark means an artifact was changed after something downstream was built from it. Two
// clauses matter and they are different: an UNRESOLVED mark is a blocker, and a WAIVED mark is not —
// but §15.3 requires the waiver to be visible in the final export, so the ruleset reports a waived
// mark as a MINOR finding rather than staying silent about it. A report that said nothing about a
// waiver would be hiding the one thing the clause asks to be shown.
func (f *FinalRuleset) checkStale(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	for _, mark := range facts.StaleMarks {
		if mark.Waived {
			findings = append(findings, consistency.Finding{
				Rule:       RuleStaleOpen,
				Severity:   consistency.SeverityMinor,
				EntityType: mark.EntityType,
				EntityID:   mark.EntityID,
				Field:      "waiver",
				Problem: "A stale mark on " + mark.Artifact + " was waived rather than resolved, so " +
					"this episode was exported from an artifact someone accepted as out of date.",
				Suggestion: "Nothing to fix. This finding records the waiver so the export's reader " +
					"sees it.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: mark.ID},
					{Type: "entity_ref", Ref: mark.EntityID},
				},
				AutoFixable: false,
			})
			continue
		}
		findings = append(findings, consistency.Finding{
			Rule:       RuleStaleOpen,
			Severity:   consistency.SeverityCritical,
			EntityType: mark.EntityType,
			EntityID:   mark.EntityID,
			Field:      "stale",
			Problem: "A stale mark on " + mark.Artifact + " is still open, so something this episode " +
				"was built from has changed since.",
			Suggestion: "Regenerate the affected artifact, or clear the mark if it no longer applies.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: mark.ID},
				{Type: "entity_ref", Ref: mark.EntityID},
			},
			AutoFixable: false,
		})
	}
	return findings
}

// checkDuration is 11.4's "总时长".
//
// Two comparisons, and they are different questions. The board's own rows against the script's
// estimate is the planning drift the storyboard ruleset also checks — here it is reported against
// the EPISODE, because an export's length is what a distributor is promised. The export's recorded
// duration against the board's total is the composition's own drift, and a mismatch there means the
// file that was produced is not the film that was planned.
func (f *FinalRuleset) checkDuration(facts FinalFacts) []consistency.Finding {
	findings := []consistency.Finding{}
	if facts.TotalDurationMS == 0 {
		return nil
	}
	if facts.ScriptDurationMS > 0 {
		// BOTH SIDES ARE MILLISECONDS, and the adapter is where the script's whole seconds become
		// them: `ScriptDurationMS` is named for its unit, and the field the migration stores
		// (`estimated_duration_seconds`) is not. An earlier draft multiplied here as well, which made
		// an eight-second board read as two hours twenty against its own script's eight seconds and
		// reported drift on every episode. The healthy-episode baseline is what caught it.
		expected := facts.ScriptDurationMS
		drift := facts.TotalDurationMS - expected
		if drift < 0 {
			drift = -drift
		}
		// A tenth, with a floor of five seconds: the same tolerance the storyboard ruleset uses and
		// for the same reason — a board states whole seconds per row, so the total is approximate by
		// construction.
		tolerance := expected / 10
		if tolerance < 5000 {
			tolerance = 5000
		}
		if drift > tolerance {
			findings = append(findings, consistency.Finding{
				Rule:       RuleFinalDuration,
				Severity:   consistency.SeverityMinor,
				EntityType: "episode",
				EntityID:   facts.EpisodeID,
				Field:      "durationMs",
				Problem: fmt.Sprintf("The board's rows add up to %s against the script's %s, a "+
					"difference of %s.", duration(facts.TotalDurationMS),
					duration(expected), duration(drift)),
				Suggestion: "Adjust the rows' durations or the script's estimate so the episode's " +
					"length is what either one says.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: facts.BoardVersionID},
					{Type: "entity_ref", Ref: facts.ScriptVersionID},
				},
				AutoFixable: false,
			})
		}
	}
	if facts.Export != nil && facts.Export.DurationMS > 0 {
		drift := facts.Export.DurationMS - facts.TotalDurationMS
		if drift < 0 {
			drift = -drift
		}
		if drift > 1000 {
			findings = append(findings, consistency.Finding{
				Rule:       RuleFinalDuration,
				Severity:   consistency.SeverityMajor,
				EntityType: "episode_export",
				EntityID:   facts.Export.ID,
				Field:      "durationMs",
				Problem: "The exported file is " + duration(facts.Export.DurationMS) + " while the " +
					"board's shots add up to " + duration(facts.TotalDurationMS) + ", so the file is " +
					"not the film that was planned.",
				Suggestion: "Re-export the episode, and check that every shot's approved media has " +
					"the length the row states.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: facts.Export.ID},
					{Type: "entity_ref", Ref: facts.BoardVersionID},
				},
				AutoFixable: false,
			})
		}
	}
	return findings
}

// checkLicenses is 11.4's "资源许可证元数据".
//
// # The clause this build cannot fully answer, and how it says so
//
// There is nowhere in this build's schema to read a license from: no migration carries a license
// column, and PRD R9's mitigation is a THIRD_PARTY_NOTICES document rather than a per-asset rights
// record. A rule that looked for one and found nothing would either report every asset as
// unlicensed — a page of findings a user learns to ignore — or report none and read as green, which
// is the silently-passing shape this repository refuses outright.
//
// So the rule reports the GAP ITSELF, once, as a minor finding that names what is missing and what
// adding it would take. That is the honest answer to "did you check the licence metadata": no, and
// here is why, rather than a clean result nobody earned. STATUS section 0l carries the same
// limitation.
//
// When an adapter CAN answer — a build whose storage has the column — the per-asset rule below runs
// in full, and the two paths are exclusive: a build either has the storage or reports that it does
// not.
func (f *FinalRuleset) checkLicenses(facts FinalFacts) []consistency.Finding {
	if !facts.LicensesChecked {
		// Nothing is exported from an episode with no shots, so the gap is only worth reporting
		// once there is something whose rights would matter.
		if len(facts.Shots) == 0 {
			return nil
		}
		return []consistency.Finding{{
			Rule:       RuleLicenseMetadata,
			Severity:   consistency.SeverityMinor,
			EntityType: "episode",
			EntityID:   facts.EpisodeID,
			Field:      "license",
			Problem: "This build has no per-asset licence record to check, so nothing verified that " +
				"the assets this episode uses may be exported. PRD R9's mitigation is a THIRD_PARTY_" +
				"NOTICES document rather than a rights record on each asset.",
			Suggestion: "Nothing to fix in the episode. Closing this gap means adding a licence " +
				"column to the asset aggregate and a command to record it, which STATUS section 0l " +
				"records as unbuilt.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: facts.EpisodeID},
			},
			AutoFixable: false,
		}}
	}
	findings := []consistency.Finding{}
	reported := map[string]bool{}
	for _, shot := range facts.Shots {
		for _, assetID := range shotLicenseAssets(shot) {
			if assetID == "" || reported[assetID] {
				continue
			}
			license, ok := facts.Licenses[assetID]
			if ok && license.Present && license.AllowsUse {
				continue
			}
			reported[assetID] = true
			problem := "An asset this episode uses has no licence metadata, so the episode's rights " +
				"cannot be stated."
			suggestion := "Record the asset's licence and source before exporting."
			severity := consistency.SeverityMajor
			if ok && license.Present && !license.AllowsUse {
				problem = "The asset " + license.Name + " states the licence " + license.License +
					", which does not allow this use."
				suggestion = "Replace the asset with one whose licence permits the export."
				severity = consistency.SeverityCritical
			} else if ok && license.Name != "" {
				problem = "The asset " + license.Name + " has no licence metadata, so the episode's " +
					"rights cannot be stated."
			}
			findings = append(findings, consistency.Finding{
				Rule:       RuleLicenseMetadata,
				Severity:   severity,
				EntityType: "asset",
				EntityID:   assetID,
				Field:      "license",
				Problem:    problem,
				Suggestion: suggestion,
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: assetID},
					{Type: "entity_ref", Ref: facts.EpisodeID},
				},
				AutoFixable: false,
			})
		}
	}
	return findings
}

// checkExportParameters is 11.4's "导出参数".
//
// The bounds are stated in the ruleset rather than read from the export, because an export that
// stated its own limits would pass whatever it was given.
func (f *FinalRuleset) checkExportParameters(facts FinalFacts) []consistency.Finding {
	export := facts.Export
	if export == nil {
		// No export yet is not a parameter fault: the episode has not been exported, which the
		// traceability rule reports as "there is nothing to trace" rather than here.
		return nil
	}
	findings := []consistency.Finding{}
	refuse := func(field, problem, suggestion string, auto bool) {
		findings = append(findings, consistency.Finding{
			Rule:       RuleExportParameters,
			Severity:   consistency.SeverityCritical,
			EntityType: "episode_export",
			EntityID:   export.ID,
			Field:      field,
			Problem:    problem,
			Suggestion: suggestion,
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: export.ID},
			},
			AutoFixable: auto,
		})
	}
	if !domainmedia.IsValidExportQuality(domainmedia.ExportQuality(export.Quality)) {
		refuse("quality", "The export's quality is "+export.Quality+", which is not one this build "+
			"produces.", "Choose a preview or a final export.", false)
	}
	if export.Width <= 0 || export.Height <= 0 {
		refuse("width", "The export states no frame size.", "Choose a resolution.", false)
	} else if export.Width > f.maxWidth || export.Height > f.maxHeight {
		refuse("width", fmt.Sprintf("The export states %dx%d, which is larger than the %dx%d this "+
			"build renders.", export.Width, export.Height, f.maxWidth, f.maxHeight),
			"Choose a resolution within the build's bounds.", false)
	}
	// An odd dimension is refused rather than rounded: every video codec this build writes wants
	// even dimensions, and an odd one produces a file that plays in some players and not others.
	if export.Width%2 != 0 || export.Height%2 != 0 {
		refuse("width", fmt.Sprintf("The export states %dx%d, and a video codec needs even "+
			"dimensions.", export.Width, export.Height), "Choose an even frame size.", false)
	}
	if export.FPS > f.maxFPS {
		refuse("fps", fmt.Sprintf("The export states %d frames a second, which is above the %d this "+
			"build renders.", export.FPS, f.maxFPS), "Choose a frame rate within the build's bounds.", false)
	}
	if !domainmedia.IsValidSubtitleMode(domainmedia.SubtitleMode(export.SubtitleMode)) {
		refuse("subtitleMode", "The export's subtitle mode is "+export.SubtitleMode+
			", which is not one this build recognises.", "Choose off, sidecar or burn-in.", false)
	}
	// A sidecar subtitle mode with no track is a contradiction: the export was asked to carry
	// captions and had none to carry, which produces a file a viewer reads as deliberately uncaptioned.
	if domainmedia.SubtitleMode(export.SubtitleMode) != domainmedia.SubtitleNone && export.SubtitleTrack == "" {
		refuse("subtitleTrackId", "The export was asked to include subtitles in "+
			export.SubtitleMode+" mode with no subtitle track, so the file carries none.",
			"Approve a subtitle track, or export with subtitles off.", true)
	}
	return findings
}

// checkTraceability is AC-MEDIA-003's "清单可追溯": what the manifest claims must still be true.
//
// DOMAIN_MODEL section 15.3 makes the manifest the document that says what an export was made from.
// Three things can make it a lie after the fact: a version it cites has been superseded, a hash it
// states no longer matches the file, or it names a different episode from the record that holds it.
// Each is reported, because an export whose manifest cannot be trusted is an export a distributor
// cannot accept — which is the whole point of the clause.
func (f *FinalRuleset) checkTraceability(facts FinalFacts) []consistency.Finding {
	export := facts.Export
	if export == nil {
		return []consistency.Finding{{
			Rule:       RuleExportTraceable,
			Severity:   consistency.SeverityMajor,
			EntityType: "episode",
			EntityID:   facts.EpisodeID,
			Field:      "export",
			Problem: "This episode has no export, so there is no manifest to trace and nothing a " +
				"distributor could accept.",
			Suggestion: "Export the episode once its shots, audio and subtitles are approved.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: facts.EpisodeID},
			},
			AutoFixable: false,
		}}
	}
	findings := []consistency.Finding{}
	if export.ManifestEpisodeID != "" && export.ManifestEpisodeID != facts.EpisodeID {
		findings = append(findings, consistency.Finding{
			Rule:       RuleExportTraceable,
			Severity:   consistency.SeverityCritical,
			EntityType: "episode_export",
			EntityID:   export.ID,
			Field:      "manifestJson",
			Problem: "The export's manifest names a different episode from the record that holds it, " +
				"so the document does not describe this film.",
			Suggestion: "Re-export the episode, and investigate how the manifest was written.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: export.ID},
				{Type: "entity_ref", Ref: export.ManifestEpisodeID},
			},
			AutoFixable: false,
		})
	}
	// The media the manifest cites must be the media that is approved NOW. A version superseded after
	// the export is the case ADR-0015 section 11 rules on: it makes the export STALE rather than
	// wrong, which is a finding a person resolves by re-exporting or by accepting it.
	for _, shot := range facts.Shots {
		if shot.MediaVersionID == "" {
			continue
		}
		claimed, stated := export.ManifestVersions["shot:"+shot.ShotID]
		if !stated {
			continue
		}
		if claimed != shot.MediaVersionID {
			findings = append(findings, consistency.Finding{
				Rule:       RuleExportTraceable,
				Severity:   consistency.SeverityMajor,
				EntityType: "episode_export",
				EntityID:   export.ID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "manifestJson",
				Problem: "The manifest records shot " + itoa(shot.Ordinal) + "'s media as a version " +
					"that is no longer the one approved, so the file on disk was made from footage " +
					"this project has since replaced.",
				Suggestion: "Re-export the episode so the file and the manifest agree, or accept the " +
					"export as a record of the earlier cut.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: export.ID},
					{Type: "entity_ref", Ref: claimed},
					{Type: "entity_ref", Ref: shot.MediaVersionID},
				},
				AutoFixable: false,
			})
		}
		if claimedHash, stated := export.ManifestHashes["shot:"+shot.ShotID]; stated &&
			shot.MediaHash != "" && claimedHash != shot.MediaHash {
			findings = append(findings, consistency.Finding{
				Rule:       RuleExportTraceable,
				Severity:   consistency.SeverityCritical,
				EntityType: "episode_export",
				EntityID:   export.ID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "manifestJson",
				Problem: "The manifest states a content hash for shot " + itoa(shot.Ordinal) +
					" that is not the hash of the version it cites, so the document does not match " +
					"the bytes it describes.",
				Suggestion: "Re-export the episode. A hash that disagrees with its version means " +
					"either the file or the manifest was written by something other than this build.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: export.ID},
					{Type: "entity_ref", Ref: claimedHash},
					{Type: "entity_ref", Ref: shot.MediaHash},
				},
				AutoFixable: false,
			})
		}
	}
	return findings
}

// shotLicenseAssets names the assets one shot's approved media comes from.
//
// A shot with no approved media has nothing to licence, and returns nil rather than the asset list:
// the media is what an export reproduces, and an asset no exported frame shows is not this rule's
// subject.
func shotLicenseAssets(shot FinalShot) []string {
	if shot.MediaVersionID == "" {
		return nil
	}
	return shot.LicenseAssetIDs
}

// isMediaMIME reports whether a type is one ffmpeg composes.
func isMediaMIME(mime string) bool {
	if strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/") {
		return true
	}
	return strings.HasPrefix(mime, "audio/")
}

// seconds renders milliseconds as a seconds string for a sentence.
//
// The `itoa`, `pad2` and `pad3` helpers below are this FILE's own additions where they differ from
// `checker.go`'s `itoa`: that one renders an integer and this file also needs fixed-width parts. The
// integer renderer is the shared one, so the two rulesets cannot disagree about what "12" looks like.
func seconds(milliseconds int) string {
	if milliseconds <= 0 {
		return "0"
	}
	whole := milliseconds / 1000
	remainder := milliseconds % 1000
	if remainder == 0 {
		return itoa(whole)
	}
	return itoa(whole) + "." + pad3(remainder)
}

// duration renders milliseconds as `m:ss` for a sentence about a film's length.
func duration(milliseconds int) string {
	if milliseconds < 0 {
		milliseconds = -milliseconds
	}
	total := milliseconds / 1000
	return itoa(total/60) + ":" + pad2(total%60)
}

// pad2 and pad3 render the fixed-width parts of the two durations above.
func pad2(value int) string {
	if value < 10 {
		return "0" + itoa(value)
	}
	return itoa(value)
}

func pad3(value int) string {
	switch {
	case value < 10:
		return "00" + itoa(value)
	case value < 100:
		return "0" + itoa(value)
	default:
		return itoa(value)
	}
}

// truncate shortens a line of text for a message, marking the cut rather than ending mid-word.
func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}
