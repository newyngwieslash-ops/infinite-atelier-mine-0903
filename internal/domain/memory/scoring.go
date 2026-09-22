package memory

import (
	"math"
	"time"
)

// Score fusion, which AGENT_CONTRACTS section 12.2 states as a formula rather than as
// prose:
//
//	0.55 semantic
//	0.20 recency
//	0.15 importance
//	0.10 agent role
//
// The weights are constants here and the function is pure, for the reason the whole
// scoring rule is worth having in one place: a caller that re-derived these weights would
// be a second statement of the same policy, and the two would drift the first time one
// was tuned.
const (
	// WeightSemantic is how much a candidate's similarity to the query counts.
	WeightSemantic = 0.55
	// WeightRecency is how much how recently it was said counts.
	WeightRecency = 0.20
	// WeightImportance is how much the item's own importance counts.
	WeightImportance = 0.15
	// WeightRole is how much the speaking role counts: section 12.2 calls it "agent
	// role", and what it distinguishes is a user's own instruction from an agent's reply.
	WeightRole = 0.10
)

// RoleWeight is the role component of the fusion.
//
// A user's own words outweigh an assistant's, because in this domain the user is the one
// who decides: PRD FR-120's explicit-history example is "之前确定的女主服装" — something
// the user established — and section 12.4 lists 用户消息 first among the write sources. A
// tool result is the weakest, because it is a lookup rather than a statement.
func RoleWeight(role string) float64 {
	switch role {
	case "user":
		return 1
	case "developer", "system":
		return 0.8
	case "assistant":
		return 0.6
	case "tool":
		return 0.3
	default:
		return 0.5
	}
}

// RecencyWeight is the recency component of the fusion.
//
// It is an exponential decay with a half-life, rather than a linear ramp, because a
// conversation's relevance falls off by how much has happened since rather than by
// raw elapsed time: the half-life is stated in messages already seen, so a quiet
// afternoon and a busy one are treated the same way.
//
// It returns 1 for the newest item and approaches 0 for the oldest. The result is
// always in [0, 1] for a non-negative distance, so the fusion stays a convex
// combination and the weights keep their meaning.
func RecencyWeight(age time.Duration, halfLife time.Duration) float64 {
	if halfLife <= 0 {
		return 1
	}
	if age <= 0 {
		return 1
	}
	return math.Exp2(-float64(age) / float64(halfLife))
}

// DefaultRecencyHalfLife is the half-life the recall uses when a caller states none.
//
// A day, because a drama project's valuable history is what was established earlier in
// the work rather than in the last ten minutes: PRD FR-120's own example ("早期设定经过
// 大量消息") expects a setting from the beginning of a session to still be reachable.
const DefaultRecencyHalfLife = 24 * time.Hour

// Candidate is one scored recall candidate.
type Candidate struct {
	Item MemoryItem
	// Similarity is the query's similarity to the item, in [-1, 1] for a normalised
	// dot product. It is the ONLY input the threshold applies to: section 12.2 puts the
	// threshold on the semantic channel, and AC-MEM-003 says a candidate below it is not
	// returned — while a locked high-importance memory is recalled on its own rule.
	Similarity float64
}

// Score is the fused score of one candidate.
//
// Every component is bounded, so the result is bounded too and two runs over the same
// inputs give the same number: a recall whose ordering changed between runs would make
// the AC-MEM-005 rerank untestable.
func Score(candidate Candidate, now time.Time, halfLife time.Duration) float64 {
	item := candidate.Item
	recency := RecencyWeight(now.Sub(item.CreatedAt), halfLife)
	return WeightSemantic*candidate.Similarity +
		WeightRecency*recency +
		WeightImportance*item.Importance +
		WeightRole*RoleWeight(string(item.Role))
}

// PassesThreshold reports whether a candidate may enter the context on the semantic
// channel.
//
// It is two rules, not one, and the second is the exception AC-MEM-003 names:
//
//   - A locked memory of high importance is recalled regardless of the threshold,
//     because the user pinned it and section 12.2 lists it as its own channel.
//   - Everything else must reach the threshold. PRD FR-120 is emphatic — "有相似度阈值和
//     Top-K；不得无条件返回低相关结果" — so a caller cannot ask for Top-K of whatever
//     came back: below-threshold candidates are dropped BEFORE the count is applied,
//     which is what "不强制 TopK" means.
//
// A threshold of zero disables the similarity test and nothing else. A caller that wants
// every candidate still does not get the pinned exception as a special case, because
// that channel fires on its own rule either way.
func PassesThreshold(candidate Candidate, threshold float64) bool {
	if candidate.Item.IsLockedHighImportance() {
		return true
	}
	return candidate.Similarity >= threshold
}

// Select applies the threshold, then the fusion ordering, then the count.
//
// The ORDER is the point of this function existing rather than the caller sorting and
// slicing. Threshold first, because FR-120 forbids returning low-relevance results
// unconditionally; then score, because Top-K of an unordered set is meaningless; then the
// count. A caller that reversed any two of those would produce a plausible-looking list
// with a different membership.
func Select(candidates []Candidate, options Selection) []Candidate {
	eligible := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Item.Deleted() {
			continue
		}
		if !PassesThreshold(candidate, options.Threshold) {
			continue
		}
		eligible = append(eligible, candidate)
	}
	// Highest score first, and ties broken by identifier so the order is total: two
	// candidates with the same score must not swap places between runs.
	sortCandidates(eligible, options)
	if options.TopK > 0 && len(eligible) > options.TopK {
		eligible = eligible[:options.TopK]
	}
	return eligible
}

// Selection is what a caller asks a scored retrieval for.
type Selection struct {
	// Threshold is the minimum similarity, on the semantic channel.
	Threshold float64
	// TopK bounds how many come back. Zero means the caller set no count and the
	// retrieval's own ceiling applies.
	TopK int
	// Now and HalfLife are the recency inputs. A zero Now disables the recency term's
	// decay by making every age non-positive, which is what a caller that does not care
	// about recency should get rather than a silently different ordering.
	Now      time.Time
	HalfLife time.Duration
}

// sortCandidates orders by fused score, descending, with a total order.
//
// An insertion sort rather than sort.Slice, because the candidate list is bounded by the
// retrieval's own ceiling (SECURITY section 7.5's "Memory 候选上限") and a stable total
// order is easier to see here than in a comparator with a tie-break buried in it.
func sortCandidates(candidates []Candidate, options Selection) {
	for index := 1; index < len(candidates); index++ {
		current := candidates[index]
		currentScore := Score(current, options.Now, options.HalfLife)
		position := index - 1
		for position >= 0 {
			compare := candidates[position]
			compareScore := Score(compare, options.Now, options.HalfLife)
			better := currentScore > compareScore
			if !better && currentScore == compareScore {
				better = current.Item.ID < compare.Item.ID
			}
			if !better {
				break
			}
			candidates[position+1] = compare
			position--
		}
		candidates[position+1] = current
	}
}
