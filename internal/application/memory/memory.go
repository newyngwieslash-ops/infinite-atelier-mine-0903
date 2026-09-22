// Package memory is the recall port of AGENT_CONTRACTS section 12 and
// DOMAIN_MODEL section 14.
//
// WP-07 built the seam and one real implementation of it: a Recent window over the
// agent_messages table the runtime already writes, with section 14.5's two silent
// invariants (当前消息不召回自身 and 其他项目内容不可召回) enforced rather than left to
// the store. WP-10 kept that and ADDED the rest the section asks for: the memory store,
// the summary chain, the float32 vector index, the scored recall with a threshold and a
// token budget, deep recall, and the user's view/pin/edit/delete/rebuild commands.
//
// # Two stores rather than one, and why
//
// The Recent channel still reads agent_messages. That table is the agent RUNTIME's
// transcript — one row per turn of one run, cascade-deleted with the run — and it is the
// right source for "what was just said" because it is the thing that was just said. The
// memory store (migration 000019's memory_items) is the USER's record, with a lifecycle
// the transcript does not have: pinned, edited, deleted, re-embedded. ADR-0014 records the
// ruling; what it means here is that `Store` below and `Repository` in ports.go are two
// ports over two tables, and neither is an implementation of the other.
//
// So the channels of section 12.1 are:
//
//	recent    -> Store.RecentMessages      (agent_messages, this file's BuildContext)
//	summaries -> Repository + VectorIndex  (memory_items of type summary)
//	semantic  -> Repository + VectorIndex  (memory_items of any type, thresholded)
//	facts     -> the context builder's structured channel (NOT memory: section 12.2's
//	             "Project Rule/Event Graph 通过结构化事实通道注入，不混作 Memory")
package memory

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	domainmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// Scope is one conversation's memory scope (DOMAIN_MODEL section 14.4).
//
// It is the DOMAIN's type, aliased rather than restated. A second definition here would be
// a second place for the six parts and their order to live, and the whole point of section
// 14.4's structural isolation is that exactly one shape decides which conversation a
// memory belongs to.
type Scope = domainmemory.Scope

// Error is the DOMAIN's memory error, aliased for the same reason: a caller that switches
// on the category should be reading one taxonomy, not two that happen to agree today.
type Error = domainmemory.Error

// Item is one recalled message.
//
// It is a view of an agent_messages row rather than a memory item, which is why it has a
// Provenance field and no importance: the transcript's rows are what the Recent channel
// reads, and they carry the run they belong to (section 12.2's requirement that recalled
// context says where it came from).
type Item struct {
	MessageID  string
	Role       agent.MessageRole
	Content    string
	Provenance string
	CreatedAt  time.Time
}

// RecallRequest is what to recall.
type RecallRequest struct {
	Scope Scope
	// ExcludeMessageID is the message being answered. Section 12.2's order is
	// "recall previous memory excluding current turn", and excluding it is what
	// keeps a turn from recalling itself.
	ExcludeMessageID string
	// Limit bounds how many messages come back. Zero uses DefaultLimit.
	Limit int
}

// DefaultLimit is how many messages a recall returns when the caller states none.
//
// It is small on purpose. Section 5.3's budget puts memory seventh of nine
// priorities, so a recent window is what fits.
const DefaultLimit = 20

// MaxLimit bounds one recall, so a caller cannot ask for an unbounded window.
const MaxLimit = 200

// Store is the recall side of the TRANSCRIPT: the messages a scope has.
//
// One method, and it is deliberately not a general query interface. Recall asks
// "what did this conversation recently say", and a store that took a filter
// expression would put query construction in the caller.
type Store interface {
	// RecentMessages returns a scope's messages newest first, excluding one message.
	RecentMessages(ctx context.Context, scopeParts [6]string, excludeMessageID string, limit int) ([]Item, error)
}

// Service builds memory context for a run and owns the memory store's commands.
//
// The two capabilities have separate availability: `Available` is the Recent channel's,
// which needs only the transcript port, and `StorageAvailable` is the memory store's. They
// are separate because they are separate compositions: a build with a transcript and no
// memory store can still recall what was just said — which is what WP-07 shipped — and a
// caller that needs the second has to be able to tell that it is missing rather than get an
// empty result that reads like "nothing is remembered".
type Service struct {
	store    Store
	items    Repository
	vectors  VectorIndex
	embedder Embedder
	clock    Clock
	ids      IDGenerator
}

// Options configures the service.
//
// `Store` is required for the Recent channel; the rest are required for the memory store's
// commands. That split is the composition's to state, and both halves are checked by the
// method that needs them rather than here, because a constructor that refused a partial set
// would make the WP-07 composition impossible.
type Options struct {
	Store    Store
	Items    Repository
	Vectors  VectorIndex
	Embedder Embedder
	Clock    Clock
	IDs      IDGenerator
}

// NewService builds the full service.
func NewService(options Options) *Service {
	return &Service{
		store:    options.Store,
		items:    options.Items,
		vectors:  options.Vectors,
		embedder: options.Embedder,
		clock:    options.Clock,
		ids:      options.IDs,
	}
}

// New builds a Service with only the transcript port, which is the WP-07 composition and
// still a valid one: it recalls what was just said and refuses everything that needs a
// memory store.
func New(store Store) *Service {
	return NewService(Options{Store: store})
}

// Available reports whether the Recent channel can run.
func (s *Service) Available() bool {
	return s != nil && s.store != nil
}

// StorageAvailable reports whether the memory store's commands can run.
//
// It requires the clock and the identifier generator as well as the repository, because
// every command here writes a timestamp and mints an identifier, and a service that could
// reach the store but not mint an id would fail halfway through a write.
func (s *Service) StorageAvailable() bool {
	return s != nil && s.items != nil && s.clock != nil && s.ids != nil
}

// SemanticAvailable reports whether the scored channels can run.
//
// It is a THIRD question, and the distinction is the honest one PRD FR-120 asks for:
// "Embedding Provider 可替换；本地模式不得在未授权时上传项目文本". A project with no
// embedding provider configured has a perfectly good recent and summary memory and no
// semantic search, and a caller has to be able to say which of those it has rather than
// being told the whole feature is off.
func (s *Service) SemanticAvailable(ctx context.Context, projectID string) bool {
	if s == nil || s.vectors == nil || s.embedder == nil {
		return false
	}
	return s.embedder.Available(ctx, projectID)
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// BuildRecent recalls the recent messages for a scope, oldest first.
//
// The result is ordered oldest first because that is how a prompt reads a
// conversation: newest-first is how the store retrieves, and reversing here once
// means a caller does not have to remember which end it got.
//
// The recall is bounded and its failure is REPORTED rather than swallowed. A
// caller that cannot recall has two defensible choices — continue without memory,
// or stop — and that choice is the caller's, so this does not make it. What it
// does do is refuse a scope that would recall across projects, because that is not
// a choice: section 14.5 forbids it.
func (s *Service) BuildRecent(ctx context.Context, request RecallRequest) ([]Item, error) {
	if !s.Available() {
		return nil, UnavailableError()
	}
	if err := request.Scope.Validate(); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	items, err := s.store.RecentMessages(ctx, request.Scope.Parts(), request.ExcludeMessageID, limit)
	if err != nil {
		return nil, err
	}
	// Oldest first, which is the order a prompt reads.
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items, nil
}

// BuildContext recalls the recent messages for a scope, oldest first.
//
// It is BuildRecent's original name, kept because it is the one AGENT_CONTRACTS section
// 12.1 uses and the one the tool handler and the runtime are written against. WP-10's
// multi-channel assembly is BuildMemoryContext, below, which is a different operation with
// a different request and a different result.
func (s *Service) BuildContext(ctx context.Context, request RecallRequest) ([]Item, error) {
	return s.BuildRecent(ctx, request)
}

// InvalidError reports a scope or request the domain refuses.
func InvalidError(message string) *Error {
	return domainmemory.InvalidError(message)
}

// UnavailableError reports that no transcript store is composed.
func UnavailableError() *Error {
	return &Error{Category: domainmemory.CategoryStorage, SafeMessage: "No memory store is configured, so nothing can be recalled."}
}

// ScopeFor builds a scope from a run's identifiers.
//
// It is the one place the mapping from a run to a scope is written, so recall and
// persistence cannot disagree about which conversation a message belongs to.
func ScopeFor(projectID, episodeID, agentKey string) Scope {
	// The tenant is "local" because this is a single-machine desktop build. Section
	// 14.4 lists the field so a future multi-tenant deployment has somewhere to put
	// its value rather than a shape to change.
	return Scope{Tenant: "local", Project: projectID, Episode: episodeID, AgentKey: agentKey}
}

// trimOrEmpty trims a caller-supplied identifier.
func trimOrEmpty(value string) string { return strings.TrimSpace(value) }
