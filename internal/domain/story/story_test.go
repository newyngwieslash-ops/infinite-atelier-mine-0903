package story

import (
	"strings"
	"testing"
)

// categoryOf reads the category of a domain error through the public
// extractor, so the tests exercise AsError rather than reaching into the struct.
func categoryOf(err error) ErrorCategory {
	domainErr, ok := AsError(err)
	if !ok {
		return ""
	}
	return domainErr.Category
}

// checkVocabulary proves a closed vocabulary is exactly the set its migration
// CHECK admits.
//
// The positive half is that every documented value validates and that the list
// holds no duplicate. The negative half is the one that catches a drifting
// vocabulary: the empty string (which no CHECK admits), a case variant of a
// documented value and a string the migration never names must all be refused.
// Without those, a validator written as `return true` would pass every positive
// assertion.
func checkVocabulary[T ~string](t *testing.T, name string, values []T, isValid func(T) bool) {
	t.Helper()
	seen := map[T]bool{}
	for _, value := range values {
		if value == "" {
			t.Fatalf("%s lists the empty value, which no SQL CHECK admits", name)
		}
		if seen[value] {
			t.Fatalf("%s lists %q twice", name, value)
		}
		seen[value] = true
		if !isValid(value) {
			t.Fatalf("%s rejects its own documented value %q", name, value)
		}
		// A case variant of a documented value is a different string, so the
		// SQL CHECK refuses it and so must this package.
		for _, variant := range []T{T(strings.ToUpper(string(value))), T(strings.ToLower(string(value)))} {
			if variant == value {
				continue
			}
			if isValid(variant) {
				t.Fatalf("%s accepts the case variant %q of its documented value %q", name, variant, value)
			}
		}
	}
	if isValid(T("")) {
		t.Fatalf("%s accepts the empty value", name)
	}
	if isValid(T("nonsense")) {
		t.Fatalf("%s accepts the unknown value %q", name, "nonsense")
	}
}

// TestStoryVocabularies covers §5 and §6's closed sets. Each one is the value
// list migration 000007 pins in a CHECK constraint.
func TestStoryVocabularies(t *testing.T) {
	checkVocabulary(t, "DocumentTypes", DocumentTypes, IsValidDocumentType)
	checkVocabulary(t, "DocumentStatuses", DocumentStatuses, IsValidDocumentStatus)
	checkVocabulary(t, "CreatedByTypes", CreatedByTypes, IsValidCreatedByType)
	checkVocabulary(t, "ChapterStatuses", ChapterStatuses, IsValidChapterStatus)
	checkVocabulary(t, "EntityTypes", EntityTypes, IsValidEntityType)
	checkVocabulary(t, "FactStatuses", FactStatuses, IsValidFactStatus)
	checkVocabulary(t, "SourceScopes", SourceScopes, IsValidSourceScope)
	checkVocabulary(t, "ParticipantRoles", ParticipantRoles, IsValidParticipantRole)
	checkVocabulary(t, "RelationTypes", RelationTypes, IsValidRelationType)
	checkVocabulary(t, "FactTypes", FactTypes, IsValidFactType)
	checkVocabulary(t, "SourceKinds", SourceKinds, IsValidSourceKind)
	checkVocabulary(t, "ConflictStatuses", ConflictStatuses, IsValidConflictStatus)
}

// TestVocabularyMatchesMigrationChecks pins each Go vocabulary against the
// literal list in migration 000007.
//
// The expected slices are written out here rather than derived from the
// constants, so the two sides can disagree. That is the point: a value added to
// the SQL CHECK without this package, or to this package without SQL, is a bug
// — the database would refuse a value the domain accepts or the reverse — and
// this test is where it fails.
func TestVocabularyMatchesMigrationChecks(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "source_documents.document_type",
			got:  documentTypeStrings(),
			want: []string{"novel", "story", "screenplay", "outline", "notes"},
		},
		{
			name: "source_documents.status",
			got:  documentStatusStrings(),
			want: []string{"active", "archived", "trashed"},
		},
		{
			name: "source_document_versions.created_by_type",
			got:  createdByStrings(),
			want: []string{"user", "agent", "migration", "system"},
		},
		{
			name: "chapters.status",
			got:  chapterStatusStrings(),
			want: []string{"detected", "confirmed", "edited"},
		},
		{
			name: "story_entities.entity_type",
			got:  entityTypeStrings(),
			want: []string{"character", "location", "organization", "prop", "concept", "time"},
		},
		{
			name: "story_entities.status (and story_events, story_relations, character_states)",
			got:  factStatusStrings(),
			want: []string{"candidate", "accepted", "rejected", "locked"},
		},
		{
			name: "story_entities.source_scope (and story_events, story_relations)",
			got:  sourceScopeStrings(),
			want: []string{"original", "adaptation", "user"},
		},
		{
			name: "story_event_participants.role",
			got:  participantRoleStrings(),
			want: []string{"actor", "target", "witness", "owner", "affected", "other"},
		},
		{
			name: "story_relations.relation_type",
			got:  relationTypeStrings(),
			want: []string{
				"participates_in", "occurs_at", "causes", "precedes", "reveals",
				"conflicts_with", "owns", "transfers_to", "changes_state", "knows",
				"related_to", "located_in", "contradicts", "other",
			},
		},
		{
			name: "story_fact_sources.fact_type",
			got:  factTypeStrings(),
			want: []string{"entity", "event", "relation", "character_state"},
		},
		{
			name: "story_fact_sources.source_kind",
			got:  sourceKindStrings(),
			want: []string{"text", "user", "agent_inference", "adaptation"},
		},
		{
			name: "story_fact_conflicts.status",
			got:  conflictStatusStrings(),
			want: []string{"open", "resolved", "waived"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if len(testCase.got) != len(testCase.want) {
				t.Fatalf("the Go list holds %d values %v, but the migration CHECK holds %d %v",
					len(testCase.got), testCase.got, len(testCase.want), testCase.want)
			}
			for index, want := range testCase.want {
				if testCase.got[index] != want {
					t.Fatalf("position %d is %q in Go but %q in the migration CHECK, so the two vocabularies have drifted",
						index, testCase.got[index], want)
				}
			}
		})
	}
}

func documentTypeStrings() []string {
	out := make([]string, 0, len(DocumentTypes))
	for _, value := range DocumentTypes {
		out = append(out, string(value))
	}
	return out
}

func documentStatusStrings() []string {
	out := make([]string, 0, len(DocumentStatuses))
	for _, value := range DocumentStatuses {
		out = append(out, string(value))
	}
	return out
}

func createdByStrings() []string {
	out := make([]string, 0, len(CreatedByTypes))
	for _, value := range CreatedByTypes {
		out = append(out, string(value))
	}
	return out
}

func chapterStatusStrings() []string {
	out := make([]string, 0, len(ChapterStatuses))
	for _, value := range ChapterStatuses {
		out = append(out, string(value))
	}
	return out
}

func entityTypeStrings() []string {
	out := make([]string, 0, len(EntityTypes))
	for _, value := range EntityTypes {
		out = append(out, string(value))
	}
	return out
}

func factStatusStrings() []string {
	out := make([]string, 0, len(FactStatuses))
	for _, value := range FactStatuses {
		out = append(out, string(value))
	}
	return out
}

func sourceScopeStrings() []string {
	out := make([]string, 0, len(SourceScopes))
	for _, value := range SourceScopes {
		out = append(out, string(value))
	}
	return out
}

func participantRoleStrings() []string {
	out := make([]string, 0, len(ParticipantRoles))
	for _, value := range ParticipantRoles {
		out = append(out, string(value))
	}
	return out
}

func relationTypeStrings() []string {
	out := make([]string, 0, len(RelationTypes))
	for _, value := range RelationTypes {
		out = append(out, string(value))
	}
	return out
}

func factTypeStrings() []string {
	out := make([]string, 0, len(FactTypes))
	for _, value := range FactTypes {
		out = append(out, string(value))
	}
	return out
}

func sourceKindStrings() []string {
	out := make([]string, 0, len(SourceKinds))
	for _, value := range SourceKinds {
		out = append(out, string(value))
	}
	return out
}

func conflictStatusStrings() []string {
	out := make([]string, 0, len(ConflictStatuses))
	for _, value := range ConflictStatuses {
		out = append(out, string(value))
	}
	return out
}

// TestRelationTypeVocabularyIsTheMigrationUnion covers the set ADR-0007 rules
// on: PRD FR-030 names eleven relations, §6.5's example line adds located_in
// and contradicts, and the migration's CHECK holds all fourteen with 'other'.
// Every one of them must be storable, or a relation the specification asks for
// would be refused.
func TestRelationTypeVocabularyIsTheMigrationUnion(t *testing.T) {
	if len(RelationTypes) != 14 {
		t.Fatalf("the relation set has %d entries, want the 14 the migration CHECK pins", len(RelationTypes))
	}
	// The three values beyond FR-030's list are the ones most likely to be
	// dropped by a later edit.
	for _, value := range []RelationType{RelationLocatedIn, RelationContradicts, RelationOther} {
		if !IsValidRelationType(value) {
			t.Fatalf("relation %q is in the migration CHECK but this package refuses it", value)
		}
	}
	// And FR-030's eleven must all still be present.
	for _, value := range []RelationType{
		RelationParticipatesIn, RelationOccursAt, RelationCauses, RelationPrecedes,
		RelationReveals, RelationConflictsWith, RelationOwns, RelationTransfersTo,
		RelationChangesState, RelationKnows, RelationRelatedTo,
	} {
		if !IsValidRelationType(value) {
			t.Fatalf("PRD FR-030 relation %q is refused", value)
		}
	}
}

// TestDocumentAndVersionValidate covers §5.1 and §5.2, including the two
// invariants of §5.2: an import always carries the normalized text its chapters
// index, and a replacement is a new version number rather than an edit of the
// old row.
func TestDocumentAndVersionValidate(t *testing.T) {
	base := SourceDocument{
		ID:        "doc-1",
		ProjectID: "project-1",
		Type:      DocumentNovel,
		Name:      "The Long Road",
		Status:    DocumentActive,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed source document was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*SourceDocument)
	}{
		{"no project", func(d *SourceDocument) { d.ProjectID = "   " }},
		{"unknown document type", func(d *SourceDocument) { d.Type = "blog" }},
		{"empty name", func(d *SourceDocument) { d.Name = "  " }},
		{"over-long name", func(d *SourceDocument) { d.Name = strings.Repeat("字", MaxNameLength+1) }},
		{"unknown status", func(d *SourceDocument) { d.Status = "deleted" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document := base
			testCase.mutate(&document)
			if err := document.Validate(); err == nil {
				t.Fatal("a malformed source document was accepted")
			}
		})
	}
	// A name of exactly the limit is accepted, so the bound is inclusive as the
	// SQL CHECK length(name) BETWEEN 1 AND 200 is.
	atLimit := base
	atLimit.Name = strings.Repeat("字", MaxNameLength)
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("a name of exactly %d runes was rejected: %v", MaxNameLength, err)
	}

	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	version := SourceDocumentVersion{
		ID:                   "docv-1",
		SourceDocumentID:     "doc-1",
		VersionNumber:        1,
		NormalizedTextFileID: digest,
		ContentHash:          digest,
		CharCount:            30000,
		CreatedByType:        CreatedByUser,
	}
	if err := version.Validate(); err != nil {
		t.Fatalf("a well-formed document version was rejected: %v", err)
	}
	// A pasted or typed document has no original file, so an empty
	// physical_file_id is valid while the normalized text is not.
	pasted := version
	if err := pasted.Validate(); err != nil {
		t.Fatalf("a version without an original file must be accepted: %v", err)
	}
	versionCases := []struct {
		name   string
		mutate func(*SourceDocumentVersion)
	}{
		{"no document", func(v *SourceDocumentVersion) { v.SourceDocumentID = "" }},
		{"version zero", func(v *SourceDocumentVersion) { v.VersionNumber = 0 }},
		{"negative version", func(v *SourceDocumentVersion) { v.VersionNumber = -1 }},
		{"no normalized text", func(v *SourceDocumentVersion) { v.NormalizedTextFileID = "" }},
		{"normalized text is not a digest", func(v *SourceDocumentVersion) { v.NormalizedTextFileID = "not-a-hash" }},
		{"uppercase digest", func(v *SourceDocumentVersion) { v.NormalizedTextFileID = strings.ToUpper(digest) }},
		{"original file is not a digest", func(v *SourceDocumentVersion) { v.PhysicalFileID = "path/to/file.txt" }},
		{"content hash is not a digest", func(v *SourceDocumentVersion) { v.ContentHash = "abcd" }},
		{"negative character count", func(v *SourceDocumentVersion) { v.CharCount = -1 }},
		{"unknown producer", func(v *SourceDocumentVersion) { v.CreatedByType = "robot" }},
	}
	for _, testCase := range versionCases {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := version
			testCase.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("a malformed document version was accepted")
			}
		})
	}
	// The empty content hash is allowed: a version may be stored before its
	// digest is computed, and §5.2's duplicate check simply cannot run yet.
	noHash := version
	noHash.ContentHash = ""
	if err := noHash.Validate(); err != nil {
		t.Fatalf("a version without a content hash must be accepted: %v", err)
	}
}

// TestChapterValidate covers §5.3's bounds, which the schema also enforces.
func TestChapterValidate(t *testing.T) {
	base := Chapter{
		ID:                      "chapter-1",
		SourceDocumentVersionID: "docv-1",
		Ordinal:                 1,
		Title:                   "Chapter One",
		StartOffset:             0,
		EndOffset:               1000,
		SourceKind:              ChapterFromPattern,
		Status:                  ChapterDetected,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed chapter was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Chapter)
	}{
		{"no document version", func(c *Chapter) { c.SourceDocumentVersionID = "  " }},
		{"ordinal zero", func(c *Chapter) { c.Ordinal = 0 }},
		{"negative ordinal", func(c *Chapter) { c.Ordinal = -1 }},
		{"negative start", func(c *Chapter) { c.StartOffset = -1 }},
		{"negative end", func(c *Chapter) { c.EndOffset = -1 }},
		{"end before start", func(c *Chapter) { c.StartOffset = 500; c.EndOffset = 499 }},
		{"content hash is not a digest", func(c *Chapter) { c.ContentHash = "deadbeef" }},
		{"unknown status", func(c *Chapter) { c.Status = "guessed" }},
		{"unknown source kind", func(c *Chapter) { c.SourceKind = "guessed" }},
		{"empty source kind", func(c *Chapter) { c.SourceKind = "" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			chapter := base
			testCase.mutate(&chapter)
			if err := chapter.Validate(); err == nil {
				t.Fatal("a malformed chapter was accepted")
			}
		})
	}
	// A zero-length chapter is allowed by the schema's CHECK
	// (end_offset >= start_offset), so an empty chapter a user carved out is
	// not a domain error.
	empty := base
	empty.StartOffset = 1000
	empty.EndOffset = 1000
	if err := empty.Validate(); err != nil {
		t.Fatalf("an empty chapter range must be accepted: %v", err)
	}
	// A well-formed chapter hash is accepted.
	hashed := base
	hashed.ContentHash = strings.Repeat("ab", 32)
	if err := hashed.Validate(); err != nil {
		t.Fatalf("a chapter with a SHA-256 digest was rejected: %v", err)
	}
}

// TestChapterContains covers the half-open range §5.3's non-overlap rule
// implies: two adjacent chapters share the boundary offset, and exactly one of
// them owns it.
func TestChapterContains(t *testing.T) {
	first := Chapter{StartOffset: 0, EndOffset: 1000}
	second := Chapter{StartOffset: 1000, EndOffset: 2000}
	cases := []struct {
		name    string
		chapter Chapter
		offset  int
		want    bool
	}{
		{"before the chapter", first, -1, false},
		{"first offset", first, 0, true},
		{"inside the chapter", first, 500, true},
		{"last offset", first, 999, true},
		{"the shared boundary belongs to the next chapter", first, 1000, false},
		{"the next chapter starts at the boundary", second, 1000, true},
		{"past the end", second, 2000, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.chapter.Contains(testCase.offset); got != testCase.want {
				t.Fatalf("Contains(%d) on [%d,%d) is %t, want %t",
					testCase.offset, testCase.chapter.StartOffset, testCase.chapter.EndOffset, got, testCase.want)
			}
		})
	}
	// The two ranges must not both claim the boundary, or an evidence offset
	// could resolve to two chapters.
	if first.Contains(1000) && second.Contains(1000) {
		t.Fatal("two adjacent chapters both claim the boundary offset")
	}
	// An empty chapter owns nothing, which keeps the disjointness property.
	empty := Chapter{StartOffset: 1000, EndOffset: 1000}
	if empty.Contains(1000) {
		t.Fatal("an empty chapter range claimed an offset")
	}
}

// TestStoryEntityValidate covers §6.1.
func TestStoryEntityValidate(t *testing.T) {
	base := StoryEntity{
		ID:            "entity-1",
		ProjectID:     "project-1",
		Type:          EntityCharacter,
		CanonicalName: "Mira",
		Status:        FactCandidate,
		SourceScope:   ScopeOriginal,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed entity was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryEntity)
	}{
		{"no project", func(e *StoryEntity) { e.ProjectID = "" }},
		{"unknown entity type", func(e *StoryEntity) { e.Type = "vehicle" }},
		{"empty name", func(e *StoryEntity) { e.CanonicalName = "   " }},
		{"over-long name", func(e *StoryEntity) { e.CanonicalName = strings.Repeat("字", MaxNameLength+1) }},
		{"unknown status", func(e *StoryEntity) { e.Status = "pending" }},
		{"unknown scope", func(e *StoryEntity) { e.SourceScope = "imported" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			entity := base
			testCase.mutate(&entity)
			if err := entity.Validate(); err == nil {
				t.Fatal("a malformed story entity was accepted")
			}
		})
	}
}

// TestStoryEntityAliasValidate covers §6.2's alias rule and the optional source
// span.
func TestStoryEntityAliasValidate(t *testing.T) {
	start, end := 10, 14
	base := StoryEntityAlias{ID: "alias-1", StoryEntityID: "entity-1", Alias: "小艾"}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed alias was rejected: %v", err)
	}
	// An alias with no source span is valid: a user-typed alias has no chapter.
	spanned := base
	spanned.SourceChapterID = "chapter-1"
	spanned.SourceStart = &start
	spanned.SourceEnd = &end
	if err := spanned.Validate(); err != nil {
		t.Fatalf("an alias with a source span was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryEntityAlias)
	}{
		{"no entity", func(a *StoryEntityAlias) { a.StoryEntityID = "  " }},
		{"empty alias", func(a *StoryEntityAlias) { a.Alias = "" }},
		{"blank alias", func(a *StoryEntityAlias) { a.Alias = "   " }},
		{"over-long alias", func(a *StoryEntityAlias) { a.Alias = strings.Repeat("字", MaxNameLength+1) }},
		{"negative start", func(a *StoryEntityAlias) {
			negative := -1
			zero := 0
			a.SourceStart = &negative
			a.SourceEnd = &zero
		}},
		{"range ends before it starts", func(a *StoryEntityAlias) {
			start, end := 10, 14
			a.SourceStart = &end
			a.SourceEnd = &start
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			alias := base
			testCase.mutate(&alias)
			if err := alias.Validate(); err == nil {
				t.Fatal("a malformed alias was accepted")
			}
		})
	}
	// An alias of exactly the limit is accepted.
	atLimit := base
	atLimit.Alias = strings.Repeat("字", MaxNameLength)
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("an alias of exactly %d runes was rejected: %v", MaxNameLength, err)
	}
}

// TestStoryEventValidate covers §6.3.
func TestStoryEventValidate(t *testing.T) {
	base := StoryEvent{
		ID:          "event-1",
		ProjectID:   "project-1",
		Ordinal:     0,
		Name:        "Mira finds the letter",
		Status:      FactCandidate,
		SourceScope: ScopeOriginal,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed event was rejected: %v", err)
	}
	// Ordinal zero is a valid first position here: the schema's CHECK is
	// ordinal >= 0 for events, unlike the >= 1 it uses for chapters.
	cases := []struct {
		name   string
		mutate func(*StoryEvent)
	}{
		{"negative ordinal", func(e *StoryEvent) { e.Ordinal = -1 }},
		{"no project", func(e *StoryEvent) { e.ProjectID = "  " }},
		{"empty name", func(e *StoryEvent) { e.Name = "" }},
		{"over-long name", func(e *StoryEvent) { e.Name = strings.Repeat("字", MaxNameLength+1) }},
		{"unknown status", func(e *StoryEvent) { e.Status = "confirmed" }},
		{"unknown scope", func(e *StoryEvent) { e.SourceScope = "machine" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			event := base
			testCase.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("a malformed story event was accepted")
			}
		})
	}
}

// TestStoryEventParticipantValidate covers §6.4.
func TestStoryEventParticipantValidate(t *testing.T) {
	base := StoryEventParticipant{
		StoryEventID:  "event-1",
		StoryEntityID: "entity-1",
		Role:          ParticipantActor,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed participant was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryEventParticipant)
	}{
		{"no event", func(p *StoryEventParticipant) { p.StoryEventID = "  " }},
		{"no entity", func(p *StoryEventParticipant) { p.StoryEntityID = "" }},
		{"unknown role", func(p *StoryEventParticipant) { p.Role = "culprit" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			participant := base
			testCase.mutate(&participant)
			if err := participant.Validate(); err == nil {
				t.Fatal("a malformed participant was accepted")
			}
		})
	}
	// One entity may hold several roles in one event; the schema's primary key
	// is (event, entity, role) for that reason, so nothing here collapses them.
	second := base
	second.Role = ParticipantWitness
	if err := second.Validate(); err != nil {
		t.Fatalf("a second role for the same entity and event must be accepted: %v", err)
	}
}

// TestStoryRelationValidate covers §6.5.
func TestStoryRelationValidate(t *testing.T) {
	base := StoryRelation{
		ID:             "relation-1",
		ProjectID:      "project-1",
		Type:           RelationKnows,
		SourceEntityID: "entity-1",
		TargetEntityID: "entity-2",
		Status:         FactCandidate,
		SourceScope:    ScopeOriginal,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed relation was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryRelation)
	}{
		{"no project", func(r *StoryRelation) { r.ProjectID = "" }},
		{"unknown relation type", func(r *StoryRelation) { r.Type = "loves" }},
		{"no source", func(r *StoryRelation) { r.SourceEntityID = "" }},
		{"no target", func(r *StoryRelation) { r.TargetEntityID = "  " }},
		{"unknown status", func(r *StoryRelation) { r.Status = "proposed" }},
		{"unknown scope", func(r *StoryRelation) { r.SourceScope = "derived" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			relation := base
			testCase.mutate(&relation)
			if err := relation.Validate(); err == nil {
				t.Fatal("a malformed story relation was accepted")
			}
		})
	}
}

// TestStoryFactSourceValidate covers §6.6: a fact's evidence names the version
// it quotes from, and a range that is present must be a range.
func TestStoryFactSourceValidate(t *testing.T) {
	base := StoryFactSource{
		ID:                      "fact-1",
		FactType:                FactEvent,
		FactID:                  "event-1",
		ChapterID:               "chapter-1",
		SourceDocumentVersionID: "docv-1",
		SourceKind:              SourceKindText,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed fact source was rejected: %v", err)
	}
	// A user-stated fact has no offsets and no chapter, which §6.6 allows, but
	// it still names the version whose text it is about.
	userStated := base
	userStated.ChapterID = ""
	userStated.SourceKind = SourceKindUser
	if err := userStated.Validate(); err != nil {
		t.Fatalf("a fact source without a span must be accepted: %v", err)
	}
	// A complete range is accepted in either order of assignment.
	start, end := 120, 200
	spanned := base
	spanned.StartOffset = &start
	spanned.EndOffset = &end
	if err := spanned.Validate(); err != nil {
		t.Fatalf("a fact source with a range was rejected: %v", err)
	}
	reversed := base
	reversed.StartOffset = &end
	reversed.EndOffset = &start
	if err := reversed.Validate(); err == nil {
		t.Fatal("an evidence range that ends before it starts was accepted")
	}
	negative := -1
	negativeStart := base
	negativeStart.StartOffset = &negative
	if err := negativeStart.Validate(); err == nil {
		t.Fatal("a negative evidence offset was accepted")
	}
	cases := []struct {
		name   string
		mutate func(*StoryFactSource)
	}{
		{"unknown fact type", func(s *StoryFactSource) { s.FactType = "chapter" }},
		{"no fact", func(s *StoryFactSource) { s.FactID = "  " }},
		{"no document version", func(s *StoryFactSource) { s.SourceDocumentVersionID = "" }},
		{"quote hash is not a digest", func(s *StoryFactSource) { s.QuoteHash = "abc123" }},
		{"unknown evidence kind", func(s *StoryFactSource) { s.SourceKind = "guess" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := base
			testCase.mutate(&source)
			if err := source.Validate(); err == nil {
				t.Fatal("a malformed fact source was accepted")
			}
		})
	}
}

// TestStoryFactConflictResolve covers §6.7's state machine: a conflict is
// created open by an agent, closed once by a user or an explicit rule, and a
// closed conflict is never re-opened.
func TestStoryFactConflictResolve(t *testing.T) {
	newConflict := func() StoryFactConflict {
		return StoryFactConflict{
			ID:            "conflict-1",
			ProjectID:     "project-1",
			LeftFactType:  FactEvent,
			LeftFactID:    "event-1",
			RightFactType: FactRelation,
			RightFactID:   "relation-1",
			Status:        ConflictOpen,
		}
	}
	open := newConflict()
	if err := open.Validate(); err != nil {
		t.Fatalf("a well-formed conflict was rejected: %v", err)
	}

	// The happy path: an open conflict resolves and records who decided what.
	resolved := open
	if err := resolved.Resolve("  The letter arrives in episode two.  ", "user-1"); err != nil {
		t.Fatalf("an open conflict must be resolvable: %v", err)
	}
	if resolved.Status != ConflictResolved {
		t.Fatalf("resolving left the status at %q", resolved.Status)
	}
	if resolved.Resolution != "The letter arrives in episode two." {
		t.Fatalf("the resolution was not stored trimmed: %q", resolved.Resolution)
	}
	if resolved.ResolvedBy != "user-1" {
		t.Fatalf("the resolver was not recorded: %q", resolved.ResolvedBy)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("a resolved conflict must still validate: %v", err)
	}

	// Resolving twice is refused and changes nothing.
	before := resolved
	if err := resolved.Resolve("A second opinion.", "user-2"); err == nil {
		t.Fatal("an already resolved conflict was resolved again")
	}
	if resolved != before {
		t.Fatal("the refused second resolution still modified the conflict")
	}

	// A waived conflict cannot be resolved: §6.7 keeps the waiver as the
	// decision that closed it.
	waived := newConflict()
	waived.Status = ConflictWaived
	if err := waived.Resolve("Actually the letter is late.", "user-1"); err == nil {
		t.Fatal("a waived conflict was resolved")
	}
	if waived.Status != ConflictWaived || waived.Resolution != "" {
		t.Fatalf("the refused resolution modified the waived conflict: %+v", waived)
	}

	// An empty or blank resolution is refused, and the conflict stays open.
	for _, blank := range []string{"", "   ", "\n"} {
		pending := newConflict()
		if err := pending.Resolve(blank, "user-1"); err == nil {
			t.Fatalf("the blank resolution %q was accepted", blank)
		}
		if pending.Status != ConflictOpen || pending.Resolution != "" {
			t.Fatalf("the blank resolution %q closed the conflict anyway: %+v", blank, pending)
		}
	}

	// An unrecognised starting status is refused rather than treated as open.
	unknown := newConflict()
	unknown.Status = "pending"
	if err := unknown.Resolve("A resolution.", "user-1"); err == nil {
		t.Fatal("a conflict with an unknown status was resolved")
	}

	// The conflict category is reported, so the desktop layer can map it.
	closedAgain := resolved
	err := closedAgain.Resolve("Another.", "user-1")
	if err == nil {
		t.Fatal("a resolved conflict was resolved again")
	}
	if category := categoryOf(err); category != CategoryConflict {
		t.Fatalf("a second resolution reports category %q, want %q", category, CategoryConflict)
	}
	if category := categoryOf(InvalidError("x")); category != CategoryInvalidInput {
		t.Fatalf("InvalidError reports category %q", category)
	}
}

// TestStoryFactConflictValidate covers §6.7's shape checks.
func TestStoryFactConflictValidate(t *testing.T) {
	base := StoryFactConflict{
		ID:            "conflict-1",
		ProjectID:     "project-1",
		LeftFactType:  FactEntity,
		LeftFactID:    "entity-1",
		RightFactType: FactEntity,
		RightFactID:   "entity-2",
		Status:        ConflictOpen,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed conflict was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryFactConflict)
	}{
		{"no project", func(c *StoryFactConflict) { c.ProjectID = " " }},
		{"unknown left fact type", func(c *StoryFactConflict) { c.LeftFactType = "chapter" }},
		{"unknown right fact type", func(c *StoryFactConflict) { c.RightFactType = "script" }},
		{"no left fact", func(c *StoryFactConflict) { c.LeftFactID = "" }},
		{"no right fact", func(c *StoryFactConflict) { c.RightFactID = "  " }},
		{"unknown status", func(c *StoryFactConflict) { c.Status = "closed" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			conflict := base
			testCase.mutate(&conflict)
			if err := conflict.Validate(); err == nil {
				t.Fatal("a malformed conflict was accepted")
			}
		})
	}
}

// TestCharacterStateEventOrderInvariant covers §6.8: a state's span must be
// ordered, because continuity queries read the two event orders as an interval.
func TestCharacterStateEventOrderInvariant(t *testing.T) {
	base := CharacterState{
		ID:                "state-1",
		CharacterEntityID: "entity-1",
		FromEventOrder:    3,
		Status:            FactCandidate,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed character state was rejected: %v", err)
	}
	// An open-ended state is the normal case while a story is still being
	// written, and a zero-length span is a state recorded at one event.
	if err := base.Validate(); err != nil {
		t.Fatalf("an open-ended state must be accepted: %v", err)
	}
	sameEvent := 3
	closed := base
	closed.ToEventOrder = &sameEvent
	if err := closed.Validate(); err != nil {
		t.Fatalf("a state ending at its own start event must be accepted: %v", err)
	}
	later := 9
	span := base
	span.ToEventOrder = &later
	if err := span.Validate(); err != nil {
		t.Fatalf("a state spanning several events must be accepted: %v", err)
	}

	earlier := 2
	cases := []struct {
		name   string
		mutate func(*CharacterState)
	}{
		{"no character", func(s *CharacterState) { s.CharacterEntityID = "  " }},
		{"negative start", func(s *CharacterState) { s.FromEventOrder = -1 }},
		{"ends before it starts", func(s *CharacterState) { s.ToEventOrder = &earlier }},
		{"unknown status", func(s *CharacterState) { s.Status = "draft" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			state := base
			testCase.mutate(&state)
			if err := state.Validate(); err == nil {
				t.Fatal("a malformed character state was accepted")
			}
		})
	}
}

// TestOffsetPairIsOptional documents the shared evidence-range rule: absence is
// allowed, a present pair must be ordered.
func TestOffsetPairIsOptional(t *testing.T) {
	start, end := 0, 0
	if err := validateOffsetPair(nil, nil); err != nil {
		t.Fatalf("an absent range must be allowed: %v", err)
	}
	if err := validateOffsetPair(&start, nil); err != nil {
		t.Fatalf("a start without an end must be allowed: %v", err)
	}
	if err := validateOffsetPair(nil, &end); err != nil {
		t.Fatalf("an end without a start must be allowed: %v", err)
	}
	if err := validateOffsetPair(&start, &end); err != nil {
		t.Fatalf("an empty range must be allowed: %v", err)
	}
	negative := -1
	zero := 0
	earlier, later := 10, 40
	for _, testCase := range []struct {
		name       string
		start, end *int
	}{
		{"negative start", &negative, &zero},
		{"negative end", &zero, &negative},
		{"reversed", &later, &earlier},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateOffsetPair(testCase.start, testCase.end); err == nil {
				t.Fatal("a malformed evidence range was accepted")
			}
		})
	}
}

// TestErrorCategories covers the taxonomy the desktop layer maps: each
// constructor reports its own category and AsError finds it through a wrap.
func TestErrorCategories(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{"invalid", InvalidError("x"), CategoryInvalidInput},
		{"not found", NotFoundError(), CategoryNotFound},
		{"conflict", ConflictError("x"), CategoryConflict},
		{"storage", StorageError("x", nil), CategoryStorage},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := categoryOf(testCase.err); got != testCase.want {
				t.Fatalf("category is %q, want %q", got, testCase.want)
			}
		})
	}
	// A wrapped domain error is still found.
	var nilErr *Error
	if nilErr.Error() != "" || nilErr.Unwrap() != nil {
		t.Fatal("a nil domain error must be safe to print and unwrap")
	}
	storage := StorageError("the write failed", InvalidError("cause"))
	if categoryOf(storage) != CategoryStorage {
		t.Fatal("a storage error must keep its category")
	}
	if storage.Unwrap() == nil {
		t.Fatal("a storage error must keep its cause")
	}
}

// TestChapterSourceKindVocabulary pins the values migration 000014 constrains.
func TestChapterSourceKindVocabulary(t *testing.T) {
	for _, kind := range []ChapterSourceKind{ChapterFromHeading, ChapterFromPattern, ChapterWholeDocument, ChapterManual} {
		if !IsValidChapterSourceKind(kind) {
			t.Fatalf("documented chapter source %q rejected", kind)
		}
	}
	for _, rejected := range []ChapterSourceKind{"", "Heading", "guess", "detected"} {
		if IsValidChapterSourceKind(rejected) {
			t.Fatalf("undocumented chapter source %q accepted", rejected)
		}
	}
	if len(ChapterSourceKinds) != 4 {
		t.Fatalf("the source set has %d entries, want 4", len(ChapterSourceKinds))
	}
}

// TestSourceHashIsValidated proves the second hash column is checked as
// strictly as the first, since the two are written independently.
func TestSourceHashIsValidated(t *testing.T) {
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := SourceDocumentVersion{
		ID:                   "docv-1",
		SourceDocumentID:     "doc-1",
		VersionNumber:        1,
		NormalizedTextFileID: digest,
		ContentHash:          digest,
		CharCount:            30000,
		CreatedByType:        CreatedByUser,
	}
	base.SourceHash = strings.Repeat("a", 64)
	if err := base.Validate(); err != nil {
		t.Fatalf("a valid source hash was rejected: %v", err)
	}
	// Empty stays legal: a pasted document has no original file.
	base.SourceHash = ""
	if err := base.Validate(); err != nil {
		t.Fatalf("an empty source hash was rejected: %v", err)
	}
	// A malformed one must be refused, which is what stops a caller that
	// computed only this hash from storing a fragment.
	for _, rejected := range []string{"deadbeef", strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		base.SourceHash = rejected
		if err := base.Validate(); err == nil {
			t.Fatalf("malformed source hash %q was accepted", rejected)
		}
	}
}
