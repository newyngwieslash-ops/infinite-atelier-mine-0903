import io

path = "internal/application/script/structure_service_test.go"
src = io.open(path, encoding="utf-8").read()

replacements = []

# --- Skeleton lock test ---
replacements.append((
'''	for _, testCase := range cases {
		store := newMemoryStore()
		service := newTestService(store)
		base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
			EpisodeID: "episode-1", OpeningHook: "hook", CoreConflict: "conflict",''',
'''	for _, testCase := range cases {
		store := newMemoryStore()
		service := newTestService(store)
		episode := seedEpisode(t, service)
		base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
			EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",'''))

replacements.append((
'''		revision := CreateStorySkeletonVersionRequest{
			EpisodeID: "episode-1", BasedOnVersionID: base.ID, OpeningHook: "hook",''',
'''		revision := CreateStorySkeletonVersionRequest{
			EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook",'''))

replacements.append((
'''		history, err := service.ListVersions(ctx, scriptdomain.FamilyStorySkeleton, "episode-1")''',
'''		history, err := service.ListVersions(ctx, scriptdomain.FamilyStorySkeleton, episode.ID)'''))

# --- Respecting every lock ---
replacements.append((
'''	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", OpeningHook: "hook", CoreConflict: "conflict",''',
'''	episode := seedEpisode(t, service)
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",'''))

replacements.append((
'''	revision, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", BasedOnVersionID: base.ID, OpeningHook: "hook\\n",''',
'''	revision, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook\\n",'''))

# --- Skeleton selection link test ---
replacements.append((
'''	version, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", OpeningHook: "hook",
		SelectedEventIDs: []string{" event-2 ", "event-1", "  "},
	})''',
'''	episode := seedEpisode(t, service)
	version, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{" event-2 ", "event-1", "  "},
	})'''))

replacements.append((
'''	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", OpeningHook: "hook",
		SelectedEventIDs: []string{"event-1", "event-1"},
	}); err == nil {''',
'''	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-1", "event-1"},
	}); err == nil {'''))

# --- Strategy link test ---
replacements.append((
'''	version, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: "episode-1", StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-3", Treatment: scriptdomain.TreatmentReordered},''',
'''	episode := seedEpisode(t, service)
	version, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-3", Treatment: scriptdomain.TreatmentReordered},'''))

replacements.append((
'''	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: "episode-1", StrategySummary: "x",
		EventLinks: []scriptdomain.StrategyEventLink{{StoryEventID: "event-1"}},
	}); err == nil {''',
'''	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "x",
		EventLinks: []scriptdomain.StrategyEventLink{{StoryEventID: "event-1"}},
	}); err == nil {'''))

replacements.append((
'''	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: "episode-1", StrategySummary: "x",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRemoved},''',
'''	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "x",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRemoved},'''))

# --- Strategy lock test ---
replacements.append((
'''		store := newMemoryStore()
		service := newTestService(store)
		base, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: "episode-1", StrategySummary: "balanced", AdaptationMode: scriptdomain.AdaptationBalanced,''',
'''		store := newMemoryStore()
		service := newTestService(store)
		episode := seedEpisode(t, service)
		base, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, StrategySummary: "balanced", AdaptationMode: scriptdomain.AdaptationBalanced,'''))

replacements.append((
'''		revision := CreateAdaptationStrategyVersionRequest{
			EpisodeID: "episode-1", BasedOnVersionID: base.ID, StrategySummary: "balanced",''',
'''		revision := CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, BasedOnVersionID: base.ID, StrategySummary: "balanced",'''))

replacements.append((
'''		if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: "episode-1", BasedOnVersionID: base.ID, StrategySummary: "balanced",''',
'''		if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, BasedOnVersionID: base.ID, StrategySummary: "balanced",'''))

# --- First version has no locks ---
replacements.append((
'''	first, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("a first version with no locks was refused: %v", err)
	}
	// A lock on a DIFFERENT version does not reach it.
	other, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", OpeningHook: "another",
	})''',
'''	episode := seedEpisode(t, service)
	first, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("a first version with no locks was refused: %v", err)
	}
	// A lock on a DIFFERENT version does not reach it.
	other, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "another",
	})'''))

replacements.append((
'''	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: "episode-1", BasedOnVersionID: first.ID, OpeningHook: "hook", EndingHook: "changed",
	}); err != nil {''',
'''	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: first.ID, OpeningHook: "hook", EndingHook: "changed",
	}); err != nil {'''))

# --- seedAnotherScriptVersion: use the real fixture rather than a guessed script id ---
replacements.append((
'''// seedAnotherScriptVersion writes a second script version for the same script, so a test that
// refused a write can try again without colliding with the version it already filled.
func seedAnotherScriptVersion(t *testing.T, service *Service) scriptdomain.ScriptVersion {
	t.Helper()
	ctx := context.Background()
	history, err := service.ListVersions(ctx, scriptdomain.FamilyScript, "script-1")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	versions, ok := history.([]scriptdomain.ScriptVersion)
	if !ok {
		t.Fatalf("ListVersions returned %T", history)
	}
	highest := 0
	scriptID := ""
	for _, version := range versions {
		if version.VersionNumber > highest {
			highest = version.VersionNumber
		}
		scriptID = version.ScriptID
	}
	if scriptID == "" {
		t.Fatal("the fixture wrote no script version")
	}
	version, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: scriptID, BasedOnVersionID: versions[0].ID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	return version
}''',
'''// seedAnotherScriptVersion writes an extra EMPTY script version for the script the fixture made.
//
// It exists because a refused structure write leaves its version empty, so a test that wants to
// observe a second refusal cannot reuse the first version — it would be testing the refusal against
// a version that is still empty for a reason the test already established.
func seedAnotherScriptVersion(t *testing.T, service *Service, scriptID string) scriptdomain.ScriptVersion {
	t.Helper()
	version, err := service.CreateScriptVersion(context.Background(), CreateScriptVersionRequest{
		ScriptID:               scriptID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	return version
}

// seedEpisode writes an episode for the tests that need one before a skeleton can exist.
//
// Every skeleton and strategy version hangs off an episode, and the service reads that row before it
// writes — which is why an invented episode id fails with a not-found rather than producing a
// version whose foreign key would be refused.
func seedEpisode(t *testing.T, service *Service) scriptdomain.Episode {
	t.Helper()
	episode, err := service.CreateEpisode(context.Background(), CreateEpisodeRequest{
		ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot",
	})
	if err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	return episode
}'''))

for old, new in replacements:
    if old not in src:
        raise SystemExit("NOT FOUND:\n" + old[:200])
    src = src.replace(old, new, 1)

# The three call sites of seedAnotherScriptVersion take the script id the fixture made.
src = src.replace("seedAnotherScriptVersion(t, service)", "seedAnotherScriptVersion(t, service, version.ScriptID)")

io.open(path, "w", encoding="utf-8", newline="\n").write(src)
print("patched", src.count("episode-1"), "episode-1 left")
