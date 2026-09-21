package script

import (
	"context"
	"sort"
	"strings"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// enforcement.go holds the two rules that apply to EVERY version a stage or a person writes: a
// locked field must come back unchanged, and a version whose content is frozen may not be written
// at all.
//
// Both existed for the script structure alone until this file, and the gap had a named victim:
// AC-SCRIPT-002's scenario is a FIX on a STORY SKELETON whose ending hook is missing, so the lock
// the criterion turns on is a SKELETON field — and the skeleton write path enforced nothing. A user
// could pin an ending hook, ask for a FIX and receive a version with the hook rewritten, and every
// test would still pass, because the tests were about the other family.
//
// WHY ONE COMPARISON RATHER THAN THREE. The three families differ in their FIELDS and not in their
// RULE, and the rule is the part that must not diverge: "read the locks from the version the new
// one is based on, compare the named fields, refuse on a difference". Three copies would drift at
// exactly the point that matters — which version the locks are read from — and that mistake was
// already made once in the script write path, where the locks were read from the NEW version, so
// the set was always empty and nothing was ever enforced.

// readLocks reads the locks pinned on one version.
//
// It is called with the BASE version's id and never with the new one's, and that is the single
// invariant this file exists to keep. It is a named function rather than an inline call so that
// invariant has one place to be got right.
//
// The trim below is DEFENSIVE AND CURRENTLY REDUNDANT, which a mutation pass established: replacing
// it with the raw value kills no test, because every id that reaches here was already trimmed where it
// was STORED (see the three write paths' `BasedOnVersionID`). It is kept rather than removed for two
// reasons: a lookup key that must match a stored key is the wrong place to discover a caller's typo,
// and the failure it would hide is silent — an untrimmed id finds no locks, so the version reads as
// unlocked and a user's pin evaporates without an error. The cost of keeping it is one call.
func (s *Service) readLocks(ctx context.Context, versionID string) ([]scriptdomain.FieldLock, error) {
	base := strings.TrimSpace(versionID)
	if base == "" {
		// A first draft has no predecessor, so there is nothing a user could have pinned. This is
		// not a bypass: the id comes from the version ROW, so it is empty only when the row says the
		// version is a first version.
		return nil, nil
	}
	return s.repository.ListScriptFieldLocks(ctx, base)
}

// fieldValues is one version's lockable fields as comparable text.
//
// It REPLACES a reader function plus a separate "which fields does this comparison cover" map, and
// the replacement is the point: those two declarations could disagree, and when they did the failure
// was silent in the direction that matters — a map that omitted a field would leave a lock in the
// table that nothing checked. Here the VALUES ARE THE COVERAGE: a field is compared if and only if
// it is in the map, and `compareFieldSets` refuses a lock whose field is absent from either side. A
// field can therefore be added to a family's lock vocabulary and forgotten in its comparison, and
// the first write that meets the new lock refuses loudly instead of passing unchecked.
//
// The values are strings because every lockable field's comparison meaning is textual: a mode, a
// hook, a list of ids joined in a stable order. A field that is not one string — a script version's
// whole structure — is deliberately NOT in any map, and its family compares it separately.
type fieldValues map[scriptdomain.LockableField]string

// compareFieldSets refuses when a field locked on the base version came back different.
//
// This is AC-SCRIPT-002's "锁定字段不变". It is a refusal rather than a repair: a write path that
// silently restored the pinned values would produce a version the model did not write and nobody
// chose, and the record would claim the model produced content it did not.
//
// It reports the FIRST differing field rather than all of them, because the caller refuses the whole
// write: a list of further differences would describe a write that is not going to happen.
func compareFieldSets(locks []scriptdomain.FieldLock, family scriptdomain.VersionFamily, before, after fieldValues) error {
	for _, lock := range locks {
		if lock.Family != family {
			// A lock of another family cannot belong to this version, so this is a corrupt row rather
			// than a user's intent. Fail closed rather than skip: a lock that reads as a protection
			// and enforces nothing is worse than no lock, which is the rule the lock vocabulary
			// itself states.
			return scriptdomain.ConflictError(
				"A lock on this version names a different artifact family, so this write cannot be checked against it.")
		}
		oldValue, oldPresent := before[lock.Field]
		newValue, newPresent := after[lock.Field]
		if !oldPresent || !newPresent {
			// The lock names a field this comparison does not carry, so the lock vocabulary and the
			// comparison have diverged. Refusing makes that a visible failure; skipping would leave
			// a lock in the table that nothing enforces.
			return scriptdomain.ConflictError(
				"A field locked on this version is not one this write can compare, so the revision cannot be verified.")
		}
		if !scriptdomain.LocksEqual(lock.Field, oldValue, newValue) {
			return scriptdomain.ConflictError(
				"A locked field was changed: " + string(lock.Field) +
					" came back different from the version the user pinned.")
		}
	}
	return nil
}

// assertLocksCovered refuses a lock on a field no comparison in this write path handles.
//
// It is the guard for a family whose locks are split across two comparisons: a script version's
// structure is not a string, so its family compares the structure separately and hands only its
// string fields to `compareFieldSets`. `covered` must then be the union of the two, and this is the
// one place that states it — checked BEFORE the write rather than after, so a vocabulary addition
// that forgot its comparison fails the first write that meets it.
func assertLocksCovered(locks []scriptdomain.FieldLock, family scriptdomain.VersionFamily, covered []scriptdomain.LockableField) error {
	claimed := make(map[scriptdomain.LockableField]bool, len(covered))
	for _, field := range covered {
		claimed[field] = true
	}
	for _, lock := range locks {
		if lock.Family != family {
			return scriptdomain.ConflictError(
				"A lock on this version names a different artifact family, so this write cannot be checked against it.")
		}
		if !claimed[lock.Field] {
			return scriptdomain.ConflictError(
				"A field locked on this version is not one this write can compare, so the revision cannot be verified.")
		}
	}
	return nil
}

// joinedEvents renders a set of event identifiers as one comparable value.
//
// The ids are SORTED rather than taken in order, and the difference matters: a skeleton that
// selected the same events and wrote them in a different order has not changed which events it
// selects, and refusing that would make a lock unsatisfiable for a reason no user asked for. Order
// among selected events is the ADAPTATION's business, and the strategy's link table is where the
// order is stated.
func joinedEvents(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return strings.Join(sorted, "\n")
}

// renderTreatments renders a strategy's per-event decisions as one comparable value.
//
// The events are SORTED by identifier and the treatments travel with them, so the value states
// WHICH event got WHICH treatment and not the order they were listed in. Ordinals are deliberately
// excluded: a reorder is already visible as a treatment, and comparing positions would refuse a
// revision that re-stored its list without changing any event's treatment — a change no reader of
// the strategy would see.
func renderTreatments(links []scriptdomain.StrategyEventLink) string {
	rendered := make([]string, 0, len(links))
	for _, link := range links {
		rendered = append(rendered, strings.TrimSpace(link.StoryEventID)+"="+string(link.Treatment))
	}
	sort.Strings(rendered)
	return strings.Join(rendered, "\n")
}

// assertWritable refuses a version whose content is frozen (§2.5).
//
// An approved or superseded version's content is final, and a change is a NEW version rather than
// an edit. WP-05 wrote `versioning.IsContentFrozen` and recorded that nothing called it yet
// ("WP-08 introduces the script-edit path that will call it"); the write paths in this package are
// its callers.
func assertWritable(status versioning.Status) error {
	if versioning.IsContentFrozen(status) {
		return scriptdomain.ConflictError(
			"That version is approved or superseded, so its content cannot be rewritten. Author a new version instead.")
	}
	return nil
}
