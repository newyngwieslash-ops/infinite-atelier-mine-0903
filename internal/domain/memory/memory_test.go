package memory

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover the rules section 14.5 states as invariants and the two encodings
// the storage format depends on: the float32 BLOB and the fused score. They are pure
// functions, so each is pinned exactly rather than observed through a store.

// ---------------------------------------------------------------------------
// The scope (section 14.4)
// ---------------------------------------------------------------------------

// TestScopeSeparatesProjectsAndAgents is section 14.4's structural isolation.
//
// Two projects whose identifiers share a PREFIX are the case a prefix-matching store
// would leak, and two agents in one project are the case that makes a supervisor recall
// its own conversation rather than the decision agent's.
func TestScopeSeparatesProjectsAndAgents(t *testing.T) {
	first := Scope{Tenant: "local", Project: "project-1", AgentKey: "script.decision"}
	second := Scope{Tenant: "local", Project: "project-10", AgentKey: "script.decision"}
	third := Scope{Tenant: "local", Project: "project-1", AgentKey: "script.supervision"}

	if first.Key() == second.Key() {
		t.Fatal("two projects produced the same scope key")
	}
	if first.Key() == third.Key() {
		t.Fatal("two agents produced the same scope key")
	}
	// The parts are what a query filters on, and the project is the third of them.
	if first.Parts()[2] != "project-1" || second.Parts()[2] != "project-10" {
		t.Fatalf("the project is not the third part: %+v", first.Parts())
	}
	// A scope with no project would recall across projects, so it is refused rather than
	// defaulted.
	if err := (Scope{AgentKey: "script.decision"}).Validate(); err == nil {
		t.Fatal("a scope with no project was accepted")
	}
	// A control character is refused because it would let one scope render to the same
	// key as another.
	if err := (Scope{Project: "p\x00q"}).Validate(); err == nil {
		t.Fatal("a scope containing a control character was accepted")
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("a well-formed scope was refused: %v", err)
	}
}

// TestScopeWideningStaysInsideTheProject covers the two widenings a summary level needs.
func TestScopeWideningStaysInsideTheProject(t *testing.T) {
	full := Scope{Tenant: "local", Project: "p1", Episode: "e1", AgentKey: "a1", Session: "s1"}

	episode := full.EpisodeOnly()
	if episode.Project != "p1" || episode.Episode != "e1" || episode.AgentKey != "" || episode.Session != "" {
		t.Fatalf("EpisodeOnly gave %+v", episode)
	}
	project := full.ProjectOnly()
	if project.Project != "p1" || project.Episode != "" || project.AgentKey != "" || project.Session != "" {
		t.Fatalf("ProjectOnly gave %+v", project)
	}
	// Neither widening may drop the project, which is the only leak that matters.
	if episode.Project == "" || project.Project == "" {
		t.Fatal("a widening dropped the project, which is the leak section 14.5 forbids")
	}
}

// ---------------------------------------------------------------------------
// Item validation and the invariants (sections 14.1 and 14.5)
// ---------------------------------------------------------------------------

func sampleItem(t *testing.T) MemoryItem {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return MemoryItem{
		ID:         "m1",
		Type:       TypeEpisodic,
		Scope:      Scope{Tenant: "local", Project: "p1"},
		Role:       agent.MessageUser,
		Content:    "女主不能穿红色",
		Importance: DefaultImportance,
		Confidence: DefaultConfidence,
		SourceType: SourceMessage,
		SourceID:   "msg-1",
		CreatedAt:  now,
		UpdatedAt:  now,
		Revision:   1,
	}
}

func TestMemoryItemValidateAcceptsTheDocumentedShape(t *testing.T) {
	if err := sampleItem(t).Validate(); err != nil {
		t.Fatalf("the documented shape was refused: %v", err)
	}
	// A user-typed preference cites nothing, which is what SourceUnset is for.
	typed := sampleItem(t)
	typed.SourceType = SourceUnset
	typed.SourceID = ""
	typed.Type = TypeSemantic
	if err := typed.Validate(); err != nil {
		t.Fatalf("a memory with no upstream row was refused: %v", err)
	}
}

func TestMemoryItemValidateRefusesTheBrokenShapes(t *testing.T) {
	cases := []struct {
		name   string
		change func(*MemoryItem)
	}{
		{"no identifier", func(m *MemoryItem) { m.ID = "" }},
		{"no project", func(m *MemoryItem) { m.Scope = Scope{AgentKey: "a"} }},
		{"unknown type", func(m *MemoryItem) { m.Type = "daydream" }},
		{"unknown role", func(m *MemoryItem) { m.Role = "narrator" }},
		{"unknown source type", func(m *MemoryItem) { m.SourceType = "guess" }},
		{"importance above one", func(m *MemoryItem) { m.Importance = 1.5 }},
		{"negative confidence", func(m *MemoryItem) { m.Confidence = -0.1 }},
		{"citation with no kind", func(m *MemoryItem) { m.SourceType = SourceUnset }},
		{"kind with no citation", func(m *MemoryItem) { m.SourceID = "" }},
		{"embedding with no model", func(m *MemoryItem) {
			m.EmbeddingBlob = EncodeVector([]float32{1, 0})
			m.EmbeddingVersion = "v1"
		}},
		{"embedding with no version", func(m *MemoryItem) {
			m.EmbeddingBlob = EncodeVector([]float32{1, 0})
			m.EmbeddingModel = "m"
		}},
		{"content longer than a message", func(m *MemoryItem) {
			m.Content = strings.Repeat("x", MaxContentLength+1)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			item := sampleItem(t)
			testCase.change(&item)
			if err := item.Validate(); err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
		})
	}
}

// TestCanModifyMemoryIsTheLockedRule is section 14.5's "locked memory 只有用户可修改/删除".
//
// The rule is on the item and the ACTOR together rather than on the caller, because a
// caller that forgot to check is exactly what the invariant exists for. An unlocked
// memory is changeable by anyone, so the check is not a blanket refusal.
func TestCanModifyMemoryIsTheLockedRule(t *testing.T) {
	item := sampleItem(t)
	if err := CanModifyMemory(item, ActorAgent); err != nil {
		t.Fatalf("an unlocked memory was refused to an agent: %v", err)
	}
	item.Locked = true
	if err := CanModifyMemory(item, ActorUser); err != nil {
		t.Fatalf("the user was refused their own pinned memory: %v", err)
	}
	err := CanModifyMemory(item, ActorAgent)
	if err == nil {
		t.Fatal("an agent was allowed to change a pinned memory")
	}
	if _, ok := AsError(err); !ok {
		t.Fatalf("the refusal is a %T, want a memory error", err)
	}
	if err := CanModifyMemory(item, ActorSystem); err == nil {
		t.Fatal("a system actor was allowed to change a pinned memory")
	}
}

// TestIsLockedHighImportanceNeedsBoth is section 12.2's separate channel.
//
// Locked AND high. A channel that fired on either would recall every pinned triviality,
// and AC-MEM-003 names the exception as "locked high-importance memory".
func TestIsLockedHighImportanceNeedsBoth(t *testing.T) {
	item := sampleItem(t)
	item.Importance = 1
	if item.IsLockedHighImportance() {
		t.Fatal("an unpinned high-importance memory was in the pinned channel")
	}
	item.Locked = true
	item.Importance = HighImportance - 0.01
	if item.IsLockedHighImportance() {
		t.Fatal("a pinned low-importance memory was in the pinned channel")
	}
	item.Importance = HighImportance
	if !item.IsLockedHighImportance() {
		t.Fatal("a pinned high-importance memory was not in the pinned channel")
	}
}

// TestEmbeddingIsCurrentSeparatesIndexVersions is section 14.5's
// "embedding 模型变化不覆盖旧向量，重建后切换索引版本".
func TestEmbeddingIsCurrentSeparatesIndexVersions(t *testing.T) {
	item := sampleItem(t)
	if item.EmbeddingIsCurrent("model-a", "v1") {
		t.Fatal("an item with no vector reports a current embedding")
	}
	item.EmbeddingBlob = EncodeVector([]float32{1, 0})
	item.EmbeddingModel = "model-a"
	item.EmbeddingVersion = "v1"
	if !item.EmbeddingIsCurrent("model-a", "v1") {
		t.Fatal("an item embedded by the named model was not current")
	}
	if item.EmbeddingIsCurrent("model-b", "v1") {
		t.Fatal("a vector from another model was treated as current")
	}
	if item.EmbeddingIsCurrent("model-a", "v2") {
		t.Fatal("a vector from another version was treated as current")
	}
}

// ---------------------------------------------------------------------------
// The summary link and the entity link (sections 14.2 and 14.3)
// ---------------------------------------------------------------------------

func TestSummarySourceAndEntityLinkValidate(t *testing.T) {
	good := SummarySource{SummaryID: "s1", SourceMemoryID: "m1", SourceOrder: 1}
	if err := good.Validate(); err != nil {
		t.Fatalf("a well-formed source link was refused: %v", err)
	}
	for _, broken := range []SummarySource{
		{SummaryID: "", SourceMemoryID: "m1", SourceOrder: 1},
		{SummaryID: "s1", SourceMemoryID: "", SourceOrder: 1},
		// A summary citing itself is a cycle with no source behind it.
		{SummaryID: "s1", SourceMemoryID: "s1", SourceOrder: 1},
		// Order zero would make the summary's ordering ambiguous.
		{SummaryID: "s1", SourceMemoryID: "m1", SourceOrder: 0},
	} {
		if err := broken.Validate(); err == nil {
			t.Fatalf("%+v was accepted", broken)
		}
	}

	link := EntityLink{MemoryID: "m1", EntityType: "character", EntityID: "c1", RelationType: RelationAbout}
	if err := link.Validate(); err != nil {
		t.Fatalf("a well-formed entity link was refused: %v", err)
	}
	link.RelationType = "vibes"
	if err := link.Validate(); err == nil {
		t.Fatal("an unknown relation was accepted")
	}
	link = EntityLink{MemoryID: "", EntityType: "character", EntityID: "c1", RelationType: RelationAbout}
	if err := link.Validate(); err == nil {
		t.Fatal("a link with no memory was accepted")
	}
	// The three documented relations are all accepted, so the vocabulary is not a
	// one-value list that happens to pass.
	for _, relation := range EntityRelations {
		candidate := EntityLink{MemoryID: "m1", EntityType: "t", EntityID: "i", RelationType: relation}
		if err := candidate.Validate(); err != nil {
			t.Fatalf("documented relation %q was refused: %v", relation, err)
		}
	}
}

// ---------------------------------------------------------------------------
// The vector encoding (FR-120's FLOAT32/BLOB rule)
// ---------------------------------------------------------------------------

func TestVectorRoundTripsExactly(t *testing.T) {
	// Values chosen to exercise the sign bit, a fraction and a magnitude: a
	// byte-order mistake shows up as a wrong sign or a wildly wrong magnitude, and
	// a float64/float32 mix-up shows up as a fractional difference.
	vector := []float32{0.5, -1.25, 0, 3.5, -0.0078125}
	encoded := EncodeVector(vector)
	if len(encoded) != len(vector)*VectorBytesPerElement {
		t.Fatalf("encoded to %d bytes, want %d", len(encoded), len(vector)*VectorBytesPerElement)
	}
	decoded, err := DecodeVector(encoded)
	if err != nil {
		t.Fatalf("DecodeVector: %v", err)
	}
	if len(decoded) != len(vector) {
		t.Fatalf("decoded %d values, want %d", len(decoded), len(vector))
	}
	for index := range vector {
		if decoded[index] != vector[index] {
			t.Fatalf("element %d came back as %v, want %v", index, decoded[index], vector[index])
		}
	}
	// Little-endian is what the format says, so the first element's bytes are pinned
	// here rather than only being implied by a round trip through the same code.
	// 0.5 is 0x3F000000, and its LAST byte is the high one under little-endian.
	if encoded[3] != 0x3F || encoded[0] != 0x00 {
		t.Fatalf("0.5 encoded to % x, which is not little-endian binary32", encoded[:4])
	}
	// No embedding and an empty vector both encode to nil, so "unembedded" stays a
	// single state.
	if EncodeVector(nil) != nil || EncodeVector([]float32{}) != nil {
		t.Fatal("an empty vector did not encode to nil")
	}
	decoded, err = DecodeVector(nil)
	if err != nil || len(decoded) != 0 {
		t.Fatalf("a nil blob decoded to %v with error %v", decoded, err)
	}
}

func TestVectorDecodeRefusesBrokenBlobs(t *testing.T) {
	// Not a whole number of float32 values: a truncated read would compare as though it
	// were complete, which is the wrong answer nothing downstream could detect.
	if _, err := DecodeVector([]byte{1, 2, 3}); err == nil {
		t.Fatal("a partial vector was decoded")
	}
	// Larger than any embedding this build supports, so a corrupt row cannot make a
	// decode allocate without bound.
	oversized := make([]byte, MaxVectorBytes+VectorBytesPerElement)
	if _, err := DecodeVector(oversized); err == nil {
		t.Fatal("an oversized vector was decoded")
	}
	// And the boundary itself is accepted, so the ceiling is a ceiling rather than an
	// off-by-one refusal.
	atCeiling := make([]byte, MaxVectorBytes)
	if _, err := DecodeVector(atCeiling); err != nil {
		t.Fatalf("a vector exactly at the ceiling was refused: %v", err)
	}
}

func TestNormalizeAndDot(t *testing.T) {
	normalized := Normalize([]float32{3, 4})
	if math.Abs(float64(normalized[0])-0.6) > 1e-6 || math.Abs(float64(normalized[1])-0.8) > 1e-6 {
		t.Fatalf("(3,4) normalised to %+v", normalized)
	}
	// The point of normalising at write time: the dot product of two unit vectors IS
	// the cosine similarity, so an identical vector scores one and an orthogonal one
	// scores zero.
	if score := Dot(normalized, normalized); math.Abs(score-1) > 1e-6 {
		t.Fatalf("a unit vector scored %v against itself", score)
	}
	orthogonal := Normalize([]float32{-4, 3})
	if score := Dot(normalized, orthogonal); math.Abs(score) > 1e-6 {
		t.Fatalf("two orthogonal vectors scored %v", score)
	}
	// Opposite vectors score -1, which is why the threshold can be negative and why the
	// similarity is not clamped to [0,1].
	if score := Dot(normalized, Normalize([]float32{-3, -4})); math.Abs(score+1) > 1e-6 {
		t.Fatalf("opposite vectors scored %v", score)
	}
	// A zero vector normalises to itself rather than dividing by zero, and then scores
	// zero against everything: the honest answer for a vector with no direction.
	zero := Normalize([]float32{0, 0, 0})
	if zero[0] != 0 || zero[1] != 0 || zero[2] != 0 {
		t.Fatalf("a zero vector normalised to %+v", zero)
	}
	if score := Dot(zero, normalized); score != 0 {
		t.Fatalf("a zero vector scored %v", score)
	}
	// Vectors of different lengths score zero, because two different models produce
	// different lengths and a comparison between them is meaningless.
	if score := Dot([]float32{1, 0}, []float32{1, 0, 0}); score != 0 {
		t.Fatalf("mismatched lengths scored %v", score)
	}
	if score := Dot(nil, []float32{1}); score != 0 {
		t.Fatalf("an empty vector scored %v", score)
	}
}

// ---------------------------------------------------------------------------
// The fused score (section 12.2)
// ---------------------------------------------------------------------------

func TestRecencyWeightDecaysByHalfLife(t *testing.T) {
	halfLife := 10 * time.Hour
	if got := RecencyWeight(0, halfLife); got != 1 {
		t.Fatalf("the newest item scored %v, want 1", got)
	}
	if got := RecencyWeight(-time.Hour, halfLife); got != 1 {
		t.Fatalf("a future item scored %v, want the ceiling 1", got)
	}
	if got := RecencyWeight(halfLife, halfLife); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("one half-life scored %v, want 0.5", got)
	}
	if got := RecencyWeight(3*halfLife, halfLife); math.Abs(got-0.125) > 1e-9 {
		t.Fatalf("three half-lives scored %v, want 0.125", got)
	}
	// Monotone: an older item never scores higher than a newer one, which is what makes
	// the term a recency weight rather than a periodic one.
	previous := 1.0
	for age := time.Duration(0); age < 100*halfLife; age += halfLife / 4 {
		got := RecencyWeight(age, halfLife)
		if got > previous+1e-12 {
			t.Fatalf("recency rose from %v to %v at age %v", previous, got, age)
		}
		previous = got
	}
	// A zero half-life is a caller that did not set one; it must not divide by zero.
	if got := RecencyWeight(time.Hour, 0); got != 1 {
		t.Fatalf("a zero half-life scored %v", got)
	}
}

func TestRoleWeightRanksTheUserAboveTheAgent(t *testing.T) {
	// A user's own words outweigh an assistant's, because in this domain the user is the
	// one who decides; a tool result is the weakest, being a lookup rather than a
	// statement.
	if RoleWeight("user") <= RoleWeight("assistant") {
		t.Fatal("an assistant's reply outweighed the user's own words")
	}
	if RoleWeight("assistant") <= RoleWeight("tool") {
		t.Fatal("a tool result outweighed an assistant's reply")
	}
	if RoleWeight("user") > 1 || RoleWeight("tool") < 0 || RoleWeight("") < 0 {
		t.Fatal("a role weight left the range the fusion assumes")
	}
}

// TestScoreIsTheDocumentedFusion pins section 12.2's arithmetic exactly.
//
// The weights are stated in the contract, so the sum is checkable by hand: a change to
// any one of them is a change to recall behaviour and has to fail here.
func TestScoreIsTheDocumentedFusion(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	item := sampleItem(t)
	item.CreatedAt = now // age zero, so recency is 1
	item.Importance = 1  // the maximum, so the importance term is its full weight
	item.Role = agent.MessageUser

	candidate := Candidate{Item: item, Similarity: 1}
	want := WeightSemantic*1 + WeightRecency*1 + WeightImportance*1 + WeightRole*1
	if want != 1.0 {
		t.Fatalf("the weights total %v, not 1", want)
	}
	if got := Score(candidate, now, DefaultRecencyHalfLife); math.Abs(got-1) > 1e-9 {
		t.Fatalf("a perfect candidate scored %v, want 1", got)
	}
	// The weights add to one and every component is bounded by one, so the score is too.
	if WeightSemantic+WeightRecency+WeightImportance+WeightRole != 1.0 {
		t.Fatalf("the weights are %v, %v, %v, %v", WeightSemantic, WeightRecency, WeightImportance, WeightRole)
	}
	// A candidate with no similarity, no importance and the weakest role, aged far
	// beyond its half-life, scores near the floor rather than below it.
	floor := sampleItem(t)
	floor.CreatedAt = now.Add(-1000 * DefaultRecencyHalfLife)
	floor.Importance = 0
	floor.Role = agent.MessageTool
	got := Score(Candidate{Item: floor, Similarity: -1}, now, DefaultRecencyHalfLife)
	if got < -WeightSemantic || got > 0.5 {
		t.Fatalf("the worst candidate scored %v", got)
	}
}

// TestPassesThresholdIsTheSemanticRuleAndItsException is AC-MEM-003.
//
// Two rules: everything must reach the threshold, and a locked high-importance memory is
// recalled on its own rule regardless.
func TestPassesThresholdIsTheSemanticRuleAndItsException(t *testing.T) {
	item := sampleItem(t)
	low := Candidate{Item: item, Similarity: 0.1}
	if PassesThreshold(low, 0.5) {
		t.Fatal("a below-threshold candidate passed")
	}
	if !PassesThreshold(Candidate{Item: item, Similarity: 0.5}, 0.5) {
		t.Fatal("a candidate exactly at the threshold was dropped")
	}
	// The exception: pinned AND important.
	pinned := sampleItem(t)
	pinned.Locked = true
	pinned.Importance = HighImportance
	if !PassesThreshold(Candidate{Item: pinned, Similarity: 0}, 0.9) {
		t.Fatal("a pinned high-importance memory was dropped by the threshold")
	}
	// Pinned but unimportant is NOT the exception, which is what keeps a pinned
	// triviality out of every context.
	trivial := sampleItem(t)
	trivial.Locked = true
	if PassesThreshold(Candidate{Item: trivial, Similarity: 0}, 0.9) {
		t.Fatal("a pinned triviality passed the threshold")
	}
	// A zero threshold disables the similarity test and nothing else.
	if !PassesThreshold(low, 0) {
		t.Fatal("a zero threshold still dropped a candidate")
	}
}

// TestSelectAppliesThresholdThenScoreThenCount is the order FR-120 requires.
//
// "有相似度阈值和 Top-K；不得无条件返回低相关结果" — so below-threshold candidates are
// dropped BEFORE the count is applied. That is what "不强制 TopK" means: a caller asking
// for three does not get three when only one is relevant.
func TestSelectAppliesThresholdThenScoreThenCount(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// The old item is the MOST similar; the new one is less similar. If the count were
	// applied before the threshold, the ordering would differ from this.
	old := sampleItem(t)
	old.ID = "old"
	old.CreatedAt = now.Add(-10 * time.Hour)
	old.Content = "女主不能穿红色"
	newer := sampleItem(t)
	newer.ID = "new"
	newer.CreatedAt = now

	candidates := []Candidate{
		{Item: old, Similarity: 0.9},
		{Item: newer, Similarity: 0.2}, // below the threshold
	}
	selected := Select(candidates, Selection{Threshold: 0.5, TopK: 5, Now: now, HalfLife: DefaultRecencyHalfLife})
	if len(selected) != 1 || selected[0].Item.ID != "old" {
		t.Fatalf("Select returned %+v, want only the above-threshold candidate", selected)
	}
	// A count smaller than the eligible set keeps the best-scoring ones, and the score
	// is the fused one rather than the raw similarity.
	third := sampleItem(t)
	third.ID = "third"
	third.CreatedAt = now.Add(-2 * time.Hour)
	selected = Select([]Candidate{
		{Item: old, Similarity: 0.9},
		{Item: third, Similarity: 0.8},
	}, Selection{Threshold: 0, TopK: 1, Now: now, HalfLife: DefaultRecencyHalfLife})
	if len(selected) != 1 || selected[0].Item.ID != "old" {
		t.Fatalf("TopK kept %+v, want the higher-scoring candidate", selected)
	}
	// A deleted memory never comes back, whatever its score.
	gone := sampleItem(t)
	gone.ID = "gone"
	gone.DeletedAt = now
	selected = Select([]Candidate{{Item: gone, Similarity: 1}}, Selection{Threshold: 0, Now: now})
	if len(selected) != 0 {
		t.Fatalf("a deleted memory was recalled: %+v", selected)
	}
	// A zero TopK means the caller set no count, and the retrieval's own ceiling applies
	// rather than the list being truncated to nothing.
	selected = Select([]Candidate{{Item: old, Similarity: 0.9}, {Item: third, Similarity: 0.8}},
		Selection{Threshold: 0, Now: now})
	if len(selected) != 2 {
		t.Fatalf("a zero TopK returned %d candidates", len(selected))
	}
	// All below the threshold: the honest empty result, not a forced Top-K.
	selected = Select([]Candidate{{Item: old, Similarity: 0.1}, {Item: third, Similarity: 0.2}},
		Selection{Threshold: 0.9, TopK: 3, Now: now})
	if len(selected) != 0 {
		t.Fatalf("an all-below-threshold retrieval returned %+v", selected)
	}
}

// TestSelectIsDeterministicOnTies pins the total order.
//
// Two candidates with the same score must not swap places between runs, or a recall's
// ordering would depend on the store's row order and AC-MEM-005's rerank would have
// nothing stable to rerank.
func TestSelectIsDeterministicOnTies(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	build := func(ids ...string) []Candidate {
		candidates := make([]Candidate, 0, len(ids))
		for _, id := range ids {
			item := sampleItem(t)
			item.ID = id
			item.CreatedAt = now
			candidates = append(candidates, Candidate{Item: item, Similarity: 0.5})
		}
		return candidates
	}
	options := Selection{Threshold: 0, Now: now, HalfLife: DefaultRecencyHalfLife}
	first := Select(build("b", "a", "c"), options)
	second := Select(build("c", "b", "a"), options)
	for index := range first {
		if first[index].Item.ID != second[index].Item.ID {
			t.Fatalf("two runs ordered tied candidates differently: %v vs %v", first, second)
		}
	}
	if first[0].Item.ID != "a" || first[2].Item.ID != "c" {
		t.Fatalf("ties are not ordered by identifier: %+v", first)
	}
}
