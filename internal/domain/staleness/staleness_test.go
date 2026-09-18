package staleness

import (
	"testing"
)

// TestChainMatchesSection15Order pins the propagation order. The order is what
// Downstream and Classify derive from, so a reordering would silently change
// which artifacts are marked and how severely.
func TestChainMatchesSection15Order(t *testing.T) {
	documented := []string{
		"source_document_version", "chapter", "story_entity", "story_event", "story_relation",
		"character_state", "story_skeleton_version", "adaptation_strategy_version",
		"script_version", "scene", "shot", "asset_version", "director_plan_version",
		"storyboard_version", "storyboard_item", "storyboard_panel_version",
		"workflow_run", "stage_run", "canvas_node",
	}
	if len(Chain) != len(documented) {
		t.Fatalf("the chain has %d nodes, the specification names %d", len(Chain), len(documented))
	}
	for index, want := range documented {
		if got := string(Chain[index]); got != want {
			t.Fatalf("chain node %d is %q, want %q", index, got, want)
		}
	}
	// The section 15.2 diagram puts script before director plan before
	// storyboard, and the panel after the item. Those relative positions are the
	// ones a wrong edit would break.
	positions := map[ArtifactType]int{}
	for index, node := range Chain {
		positions[node] = index
	}
	for _, pair := range [][2]ArtifactType{
		{ArtifactSourceDocumentVersion, ArtifactChapter},
		{ArtifactChapter, ArtifactStoryEvent},
		{ArtifactStoryEvent, ArtifactScriptVersion},
		{ArtifactScriptVersion, ArtifactDirectorPlan},
		{ArtifactScriptVersion, ArtifactAssetVersion},
		{ArtifactDirectorPlan, ArtifactStoryboardVersion},
		{ArtifactStoryboardVersion, ArtifactStoryboardItem},
		{ArtifactStoryboardItem, ArtifactStoryboardPanel},
	} {
		if positions[pair[0]] >= positions[pair[1]] {
			t.Fatalf("%s must precede %s in the chain", pair[0], pair[1])
		}
	}
	for _, rejected := range []string{"", "storyboard", "memory_item", "Canvas_Node"} {
		if IsValidArtifactType(ArtifactType(rejected)) {
			t.Fatalf("undocumented artifact type %q accepted", rejected)
		}
	}
}

// TestDownstreamFollowsTheDependencyGraph covers the propagation direction: a
// change implicates its consumers, their consumers, and nothing upstream.
func TestDownstreamFollowsTheDependencyGraph(t *testing.T) {
	// Every content artifact should be reachable from the document version,
	// because the document is the root of the content flow.
	fromDocument := Downstream(ArtifactSourceDocumentVersion)
	wantReachable := len(Chain) - 1 - len(OrchestrationTypes)
	if len(fromDocument) != wantReachable {
		t.Fatalf("a document version change reaches %d artifacts, want %d", len(fromDocument), wantReachable)
	}
	for _, node := range Chain {
		if node == ArtifactSourceDocumentVersion || IsOrchestrationType(node) {
			continue
		}
		if !contains(fromDocument, node) {
			t.Fatalf("a document version change must reach %s", node)
		}
	}
	// Orchestration records are not content: they are marked directly by the
	// code that knows why, not discovered by walking content references.
	for _, node := range OrchestrationTypes {
		if contains(fromDocument, node) {
			t.Fatalf("%s must not be reachable from a document change by reference", node)
		}
	}
	// Reachability is transitive: a panel is not a direct consumer of a chapter,
	// so it is informational, but it is still reachable.
	if !contains(fromDocument, ArtifactStoryboardPanel) {
		t.Fatal("a document version change must reach the storyboard panel transitively")
	}
	// The orchestration records are only reachable from what they run on.
	fromPanel := Downstream(ArtifactStoryboardPanel)
	want := []ArtifactType{}
	if len(fromPanel) != len(want) {
		t.Fatalf("a panel change reaches %d artifacts (%v), want %d", len(fromPanel), fromPanel, len(want))
	}
	// A stage run is reachable from the workflow run it belongs to.
	fromRun := Downstream(ArtifactWorkflowRun)
	if len(fromRun) != 1 || fromRun[0] != ArtifactStageRun {
		t.Fatalf("a workflow run reaches %v, want only the stage run", fromRun)
	}
	// The chain order is preserved in the result, so a report reads consistently.
	assertChainOrdered(t, fromDocument)
	// An unknown artifact reaches nothing.
	if got := Downstream(ArtifactType("bogus")); got != nil {
		t.Fatalf("an unknown artifact must reach nothing, got %d", len(got))
	}
}

// TestDirectDependentsAreTheSchemaEdges pins the review_required set. Every
// entry here is a foreign key or link table in the schema, so a wrong edge would
// either miss a re-review or demand one where the artifact holds no reference.
func TestDirectDependentsAreTheSchemaEdges(t *testing.T) {
	cases := []struct {
		from ArtifactType
		want []ArtifactType
		why  string
	}{
		{
			from: ArtifactChapter,
			want: []ArtifactType{ArtifactStoryEntity, ArtifactStoryEvent},
			why:  "story_events.chapter_id and aliases.source_chapter_id are chapter references",
		},
		{
			from: ArtifactSourceDocumentVersion,
			want: []ArtifactType{ArtifactChapter, ArtifactStoryEntity},
			why:  "chapters and imported entities name the version they were read from",
		},
		{
			from: ArtifactStoryEvent,
			want: []ArtifactType{ArtifactStoryRelation, ArtifactCharacterState, ArtifactStorySkeleton, ArtifactAdaptationStrategy, ArtifactScene},
			why:  "the skeleton and strategy link tables, and scenes.source_story_event_id",
		},
		{
			from: ArtifactScriptVersion,
			want: []ArtifactType{ArtifactScene, ArtifactDirectorPlan, ArtifactStoryboardVersion},
			why:  "scenes, director plans and storyboard versions all name a script version",
		},
		{
			from: ArtifactShot,
			want: []ArtifactType{ArtifactStoryboardItem},
			why:  "storyboard_items.shot_id",
		},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.from), func(t *testing.T) {
			got := DirectDependents(testCase.from)
			if len(got) != len(testCase.want) {
				t.Fatalf("%s has %d direct dependents (%v), want %d (%v): %s",
					testCase.from, len(got), got, len(testCase.want), testCase.want, testCase.why)
			}
			for index, expected := range testCase.want {
				if got[index] != expected {
					t.Fatalf("direct dependent %d = %s, want %s (%s)", index, got[index], expected, testCase.why)
				}
			}
		})
	}
	// A consumer with no reference to the changed artifact is not a direct
	// dependent, which is the distinction the severity rests on.
	if contains(DirectDependents(ArtifactChapter), ArtifactStoryboardPanel) {
		t.Fatal("a storyboard panel holds no chapter reference, so it is not a direct dependent")
	}
	if got := DirectDependents(ArtifactType("bogus")); len(got) != 0 {
		t.Fatalf("an unknown artifact has %d direct dependents, want none", len(got))
	}
}

// TestClassifySeverityFollowsTheDependencyKind covers the judgement this
// package makes that the specification leaves to the implementation.
func TestClassifySeverityFollowsTheDependencyKind(t *testing.T) {
	// A direct consumer holds a reference, so the change is about its own input.
	// This is PRD FR-030's "modified chapter marks the affected facts for
	// re-review".
	severity, ok := Classify(ArtifactChapter, ArtifactStoryEvent)
	if !ok || severity != SeverityReviewRequired {
		t.Fatalf("a direct consumer must be review_required, got %q (ok=%v)", severity, ok)
	}
	// The same pair one hop further is a notice, not a re-review demand.
	severity, ok = Classify(ArtifactChapter, ArtifactStoryboardPanel)
	if !ok || severity != SeverityInformational {
		t.Fatalf("a transitive consumer must be informational, got %q (ok=%v)", severity, ok)
	}
	// Backwards, self and unknown do not propagate.
	if _, ok := Classify(ArtifactShot, ArtifactScriptVersion); ok {
		t.Fatal("the graph does not propagate upstream")
	}
	if _, ok := Classify(ArtifactShot, ArtifactShot); ok {
		t.Fatal("an artifact does not implicate itself")
	}
	if _, ok := Classify(ArtifactType("bogus"), ArtifactShot); ok {
		t.Fatal("an unknown source must not classify")
	}
	// Every direct edge must classify as review_required, which is what makes
	// the rule uniform rather than special-cased.
	for _, from := range Chain {
		for _, to := range DirectDependents(from) {
			severity, ok := Classify(from, to)
			if !ok || severity != SeverityReviewRequired {
				t.Fatalf("direct edge %s -> %s classified as %q (ok=%v)", from, to, severity, ok)
			}
		}
	}
	// Nothing may classify as review_required unless it is a direct consumer.
	for _, from := range Chain {
		for _, to := range Chain {
			severity, ok := Classify(from, to)
			if ok && severity == SeverityReviewRequired && !contains(DirectDependents(from), to) {
				t.Fatalf("%s -> %s is review_required but is not a direct dependency", from, to)
			}
		}
	}
}

// TestGraphIsAcyclic proves the dependency graph has no cycle, which is what
// makes Downstream terminate and a propagation order exist.
func TestGraphIsAcyclic(t *testing.T) {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := map[ArtifactType]int{}
	var walk func(node ArtifactType) bool
	walk = func(node ArtifactType) bool {
		switch state[node] {
		case visiting:
			return false // a back edge: cycle
		case done:
			return true
		}
		state[node] = visiting
		for _, dependency := range dependsOn[node] {
			if !walk(dependency) {
				return false
			}
		}
		state[node] = done
		return true
	}
	for _, node := range Chain {
		if !walk(node) {
			t.Fatalf("the dependency graph has a cycle through %s", node)
		}
	}
}

func contains(values []ArtifactType, want ArtifactType) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertChainOrdered(t *testing.T, values []ArtifactType) {
	t.Helper()
	previous := -1
	for _, value := range values {
		current := rank(value)
		if current <= previous {
			t.Fatalf("%s appears out of chain order", value)
		}
		previous = current
	}
}

// TestMarkValidate covers the waiver rules of section 15.3.
func TestMarkValidate(t *testing.T) {
	base := Mark{
		ArtifactType: ArtifactScriptVersion,
		ArtifactID:   "sv-1",
		ProjectID:    "p-1",
		Severity:     SeverityReviewRequired,
		Reason:       "the source chapter changed",
		UpstreamType: ArtifactChapter,
		UpstreamID:   "ch-1",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed mark was rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Mark)
	}{
		{"unknown artifact type", func(m *Mark) { m.ArtifactType = "bogus" }},
		{"empty artifact id", func(m *Mark) { m.ArtifactID = "  " }},
		{"empty project", func(m *Mark) { m.ProjectID = "" }},
		{"unknown severity", func(m *Mark) { m.Severity = "critical" }},
		{"unknown upstream type", func(m *Mark) { m.UpstreamType = "bogus" }},
		{"waiver without a decision", func(m *Mark) { m.Waived = true }},
		{"waiver without a reason", func(m *Mark) {
			m.Waived = true
			m.WaivedByDecisionID = "ug-1"
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mark := base
			testCase.mutate(&mark)
			if err := mark.Validate(); err == nil {
				t.Fatal("a malformed mark was accepted")
			}
		})
	}
	// A complete waiver is accepted: section 15.3 requires a decision and a
	// reason, and nothing else.
	waived := base
	waived.Waived = true
	waived.WaivedByDecisionID = "ug-1"
	waived.WaivedReason = "the change does not affect this scene"
	if err := waived.Validate(); err != nil {
		t.Fatalf("a complete waiver was rejected: %v", err)
	}
	// An informational mark needs no upstream, because a recompute can record a
	// notice without naming a single cause.
	notice := base
	notice.Severity = SeverityInformational
	notice.UpstreamType = ""
	notice.UpstreamID = ""
	if err := notice.Validate(); err != nil {
		t.Fatalf("an informational mark without an upstream was rejected: %v", err)
	}
}
