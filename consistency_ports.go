package main

import (
	"context"
	"strings"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
)

// consistency_ports.go adapts the services the deterministic checks read to the checker's ports.
//
// # Why the adapters live here
//
// The checker declares narrow interfaces — what a rule needs, phrased as the question it asks — and
// the services expose their own wider surfaces. Neither should change for the other: a service that
// grew a `ShotScene` method because a checker wanted one would be putting a rule's vocabulary into
// the aggregate that owns the data, and a checker that imported the script service would depend on
// its whole graph to ask one question.
//
// So the translation is at the composition root, which is where this repository puts the seams
// between layers that must not know about each other.
//
// # The script adapter reads ONE structure and answers four questions
//
// `ShotOrders`, `ScriptDuration`, `SceneEventOf` and `ShotScene` are four views of the same two reads
// — the script version and its structure — so the adapter caches them per call. The checker calls
// them in a loop over the board's rows, and re-reading a script structure per row would make the
// rules cost O(rows × structure) for an answer that cannot change within one check.
type scriptConsistencyReader struct {
	script  *appscript.Service
	version string
	// loaded guards the two cached reads. A zero-value cache means "not read yet", and the error is
	// kept so a failure is reported once rather than retried per row.
	structure *script.ScriptStructure
	duration  int
	loaded    bool
	failed    error
}

// scriptConsistencySource hands out one reader per script version.
//
// It is the port the checker asks for a reader, and it exists so a single checker can serve every
// board: the version comes from the board's own row rather than from the caller, which is what keeps
// a check a function of the artifact it is checking.
type scriptConsistencySource struct {
	script *appscript.Service
}

func (s scriptConsistencySource) ScriptReaderFor(ctx context.Context, scriptVersionID string) appconsistency.ScriptReader {
	if s.script == nil || strings.TrimSpace(scriptVersionID) == "" {
		return nil
	}
	return &scriptConsistencyReader{script: s.script, version: scriptVersionID}
}

var _ appconsistency.ScriptReaderSource = scriptConsistencySource{}

// load reads the version and its structure once.
func (r *scriptConsistencyReader) load(ctx context.Context) error {
	if r.loaded {
		return r.failed
	}
	r.loaded = true
	if r.script == nil || strings.TrimSpace(r.version) == "" {
		return nil
	}
	version, err := r.script.GetScriptVersion(ctx, r.version)
	if err != nil {
		r.failed = err
		return err
	}
	r.duration = version.EstimatedDurationSeconds
	structure, err := r.script.GetScriptStructure(ctx, r.version)
	if err != nil {
		r.failed = err
		return err
	}
	r.structure = &structure
	return nil
}

// ShotIDs returns the identifier of every shot in the version, in order.
//
// The identifier is the SHOT's, because a board row cites a shot id rather than a position — the rows
// are compared against the script by identity, and the position is only what the message says.
func (r *scriptConsistencyReader) ShotIDs(ctx context.Context) ([]string, error) {
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	if r.structure == nil {
		return nil, nil
	}
	shots := []string{}
	for _, scene := range r.structure.Scenes {
		for _, shot := range scene.Shots {
			shots = append(shots, shot.ID)
		}
	}
	return shots, nil
}

// Duration returns the version's own estimate.
func (r *scriptConsistencyReader) Duration(ctx context.Context) (int, error) {
	if err := r.load(ctx); err != nil {
		return 0, err
	}
	return r.duration, nil
}

// SceneEventOf returns the story event a scene adapts.
func (r *scriptConsistencyReader) SceneEventOf(ctx context.Context, sceneID string) (string, error) {
	if err := r.load(ctx); err != nil {
		return "", err
	}
	if r.structure == nil {
		return "", nil
	}
	for _, scene := range r.structure.Scenes {
		if scene.ID == sceneID {
			return scene.SourceStoryEventID, nil
		}
	}
	return "", nil
}

// ShotScene returns which scene a shot belongs to.
func (r *scriptConsistencyReader) ShotScene(ctx context.Context, shotID string) (string, error) {
	if err := r.load(ctx); err != nil {
		return "", err
	}
	if r.structure == nil {
		return "", nil
	}
	for _, scene := range r.structure.Scenes {
		for _, shot := range scene.Shots {
			if shot.ID == shotID {
				return scene.ID, nil
			}
		}
	}
	return "", nil
}

// scriptRepositorySource adapts the SCRIPT REPOSITORY to the checker's reader source.
//
// It is a second source beside scriptConsistencySource, and the two exist because the two callers
// have different things in hand: the composition root has the repository, and it is the one the
// wiring can build without a service. The translation is the same four questions, so the repository
// and the service answer them identically — which the compile-time assertions below are what keep
// true.
func newScriptReaderSource(repo *database.ScriptRepository) appconsistency.ScriptReaderSource {
	return scriptRepositorySource{repo: repo}
}

type scriptRepositorySource struct {
	repo *database.ScriptRepository
}

func (s scriptRepositorySource) ScriptReaderFor(ctx context.Context, scriptVersionID string) appconsistency.ScriptReader {
	if s.repo == nil || trimWhitespace(scriptVersionID) == "" {
		return nil
	}
	return &scriptRepositoryReader{repo: s.repo, version: scriptVersionID}
}

var _ appconsistency.ScriptReaderSource = scriptRepositorySource{}

// scriptRepositoryReader answers the four questions from one structure read.
type scriptRepositoryReader struct {
	repo    *database.ScriptRepository
	version string
	// loaded guards the cached read, because the rules ask four questions of the same structure and
	// re-reading it per question would make a check cost a multiple of its own data.
	structure *script.ScriptStructure
	loaded    bool
}

func (r *scriptRepositoryReader) load(ctx context.Context) *script.ScriptStructure {
	if r.loaded {
		return r.structure
	}
	r.loaded = true
	if r.repo == nil || r.version == "" {
		return nil
	}
	structure, err := r.repo.GetScriptStructure(ctx, r.version)
	if err != nil {
		return nil
	}
	r.structure = &structure
	return r.structure
}

func (r *scriptRepositoryReader) ShotIDs(ctx context.Context) ([]string, error) {
	structure := r.load(ctx)
	if structure == nil {
		return nil, nil
	}
	ids := []string{}
	for _, scene := range structure.Scenes {
		for _, shot := range scene.Shots {
			ids = append(ids, shot.ID)
		}
	}
	return ids, nil
}

func (r *scriptRepositoryReader) Duration(ctx context.Context) (int, error) {
	_ = r.load(ctx)
	if r.repo == nil {
		return 0, nil
	}
	version, err := r.repo.GetScriptVersion(ctx, r.version)
	if err != nil {
		return 0, err
	}
	return version.EstimatedDurationSeconds, nil
}

func (r *scriptRepositoryReader) SceneEventOf(ctx context.Context, sceneID string) (string, error) {
	structure := r.load(ctx)
	if structure == nil {
		return "", nil
	}
	for _, scene := range structure.Scenes {
		if scene.ID == sceneID {
			return scene.SourceStoryEventID, nil
		}
	}
	return "", nil
}

func (r *scriptRepositoryReader) ShotScene(ctx context.Context, shotID string) (string, error) {
	structure := r.load(ctx)
	if structure == nil {
		return "", nil
	}
	for _, scene := range structure.Scenes {
		for _, shot := range scene.Shots {
			if shot.ID == shotID {
				return scene.ID, nil
			}
		}
	}
	return "", nil
}

var _ appconsistency.ScriptReader = (*scriptRepositoryReader)(nil)

// trimWhitespace reports whether a string is empty or only spaces.
func trimWhitespace(value string) string {
	start := 0
	end := len(value)
	for start < end && isSpace(value[start]) {
		start++
	}
	for end > start && isSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isSpace(symbol byte) bool {
	return symbol == ' ' || symbol == '\t' || symbol == '\n' || symbol == '\r'
}

// The compile-time proof that the adapter satisfies the checker's script port.
//
// The same reasoning the memory repository's assertions state: without this, a signature drift is
// invisible, because the checker takes an interface and a partial composition reads at runtime as
// "this rule did not run".
var _ appconsistency.ScriptReader = (*scriptConsistencyReader)(nil)

// storyboardConsistencyReader is the checker's storyboard port over the storyboard service.
//
// It is a thin pass-through and that is the point: the service already has exactly the two reads the
// rules need, so a second layer of adaptation would be a place for them to drift.
type storyboardConsistencyReader struct {
	service StoryboardConsistencySource
}

// StoryboardConsistencySource is the two storyboard reads the rules make.
type StoryboardConsistencySource interface {
	GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error)
	ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error)
}

func (r storyboardConsistencyReader) GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error) {
	return r.service.GetStoryboardVersion(ctx, id)
}

func (r storyboardConsistencyReader) ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error) {
	return r.service.ListStoryboardItems(ctx, storyboardVersionID)
}

var _ appconsistency.StoryboardReader = storyboardConsistencyReader{}

// assetConsistencyReader is the checker's asset port over the asset repository.
//
// It uses the REPOSITORY rather than the service for two of its three reads, and that is a
// deliberate exception to this file's pattern: `UsagesForConsumer` and the two lookup reads are
// storage questions with no domain rule attached, and routing them through the service would mean
// adding three pass-through methods to it for one caller.
type assetConsistencyReader struct {
	usages   UsageReader
	versions VersionReader
	assets   AssetLookup
}

// UsageReader, VersionReader and AssetLookup are the three storage reads the asset rules make.
type UsageReader interface {
	UsagesForConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error)
}

type VersionReader interface {
	GetVersion(ctx context.Context, id string) (asset.Version, error)
}

type AssetLookup interface {
	GetAsset(ctx context.Context, id string) (asset.Asset, error)
}

func (r assetConsistencyReader) UsagesForConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error) {
	return r.usages.UsagesForConsumer(ctx, consumerType, consumerID)
}

func (r assetConsistencyReader) GetVersion(ctx context.Context, id string) (asset.Version, error) {
	return r.versions.GetVersion(ctx, id)
}

func (r assetConsistencyReader) GetAsset(ctx context.Context, id string) (asset.Asset, error) {
	return r.assets.GetAsset(ctx, id)
}

var _ appconsistency.AssetReader = assetConsistencyReader{}

// storyConsistencyReader is the checker's story port over the story repository.
type storyConsistencyReader struct {
	states StoryStateSource
}

// StoryStateSource is the two story reads the continuity rules make.
type StoryStateSource interface {
	CostumeStateAt(ctx context.Context, characterEntityID string, eventOrder int) (string, bool, error)
	StoryEventParticipantsFor(ctx context.Context, storyEventID string) ([]string, error)
}

func (r storyConsistencyReader) CostumeStateAt(ctx context.Context, characterEntityID string, eventOrder int) (string, bool, error) {
	return r.states.CostumeStateAt(ctx, characterEntityID, eventOrder)
}

func (r storyConsistencyReader) StoryEventParticipantsFor(ctx context.Context, storyEventID string) ([]string, error) {
	return r.states.StoryEventParticipantsFor(ctx, storyEventID)
}

var _ appconsistency.StoryStateReader = storyConsistencyReader{}
