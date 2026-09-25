// Package memory is the persistent memory aggregate of DOMAIN_MODEL section 14 and
// PRD FR-120.
//
// The four shapes are that section's, field for field: MemoryItem (14.1),
// MemorySummarySource (14.2), MemoryEntityLink (14.3) and the six-part Scope (14.4).
// Section 14.5's invariants are here as functions rather than as prose, because each
// one is a rule a caller can get wrong in a way that leaves no trace:
//
//   - 当前消息不召回自身 and 其他项目内容不可召回 are enforced by the recall path,
//     which asks a store by the scope's structured parts (see application/memory).
//   - locked memory 只有用户可修改/删除 is CanModifyMemory, below.
//   - embedding 模型变化不覆盖旧向量 is EmbeddingIsCurrent, below.
//   - Semantic Memory 不自动覆盖 Event Graph is why this package has no reference to
//     the story graph at all: a fact about a character is a StoryFact, and a memory
//     about one cites it through an EntityLink rather than becoming it.
//
// WHY THIS IS A SEPARATE TYPE FROM agent.AgentMessage, which is the question the
// first reader of this file will have. An AgentMessage is the agent RUNTIME's
// transcript: one row per turn of one run, cascade-deleted with that run. A MemoryItem
// is the USER's record, and section 14.5 gives the user rights over it that a
// transcript does not have — to pin it, edit it, delete it, rebuild its embedding.
// Storing locked and embedding_blob on the transcript would make "delete this memory"
// a write to the runtime's own record. The episodic item cites the message it came
// from through SourceType/SourceID, which is what section 14.1's source columns are
// for. ADR-0014 records the ruling and its cost.
package memory

import (
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// MemoryType is what kind of memory an item is (DOMAIN_MODEL section 14.1, PRD FR-120).
type MemoryType string

const (
	// TypeEpisodic is what happened: turns of a conversation, in order.
	TypeEpisodic MemoryType = "episodic"
	// TypeSemantic is a preference or fact a USER approved. Section 14.5 forbids the
	// agent from promoting its own candidates into it, which is why nothing in this
	// build writes one except a user command and the pipeline's own summaries of
	// decisions that were already decided.
	TypeSemantic MemoryType = "semantic"
	// TypeProcedural is how the user works.
	TypeProcedural MemoryType = "procedural"
	// TypeArtifact is a reference to an entity or a file — never a copy of one. Section
	// 14.5: "Artifact Memory 只引用实体/文件，不复制二进制".
	TypeArtifact MemoryType = "artifact"
	// TypeSummary is a hierarchical summary of other memories.
	TypeSummary MemoryType = "summary"
)

// MemoryTypes lists the documented types in section 14.1's order.
var MemoryTypes = []MemoryType{TypeEpisodic, TypeSemantic, TypeProcedural, TypeArtifact, TypeSummary}

// IsValidMemoryType reports whether a memory type may be persisted.
func IsValidMemoryType(value MemoryType) bool {
	for _, candidate := range MemoryTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// SourceType is what a memory was derived from (DOMAIN_MODEL section 14.1).
//
// It is a closed vocabulary rather than a free string because the recall path and the
// UI both switch on it: a source a reader cannot follow is a provenance that says
// nothing.
type SourceType string

const (
	// SourceUnset is a memory with no upstream row, which is what a user's own typed
	// preference is.
	SourceUnset SourceType = ""
	// SourceMessage cites an agent_messages row.
	SourceMessage SourceType = "message"
	// SourceSummary cites another memory item of type summary.
	SourceSummary SourceType = "summary"
	// SourceArtifact cites a version or an asset row.
	SourceArtifact SourceType = "artifact"
	// SourceUser cites nothing, and says so: the user typed it.
	SourceUser SourceType = "user"
	// SourceAgentRun cites the run a memory was derived from.
	SourceAgentRun SourceType = "agent_run"
)

// SourceTypes lists the documented source kinds in the schema's order.
var SourceTypes = []SourceType{SourceUnset, SourceMessage, SourceSummary, SourceArtifact, SourceUser, SourceAgentRun}

// IsValidSourceType reports whether a source type may be persisted.
func IsValidSourceType(value SourceType) bool {
	for _, candidate := range SourceTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// ActorType is who is acting on a memory. Section 14.5 gives the user rights over a
// locked memory that no agent has, so the actor is part of the rule rather than of the
// caller's bookkeeping.
type ActorType string

const (
	ActorUser   ActorType = "user"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

// ActorTypes lists the documented actors in the schema's order.
var ActorTypes = []ActorType{ActorUser, ActorAgent, ActorSystem}

// IsValidActorType reports whether an actor may act on a memory.
//
// The check lives here rather than at each command because an actor nobody recognises would
// fall through CanModifyMemory's `actor != ActorUser` test and be treated as an agent — which
// is the SAFE direction, but silently. Refusing an unknown actor makes the caller state who it
// is.
func IsValidActorType(value string) bool {
	for _, candidate := range ActorTypes {
		if string(candidate) == value {
			return true
		}
	}
	return false
}

// Importance bounds and defaults (PRD FR-120's "importance、recency、semantic、role 权重融合").
const (
	// DefaultImportance is what an item gets when nothing scored it. It is the middle of
	// the range because section 12.2's fusion treats importance as a weight, and a
	// default of 1.0 would make every unscored memory look critical.
	DefaultImportance = 0.5
	// DefaultConfidence is the same middle value for the same reason.
	DefaultConfidence = 0.5
	// HighImportance is what counts as "high-importance" for section 12.2's separate
	// channel: "高重要性 locked memory" is recalled regardless of the similarity
	// threshold, and AC-MEM-003 names that exception explicitly.
	HighImportance = 0.8
)

// Scope is one conversation's memory scope (DOMAIN_MODEL section 14.4).
//
// The fields are that section's, in its order, and they are separate fields rather
// than one encoded string because section 14.4 requires retrieval to filter by
// structure: a store that matched a prefix would return another project's memories
// whenever one project's identifier happened to begin with another's.
type Scope struct {
	Tenant    string
	Workspace string
	Project   string
	Episode   string
	AgentKey  string
	Session   string
}

// Validate checks a scope before it is used to recall or to write anything.
//
// A project is required and everything else is optional, because every drama query is
// project-scoped: a scope without one would recall across projects, which is the leak
// section 14.5 forbids.
func (s Scope) Validate() error {
	if strings.TrimSpace(s.Project) == "" {
		return InvalidError("A memory scope must name its project.")
	}
	if strings.ContainsAny(s.Tenant+s.Workspace+s.Project+s.Episode+s.AgentKey+s.Session, "\x00") {
		return InvalidError("A memory scope cannot contain a control character.")
	}
	return nil
}

// Key renders the scope as the stable string the schema stores.
//
// It is a display and indexing convenience: the parts travel as their own columns and a
// query filters on those, so a change to this encoding cannot change what is recalled.
// The separator cannot appear in a scope part, so two different scopes cannot render to
// one key.
func (s Scope) Key() string {
	return strings.Join([]string{s.Tenant, s.Workspace, s.Project, s.Episode, s.AgentKey, s.Session}, "|")
}

// Parts returns the scope's six parts in section 14.4's order, so a repository can
// store them without parsing the key.
func (s Scope) Parts() [6]string {
	return [6]string{s.Tenant, s.Workspace, s.Project, s.Episode, s.AgentKey, s.Session}
}

// ProjectOnly returns the scope widened to its project, with the episode, agent and
// session cleared.
//
// It is how a PROJECT-level summary is scoped: PRD FR-120's hierarchy is
// message → episode/session → project, and the level a summary sits at is the scope it
// carries. Widening is not a leak here because it stays inside one project.
func (s Scope) ProjectOnly() Scope {
	return Scope{Tenant: s.Tenant, Workspace: s.Workspace, Project: s.Project}
}

// EpisodeOnly returns the scope widened to its episode, with the agent and session
// cleared, keeping the project.
func (s Scope) EpisodeOnly() Scope {
	return Scope{Tenant: s.Tenant, Workspace: s.Workspace, Project: s.Project, Episode: s.Episode}
}

// MemoryItem is one memory (DOMAIN_MODEL section 14.1).
type MemoryItem struct {
	ID   string
	Type MemoryType
	// Scope is the six-part scope the item lives in. A zero Episode or AgentKey is a
	// value, not a wildcard: the schema stores what it is given and the query treats an
	// empty part as "matches the empty part". Widening is what ProjectOnly and
	// EpisodeOnly are for, so it is always an explicit act.
	Scope Scope
	// Role is who said it, for an episodic item. Empty for the other types.
	Role agent.MessageRole
	// AgentKey is the agent that produced it, kept apart from Scope.AgentKey because a
	// project-level summary has no agent in its scope and still names the agent whose
	// run produced it.
	AgentKey string
	// Content is the text. PRD FR-120's "Artifact Memory 只引用实体/文件，不复制二进制"
	// means an artifact item's content is a reference rendered as text, never bytes.
	Content    string
	Importance float64
	Confidence float64
	// EmbeddingBlob is the float32 vector, little-endian, or nil when nothing embedded
	// it. It is a BLOB rather than a JSON array because PRD FR-120 forbids the array
	// form: "向量使用 FLOAT32/BLOB 或向量索引，不使用 JSON 数组长期存储".
	EmbeddingBlob    []byte
	EmbeddingModel   string
	EmbeddingVersion string
	EmbeddedAt       time.Time
	// Summarized marks an item that a summary already covers, which is what keeps the
	// summary window from growing without bound.
	Summarized bool
	// SummaryLevel is which rung of FR-120's ladder a SUMMARY row sits on: 1 for a summary of
	// messages, 2 for a summary of level-one summaries, 3 for the project rung. Zero means
	// "not a summary", which is what every other type carries.
	//
	// It is a field rather than something derived from the type, and the reason is that the
	// TYPE stopped discriminating at three rungs: level two and level three both read
	// SUMMARY rows, so "the uncondensed summaries in this scope" would return a level-two
	// summary to a level-three window whenever one was uncondensed. With two rungs that
	// could not happen, and the accident is recorded in ADR-0022 rather than left as a
	// property later readers would have to rediscover.
	SummaryLevel int
	// Locked is the user's pin. Section 14.5: only the user may change or delete it.
	Locked     bool
	SourceType SourceType
	SourceID   string
	// DeletedAt is the soft delete. It is a timestamp rather than a flag so the policy
	// AC-MEM-004 asks about ("删除/失效行为符合策略") can be stated in terms of when.
	DeletedAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int
}

// The summary ladder's rungs (PRD FR-120: message → episode/session → project).
//
// They are named constants rather than bare integers because three different call sites have to
// agree about them: the summarise command, the window that chooses a rung's sources, and the UI that
// lets a user pick one. A magic 3 in any of those is a rung that can drift.
const (
	// SummaryLevelMessage condenses EPISODIC memories, in one agent's conversation scope.
	SummaryLevelMessage = 1
	// SummaryLevelEpisode condenses level-one summaries, in one episode's scope.
	SummaryLevelEpisode = 2
	// SummaryLevelProject condenses episode summaries, in the project's scope.
	SummaryLevelProject = 3
)

// SummaryLevels lists the ladder's rungs in order.
var SummaryLevels = []int{SummaryLevelMessage, SummaryLevelEpisode, SummaryLevelProject}

// IsValidSummaryLevel reports whether a level may be persisted.
//
// Zero is accepted because it is what every non-summary row carries, and the caller that needs to
// refuse a missing level says so itself — the two facts are different and this predicate answers
// only the first.
func IsValidSummaryLevel(value int) bool {
	if value == 0 {
		return true
	}
	for _, candidate := range SummaryLevels {
		if candidate == value {
			return true
		}
	}
	return false
}

// MaxContentLength bounds one memory's text.
//
// It exists because content is rendered into a prompt: an unbounded item would let one
// memory crowd out every layer above it in section 5.3's budget. The number is the
// message bound the runtime already uses, so a memory is never larger than the turn it
// came from.
const MaxContentLength = agent.DefaultMessageContentBytes

// Validate checks a memory before it is stored.
func (m MemoryItem) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return InvalidError("A memory needs an identifier.")
	}
	if err := m.Scope.Validate(); err != nil {
		return err
	}
	if !IsValidMemoryType(m.Type) {
		return InvalidError("The memory type is not recognised.")
	}
	if m.Role != "" && !agent.IsValidMessageRole(m.Role) {
		return InvalidError("The memory role is not recognised.")
	}
	if !IsValidSourceType(m.SourceType) {
		return InvalidError("The memory source type is not recognised.")
	}
	// The level and the type have to agree, and the rule is stated in both directions: a
	// summary with no level would be invisible to every window, and a non-summary with one
	// would let an episodic row be mistaken for a rung.
	if m.Type == TypeSummary {
		if !IsValidSummaryLevel(m.SummaryLevel) || m.SummaryLevel == 0 {
			return InvalidError("A summary must state which level of the ladder it is.")
		}
	} else if m.SummaryLevel != 0 {
		return InvalidError("Only a summary carries a summary level.")
	}
	if m.Importance < 0 || m.Importance > 1 {
		return InvalidError("A memory's importance is a weight between zero and one.")
	}
	if m.Confidence < 0 || m.Confidence > 1 {
		return InvalidError("A memory's confidence is a weight between zero and one.")
	}
	if len([]rune(m.Content)) > MaxContentLength {
		return InvalidError("The memory is larger than one message may be.")
	}
	// A citation with no referent, or a referent with no kind, is a provenance a reader cannot
	// follow — which is the same defect as having none.
	//
	// A SUMMARY is the one type that cites MANY things, and the source columns are a single pair,
	// so it cites nothing here and its provenance lives in memory_summary_sources instead. The
	// first version of this rule had no such case: it demanded a source id of every non-empty
	// source type, so every summary failed validation, the summary table stayed empty, and both
	// AC-MEM-004 and AC-MEM-005 had no runnable path. The rule is stated per type now.
	//
	// The consequence to keep in mind is that "this row is a summary" is implied by its type
	// rather than by its source columns, and the SOURCES are the relation table — which is what
	// section 14.2 calls them.
	citesOne := strings.TrimSpace(m.SourceID) != ""
	namesKind := m.SourceType != SourceUnset
	if m.Type == TypeSummary {
		if citesOne || namesKind {
			return InvalidError("A summary cites its sources through the summary source table, not through the source columns.")
		}
	} else if citesOne != namesKind {
		return InvalidError("A memory must name both what it came from and which kind of thing that is.")
	}
	// An embedding that names no model cannot be searched: the vector index selects
	// candidates by model and version (section 14.5's rebuild switches the index
	// version), so a blob with no model would be invisible to every search.
	if len(m.EmbeddingBlob) > 0 {
		if strings.TrimSpace(m.EmbeddingModel) == "" || strings.TrimSpace(m.EmbeddingVersion) == "" {
			return InvalidError("An embedded memory must name the model and version that produced the vector.")
		}
	}
	return nil
}

// Deleted reports whether the item has been soft-deleted.
func (m MemoryItem) Deleted() bool { return !m.DeletedAt.IsZero() }

// EmbeddingIsCurrent reports whether the item's vector came from the named model and
// version.
//
// Section 14.5: "embedding 模型变化不覆盖旧向量，重建后切换索引版本". This is the
// predicate the vector search filters on, so a row left on an older model is simply not
// a candidate for the new one instead of being silently compared against vectors from a
// different space.
func (m MemoryItem) EmbeddingIsCurrent(model, version string) bool {
	if len(m.EmbeddingBlob) == 0 {
		return false
	}
	return m.EmbeddingModel == model && m.EmbeddingVersion == version
}

// IsLockedHighImportance reports whether the item is in section 12.2's separate channel:
// a locked memory of high importance is recalled regardless of the similarity threshold.
//
// Locked AND high, not either. Locking is the user saying "keep this"; importance is how
// much it mattered. AC-MEM-003 names the exception as "locked high-importance memory",
// and a channel that fired on either one would recall every pinned triviality.
func (m MemoryItem) IsLockedHighImportance() bool {
	return m.Locked && m.Importance >= HighImportance
}

// CanModifyMemory reports whether an actor may change or delete a memory.
//
// Section 14.5: "locked memory 只有用户可修改/删除". The rule is on the item and the
// actor together rather than on the caller, because a caller that forgot to check is
// exactly the case the invariant exists for.
func CanModifyMemory(item MemoryItem, actor ActorType) error {
	if item.Locked && actor != ActorUser {
		return ConflictError("That memory is pinned, so only the user can change or delete it.")
	}
	return nil
}

// SummarySource links a summary to one of the memories it summarises (DOMAIN_MODEL
// section 14.2).
//
// The schema's column is `source_memory_id`, and this build's summaries are of
// MESSAGES: an episodic memory is the message's memory-side twin, so a source row
// points at the episodic item, which in turn cites the agent_messages row through
// SourceID. AC-MEM-004's "Summary 关联源消息表" is therefore satisfied by two hops that
// are both stored relations rather than by a name that has to be resolved.
type SummarySource struct {
	SummaryID      string
	SourceMemoryID string
	SourceOrder    int
	CreatedAt      time.Time
}

// Validate checks one source link.
func (s SummarySource) Validate() error {
	if strings.TrimSpace(s.SummaryID) == "" || strings.TrimSpace(s.SourceMemoryID) == "" {
		return InvalidError("A summary source must name both the summary and the memory it came from.")
	}
	if s.SummaryID == s.SourceMemoryID {
		return InvalidError("A summary cannot cite itself as its own source.")
	}
	if s.SourceOrder < 1 {
		return InvalidError("A summary source needs a position, which is what keeps the summary reproducible.")
	}
	return nil
}

// EntityLink is one edge from a memory to a domain entity (DOMAIN_MODEL section 14.3).
type EntityLink struct {
	MemoryID     string
	EntityType   string
	EntityID     string
	RelationType string
	CreatedAt    time.Time
}

// The relation vocabulary this build writes.
//
// It is deliberately small and closed here rather than free text everywhere: a link
// whose relation nothing understands is a link no reader can follow. The three are the
// ones the recall path and the UI actually ask for.
const (
	// RelationSummarizes connects a summary to the summary or memory it condenses. This
	// is PRD FR-120's hierarchy ("message → episode/session → project") expressed as
	// section 14.3's entity link, rather than as a self-referencing column on the source
	// table.
	RelationSummarizes = "summarizes"
	// RelationAbout connects a memory to the story entity it is about.
	RelationAbout = "about"
	// RelationReferences connects an artifact memory to the version it names, which is
	// AC-E2E-005's "返回对应角色/资产事实".
	RelationReferences = "references"
)

// EntityRelations lists the documented relations in the schema's order.
var EntityRelations = []string{RelationSummarizes, RelationAbout, RelationReferences}

// IsValidEntityRelation reports whether a relation may be persisted.
func IsValidEntityRelation(value string) bool {
	for _, candidate := range EntityRelations {
		if candidate == value {
			return true
		}
	}
	return false
}

// MaxEntityTypeLength and MaxEntityIDLength are the schema's bounds.
const (
	MaxEntityTypeLength = 60
	MaxEntityIDLength   = 200
)

// Validate checks one entity link.
func (e EntityLink) Validate() error {
	if strings.TrimSpace(e.MemoryID) == "" {
		return InvalidError("An entity link must name the memory it starts from.")
	}
	if trimmed := strings.TrimSpace(e.EntityType); trimmed == "" || len([]rune(trimmed)) > MaxEntityTypeLength {
		return InvalidError("An entity link must name the kind of entity it points at.")
	}
	if trimmed := strings.TrimSpace(e.EntityID); trimmed == "" || len([]rune(trimmed)) > MaxEntityIDLength {
		return InvalidError("An entity link must name the entity it points at.")
	}
	if !IsValidEntityRelation(e.RelationType) {
		return InvalidError("The entity relation is not recognised.")
	}
	return nil
}
