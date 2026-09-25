package memory

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// The summary chain, which is PRD FR-120's "支持层级摘要：message → episode/session →
// project" and AC-MEM-004's "Summary 关联源消息表".
//
// # Why the text is extracted rather than generated
//
// A summary in this build is DETERMINISTIC: it is assembled from the memories it covers,
// by a rule stated below, with no model call. That is a deliberate limit and ADR-0014
// records the cost — the text reads mechanically — but it buys three things the
// specification actually grades on:
//
//   - AC-MEM-004 asks that role, agent and time be preserved and that a UI can jump to the
//     original. A summary whose text was written by a model can only promise that; this one
//     can PROVE it, because the reader reverses the citations mechanically.
//   - AC-MEM-005's deep recall asks for "恢复原始消息" and "返回来源". Determinism is what
//     makes that checkable in CI rather than a matter of the model's mood.
//   - PRD FR-120 forbids uploading project text without authorisation. A build with no
//     embedding provider configured can still summarise, because nothing leaves the
//     process.
//
// # The three rungs
//
// FR-120's ladder is message -> episode/session -> project, and each rung is a summary of the
// rung below it in that rung's scope:
//
//  1. MESSAGES (episodic memories), scoped to the conversation that produced them;
//  2. LEVEL-ONE SUMMARIES, scoped to the episode (the agent and session cleared);
//  3. EPISODE SUMMARIES, scoped to the project (the episode cleared too).
//
// # Why the third rung has no trigger problem
//
// The comment that stood here said a project-level summary "would need a trigger the
// specification does not give, and inventing one would mean choosing when a project is
// done". That was wrong about what the rung IS. A project summary is not a verdict on a
// finished project; it is the same operation as the other two — condense the uncondensed
// rung below you, in your own scope — and it needs no notion of completion. The floor is
// what keeps it from firing pointlessly: two children, exactly as level two requires, so a
// project with one episode summary produces nothing and a project with three produces one
// summary that a later run folds the fourth into. That is a rolling summarisation, not a
// completion event.
const (
	// SummaryRulesetVersion names the recipe, and it is stored on every summary row.
	//
	// It is the analogue of the review reports' `rulesetVersion`, and it exists for the
	// same reason: a reader has to be able to tell which rule produced a text, because a
	// change to the rule changes every future summary and an old one has to stay readable
	// on its own terms.
	SummaryRulesetVersion = "extractive/v1"

	// SummaryLevel1Separator and SummaryLevel2Separator head the two levels' text, so a
	// reader can tell which level a summary is without consulting its links.
	SummaryLevel1Separator = "Earlier in this conversation:"
	SummaryLevel2Separator = "Earlier in this episode:"
	// SummaryLevel3Separator heads the project rung's text.
	SummaryLevel3Separator = "Earlier in this project:"
)

// SummarizeRequest asks for a summary of one scope's unsummarised memories.
type SummarizeRequest struct {
	Scope Scope
	// Level is which rung to condense: 1 for messages, 2 for level-one summaries, 3 for the
	// project rung over episode summaries. Zero means level one, because that is the rung a
	// caller who did not think about it means.
	Level int
	// Window bounds how many memories the summary covers. Zero uses
	// DefaultSummaryWindow; the ceiling is MaxSummaryWindow.
	Window int
	// CreatedByID is the actor the summary is attributed to.
	CreatedByID string
	// Embed, when true, embeds the summary with the project's provider so the summary
	// channel is searchable by meaning. It is the caller's choice rather than always-on,
	// because a project with no embedding provider must still be able to summarise.
	Embed bool
}

// Summarize condenses a scope's unsummarised memories into one summary.
//
// It returns the summary it created, or found=false when there was nothing to summarise —
// which is the ordinary result of calling it twice in a row and is NOT an error: "there is
// nothing new to condense" is an answer.
func (s *Service) Summarize(ctx context.Context, request SummarizeRequest) (memory.MemoryItem, bool, error) {
	if !s.StorageAvailable() {
		return memory.MemoryItem{}, false, storageUnavailable()
	}
	if err := request.Scope.Validate(); err != nil {
		return memory.MemoryItem{}, false, err
	}
	level := request.Level
	if level == 0 {
		level = 1
	}
	if !memory.IsValidSummaryLevel(level) || level == 0 {
		// Refused rather than clamped: a caller that asked for a fourth rung has made a mistake,
		// and answering it with a third-rung summary would hide that.
		return memory.MemoryItem{}, false, memory.InvalidError("A summary is of messages, of episode summaries, or of a project's episodes.")
	}
	window := request.Window
	if window <= 0 {
		window = DefaultSummaryWindow
	}
	if window > MaxSummaryWindow {
		window = MaxSummaryWindow
	}

	// The scope a rung reads AND writes is its own, and each rung widens by one step: a
	// message-rung summary belongs to the conversation, an episode-rung one to the episode
	// (agent and session cleared), a project-rung one to the project (episode cleared too).
	//
	// Reading and writing the same scope is what makes a rung's window match its own rows. A
	// rung that read wider than it wrote would find a sibling's children and fold another
	// episode's summary into this one's.
	readScope := request.Scope
	switch level {
	case memory.SummaryLevelEpisode:
		readScope = request.Scope.EpisodeOnly()
	case memory.SummaryLevelProject:
		readScope = request.Scope.ProjectOnly()
	}
	sources, err := s.summarySources(ctx, level, readScope, window)
	if err != nil {
		return memory.MemoryItem{}, false, err
	}
	if len(sources) == 0 {
		return memory.MemoryItem{}, false, nil
	}
	// EVERY rung above the first needs at least two children: a summary of one summary is a
	// copy with an extra hop, and it would make the ladder longer without making it shorter.
	//
	// The floor does double duty on the project rung. It is the reason a project with one
	// episode produces no summary, and it is ALSO what kept the pre-WP-18 code from condensing
	// a stray pair — which was luck rather than design, and is why the window now names its
	// rung instead of relying on the floor.
	if level != memory.SummaryLevelMessage && len(sources) < 2 {
		return memory.MemoryItem{}, false, nil
	}

	summaryID, err := s.ids.New()
	if err != nil {
		return memory.MemoryItem{}, false, storageUnavailable()
	}
	now := s.now()
	summaryScope := request.Scope
	switch level {
	case memory.SummaryLevelEpisode:
		summaryScope = request.Scope.EpisodeOnly()
	case memory.SummaryLevelProject:
		summaryScope = request.Scope.ProjectOnly()
	}
	summary := memory.MemoryItem{
		ID:         summaryID,
		Type:       memory.TypeSummary,
		Scope:      summaryScope,
		Content:    renderSummary(level, sources, now),
		Importance: summaryImportance(sources),
		// Confidence is the MEAN of the sources', which is the honest summary of a set:
		// a condensation of one confident and one shaky memory is neither certain nor
		// worthless, and reporting the maximum would overstate it.
		Confidence: summaryConfidence(sources),
		// The rung is stated on the row rather than derived later: with three rungs the TYPE
		// cannot discriminate them, and a window that had to infer the level from the links
		// would be a recursive query whose answer changes as the ladder grows.
		SummaryLevel: level,
		// No source columns, because a summary cites MANY memories and the pair holds one. Its
		// provenance is the source table below, and the row's AgentKey names whose run produced it
		// so "role/agent/time 保留" (AC-MEM-004) is answerable without walking the links.
		AgentKey:  strings.TrimSpace(request.CreatedByID),
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	links := make([]memory.SummarySource, 0, len(sources))
	for index, source := range sources {
		links = append(links, memory.SummarySource{
			SummaryID:      summaryID,
			SourceMemoryID: source.ID,
			SourceOrder:    index + 1,
			CreatedAt:      now,
		})
	}
	// EVERY RUNG MARKS ITS SOURCES, and the flag means "a parent covers this" rather
	// than "a level-one summary covers this".
	//
	// The first version passed `level == 1`, with the reasoning that a level-two summary
	// should not mark its children because they are summaries themselves and remain the
	// right answer to a level-one question. The reasoning was right about RECALL and wrong
	// about the WRITE WINDOW: `UnsummarisedItems` is what chooses a summary's sources, and
	// it selects on `summarized = 0`. So a level-two run left its children unmarked, the
	// next run read the same rows, and a second, near-identical summary of the same episode
	// appeared — with the first summary nested inside it as a source. WP-18's
	// `TestASecondLevelTwoRunDoesNotRecondenseTheSameChildren` failed on exactly that, and
	// the printed text was the proof: a summary of a summary of the same two messages.
	//
	// Nothing is lost on the recall side, which is the part the original comment was
	// protecting: the semantic channel reads `VectorCandidates`, which filters on the
	// embedding columns and NOT on `summarized`, so a condensed summary is still searched
	// and still returned. The flag only ever filtered the summarise window.
	if err := s.items.CreateSummaryWithSources(ctx, summary, links, true); err != nil {
		return memory.MemoryItem{}, false, err
	}
	// The hierarchy edge, so a reader can walk from the child to its parent rather than
	// only from the parent down. It is section 14.3's entity link used for the relation
	// section 14.2's table cannot express.
	if level != memory.SummaryLevelMessage {
		entityLinks := make([]memory.EntityLink, 0, len(sources))
		for _, source := range sources {
			entityLinks = append(entityLinks, memory.EntityLink{
				MemoryID:     source.ID,
				EntityType:   "memory",
				EntityID:     summaryID,
				RelationType: memory.RelationSummarizes,
				CreatedAt:    now,
			})
		}
		if err := s.items.AddEntityLinks(ctx, entityLinks); err != nil {
			return memory.MemoryItem{}, false, err
		}
	}
	if request.Embed {
		// An embedding failure does not undo the summary. The text and its provenance are
		// the artifact; the vector is an index over it, and a summary that exists but is
		// not yet searchable is a recoverable state — the rebuild command is exactly the
		// path that recovers it. Reporting the failure to the caller would be right if the
		// caller could act on it, and the caller here cannot.
		if embedded, err := s.embedItems(ctx, readScope.Project, []memory.MemoryItem{summary}); err == nil && len(embedded) == 1 {
			summary = embedded[0]
		}
	}
	s.recordEvent(ctx, summary)
	return summary, true, nil
}

// summarySources reads what a summary at this level would cover.
func (s *Service) summarySources(ctx context.Context, level int, scope Scope, window int) ([]memory.MemoryItem, error) {
	// The window is always "the uncondensed rows of the rung below, in this rung's scope", and
	// the STORE decides which rows those are — the rung, not the caller, names the type and the
	// level predicate. Two defects in this function's history are why the read is shaped this way
	// and both are worth keeping:
	//
	//   - The first version asked for every type and filtered level two in Go, so a LEVEL-ONE
	//     window read summary rows too: the second run condensed the first summary into a new one
	//     and the `summarized` flag never cleared the window. The type became a parameter.
	//   - The second version passed `type = summary` for every rung above the first, which was
	//     correct for two rungs and wrong for three: a project window would accept an episode
	//     window's uncondensed summary, and with one sibling to satisfy the floor it condensed
	//     its own predecessor. The rung became a parameter.
	//
	// Level one's own window is bounded by `window`; every rung above it uses MaxSummaryParents,
	// because "how many children one summary covers" bounds the condensing rather than being a
	// property of one rung.
	if level == memory.SummaryLevelMessage {
		return s.items.UnsummarisedItemsForLevel(ctx, scope, level, window)
	}
	return s.items.UnsummarisedItemsForLevel(ctx, scope, level, MaxSummaryParents)
}

// renderSummary builds the summary's text from its sources.
//
// The rule, and it is the whole of the "extraction": the summary lists its sources in
// order, each as `role: content`, clipped. It adds nothing that is not in a source and
// drops nothing that fits, so a reader comparing the summary against the messages finds
// every line accounted for. That is what makes AC-MEM-004's provenance a checkable claim
// rather than a promise.
//
// The clipping is the one lossy step, and it is marked: a line that was cut ends with an
// ellipsis, so a reader knows the summary is not the whole of that source.
func renderSummary(level int, sources []memory.MemoryItem, now time.Time) string {
	separator := SummaryLevel1Separator
	switch level {
	case memory.SummaryLevelEpisode:
		separator = SummaryLevel2Separator
	case memory.SummaryLevelProject:
		separator = SummaryLevel3Separator
	}
	lines := []string{separator}
	for _, source := range sources {
		speaker := speakerOf(source)
		lines = append(lines, speaker+": "+clipLine(source.Content))
	}
	return strings.Join(lines, "\n")
}

// summaryLineBudget bounds one source's line in a summary.
//
// A summary exists to be SHORTER than what it covers, so each line is clipped well below
// the message bound. The number is small on purpose: forty sources at this length is a
// summary that fits comfortably in the memory layer's share of section 5.3's budget.
const summaryLineBudget = 160

// clipLine shortens one source's text, marking that it did.
func clipLine(content string) string {
	flattened := strings.Join(strings.Fields(content), " ")
	if len([]rune(flattened)) <= summaryLineBudget {
		return flattened
	}
	runes := []rune(flattened)
	return string(runes[:summaryLineBudget]) + "…"
}

// speakerOf names who a source's line belongs to.
//
// A message's role is the speaker; a summary's is "summary", because a level-two summary
// covering summaries is not quoting a person and must not look as though it were.
func speakerOf(source memory.MemoryItem) string {
	if source.Type == memory.TypeSummary {
		return "summary"
	}
	if source.Role != "" {
		return string(source.Role)
	}
	return "memory"
}

// summaryImportance takes the MAXIMUM of the sources'.
//
// Maximum rather than mean, and different from confidence below, for a reason worth
// stating: importance is a weight in section 12.2's fusion, and a summary that covers one
// crucial setting among twenty ordinary turns is still the way to reach that setting. A mean
// would bury it, and burying it is what summarising is supposed to prevent.
func summaryImportance(sources []memory.MemoryItem) float64 {
	highest := 0.0
	for _, source := range sources {
		if source.Importance > highest {
			highest = source.Importance
		}
	}
	if highest == 0 {
		return memory.DefaultImportance
	}
	return highest
}

// summaryConfidence takes the MEAN of the sources'.
func summaryConfidence(sources []memory.MemoryItem) float64 {
	if len(sources) == 0 {
		return memory.DefaultConfidence
	}
	total := 0.0
	for _, source := range sources {
		total += source.Confidence
	}
	return total / float64(len(sources))
}

// SummariesOf returns the summaries that cite a memory.
//
// It is the read that makes AC-MEM-004's deletion policy a behaviour rather than a claim:
// a caller deleting a memory asks this to find what has to be invalidated.
func (s *Service) SummariesOf(ctx context.Context, memoryID string) ([]memory.MemoryItem, error) {
	if !s.StorageAvailable() {
		return nil, storageUnavailable()
	}
	return s.items.SummariesOf(ctx, memoryID)
}

// ListSummarySources returns the memories one summary cites, in their stored order.
//
// This is AC-MEM-004's "UI 可跳原始消息": the identifiers come back with the source rows'
// order preserved, so the caller renders them in the order the summary reads, and each one
// can be fetched with GetMemory — which carries the role, the agent and the time, because
// those are the memory row's own fields rather than fields a summary had to remember to
// copy.
func (s *Service) ListSummarySources(ctx context.Context, summaryID string) ([]memory.SummarySource, []memory.MemoryItem, error) {
	if !s.StorageAvailable() {
		return nil, nil, storageUnavailable()
	}
	sources, err := s.items.ListSummarySources(ctx, summaryID)
	if err != nil {
		return nil, nil, err
	}
	memories := make([]memory.MemoryItem, 0, len(sources))
	for _, source := range sources {
		// A source that cannot be read is a HOLE, and it is reported as a hole rather than
		// skipped: a summary whose listed sources do not add up is exactly what AC-MEM-004's
		// deletion policy has to be visible in, so silently shrinking the list would hide
		// the thing the criterion is about.
		item, err := s.items.GetItem(ctx, source.SourceMemoryID)
		if err != nil {
			if notFound(err) {
				memories = append(memories, memory.MemoryItem{ID: source.SourceMemoryID, DeletedAt: time.Now().UTC()})
				continue
			}
			return nil, nil, err
		}
		memories = append(memories, item)
	}
	return sources, memories, nil
}
