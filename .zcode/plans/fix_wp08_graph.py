import io

path = "internal/application/script/structure_service_test.go"
src = io.open(path, encoding="utf-8").read()

pairs = [
    # Skeleton lock test: the graph it cites.
    ('''		episode := seedEpisode(t, service)
		base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
			EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",''',
     '''		episode := seedEpisode(t, service)
		seedStoryGraph(store, "event-1", "event-2")
		base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
			EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",'''),

    # Respecting every lock.
    ('''	episode := seedEpisode(t, service)
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",''',
     '''	episode := seedEpisode(t, service)
	seedStoryGraph(store, "event-1")
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",'''),

    # Skeleton selection link test.
    ('''	episode := seedEpisode(t, service)
	version, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{" event-2 ", "event-1", "  "},
	})''',
     '''	episode := seedEpisode(t, service)
	seedStoryGraph(store, "event-1", "event-2")
	version, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{" event-2 ", "event-1", "  "},
	})'''),

    # Strategy link test.
    ('''	episode := seedEpisode(t, service)
	version, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",''',
     '''	episode := seedEpisode(t, service)
	seedStoryGraph(store, "event-1", "event-2", "event-3")
	version, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",'''),

    # Strategy lock test.
    ('''		episode := seedEpisode(t, service)
		base, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, StrategySummary: "balanced", AdaptationMode: scriptdomain.AdaptationBalanced,''',
     '''		episode := seedEpisode(t, service)
		seedStoryGraph(store, "event-1")
		base, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, StrategySummary: "balanced", AdaptationMode: scriptdomain.AdaptationBalanced,'''),

    # Study treatment test's second half reuses the same episode, so the second and third calls need
    # the graph too — it is already seeded above, so nothing to add.
]

for old, new in pairs:
    if old not in src:
        raise SystemExit("NOT FOUND:\n" + old[:200])
    src = src.replace(old, new, 1)

io.open(path, "w", encoding="utf-8", newline="\n").write(src)
print("patched")
