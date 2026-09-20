// Package memory is the recall port of AGENT_CONTRACTS section 12 and
// DOMAIN_MODEL section 14, at the scope WP-07 owns.
//
// The roadmap's scope item 15 is "基础 Memory Port，暂可只 Recent" — a basic memory
// port, recent-only for now — and this package takes that literally. It defines
// the port, implements recent recall over the agent_messages table that the runtime
// already writes, and deliberately does NOT build the memory store, the summary
// chain or the vector index: those are WP-10's, and building them now would mean
// guessing at a schema that package is meant to design.
//
// So what is here is the seam plus one real implementation of it. That is enough
// for two things the specification requires today — section 12.2's recall ORDER
// (recall before persisting the current message, so a query never recalls itself)
// and section 14.4's scope isolation — and nothing more.
//
// Two invariants from section 14.5 are enforced here rather than left to the
// store, because they are the two that a wrong implementation would violate
// silently:
//
//   - 当前消息不召回自身. A recall excludes the message being answered, which is why
//     BuildContext takes the current turn's identifier rather than trusting a
//     caller to filter afterwards.
//   - 其他项目内容不可召回. The scope is an exact match on the project, not a prefix
//     of an encoded string, because section 14.4 says retrieval must filter by
//     structure.
package memory

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// Scope is one conversation's memory scope (DOMAIN_MODEL section 14.4).
//
// The fields are that section's, in its order, and they are separate fields rather
// than one encoded string because section 14.4 requires retrieval to filter by
// structure: a store that matched a prefix would return another project's messages
// whenever one project's identifier happened to begin with another's.
type Scope struct {
	Tenant    string
	Workspace string
	Project   string
	Episode   string
	AgentKey  string
	Session   string
}

// Validate checks a scope before it is used to recall anything.
//
// A project is required and everything else is optional, because every drama query
// is project-scoped: a scope without one would recall across projects, which is the
// leak section 14.5 forbids.
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
// It is a display and indexing convenience: the parts travel as their own columns
// and a query filters on those, so a change to this encoding cannot change what is
// recalled. The separator cannot appear in a scope part, so two different scopes
// cannot render to one key.
func (s Scope) Key() string {
	return strings.Join([]string{s.Tenant, s.Workspace, s.Project, s.Episode, s.AgentKey, s.Session}, "|")
}

// Parts returns the scope's six parts in section 14.4's order, so a repository can
// store them without parsing the key.
func (s Scope) Parts() [6]string {
	return [6]string{s.Tenant, s.Workspace, s.Project, s.Episode, s.AgentKey, s.Session}
}

// Item is one recalled memory.
//
// Provenance is the run the message came from, which section 12.2 requires recalled
// context to carry so a reader can tell where it came from.
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
// priorities, so a recent window is what fits; a larger recall belongs to WP-10's
// scored retrieval, which will have a threshold and a token budget to spend.
const DefaultLimit = 20

// MaxLimit bounds one recall, so a caller cannot ask for an unbounded window.
const MaxLimit = 200

// Store is the recall side: the messages a scope has.
//
// One method, and it is deliberately not a general query interface. Recall asks
// "what did this conversation recently say", and a store that took a filter
// expression would put query construction in the caller.
type Store interface {
	// RecentMessages returns a scope's messages newest first, excluding one message.
	RecentMessages(ctx context.Context, scopeParts [6]string, excludeMessageID string, limit int) ([]Item, error)
}

// Service builds memory context for a run.
type Service struct {
	store Store
}

// New builds a Service.
func New(store Store) *Service {
	return &Service{store: store}
}

// Available reports whether recall can run.
func (s *Service) Available() bool {
	return s != nil && s.store != nil
}

// BuildContext recalls the recent messages for a scope, oldest first.
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
func (s *Service) BuildContext(ctx context.Context, request RecallRequest) ([]Item, error) {
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

// Error is a memory refusal.
type Error struct {
	SafeMessage string
	Cause       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// InvalidError reports a scope or request the domain refuses.
func InvalidError(message string) *Error {
	return &Error{SafeMessage: message}
}

// UnavailableError reports that no store is composed.
func UnavailableError() *Error {
	return &Error{SafeMessage: "No memory store is configured, so nothing can be recalled."}
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
