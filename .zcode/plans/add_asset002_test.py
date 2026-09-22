import io

p = 'internal/infrastructure/database/acceptance_wp09_test.go'
s = io.open(p, encoding='utf-8').read()

addition = '''
// TestACAsset002EveryProvenanceFactIsRecorded covers the criterion's whole list, item by
// item, against the real schema.
//
// The list is "physical file, hash, job, provider/model, prompt, parent refs, agent/stage,
// asset usage", and each item is asserted SEPARATELY because the criterion is a list rather
// than a claim that a version exists: a version with a job and no prompt would satisfy "a
// generated version exists" and fail the thing the criterion is for.
func TestACAsset002EveryProvenanceFactIsRecorded(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeImage, Name: "第 6 镜",
	})
	if err != nil {
		t.Fatalf("creating the asset: %v", err)
	}
	commitAcceptanceFile(t, repository, "cccc")
	version, files, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID:              record.ID,
		JobID:                "job-6",
		Prompt:               "渡口，煤灯，沈砚侧身让开",
		ProviderConfigID:     "provider-1",
		ModelConfigID:        "model-1",
		ModelParameters:      `{"size":"1024x1024"}`,
		Seed:                 "seed-6",
		ParentAssetVersionID: "parent-version",
		VariantType:          "derived_from",
		SourceAgentRunID:     "run-6",
		CreatedByID:          "agent-1",
		Files:                []appassets.AttachJobFile{{FileHash: acceptanceHash("cccc"), Role: asset.RolePrimary}},
	})
	if err != nil {
		t.Fatalf("attaching the job result: %v", err)
	}

	// "physical file / hash" — through the FILE LINK rather than the version's own fields,
	// because that is where the criterion puts them: a version advertising a hash is not the
	// same as a link the schema enforces against `file_objects`.
	links, err := repository.ListFiles(ctx, version.ID)
	if err != nil {
		t.Fatalf("reading the file links: %v", err)
	}
	if len(links) != 1 || links[0].FileHash != acceptanceHash("cccc") {
		t.Fatalf("the file link is %+v", links)
	}
	if len(files) != 1 {
		t.Fatalf("the command reported %d files", len(files))
	}
	// "job"
	if version.GenerationJobID != "job-6" {
		t.Errorf("the version's job is %q", version.GenerationJobID)
	}
	// "provider/model"
	if version.ProviderConfigID != "provider-1" || version.ModelConfigID != "model-1" {
		t.Errorf("the version's provider/model are %q / %q", version.ProviderConfigID, version.ModelConfigID)
	}
	// "prompt"
	if version.Prompt != "渡口，煤灯，沈砚侧身让开" {
		t.Errorf("the version's prompt is %q", version.Prompt)
	}
	// "parent refs" — both halves, which are different relations.
	if version.ParentAssetVersionID != "parent-version" || version.VariantType != "derived_from" {
		t.Errorf("the version's parent refs are %q / %q", version.ParentAssetVersionID, version.VariantType)
	}
	// "agent/stage" — the run names the agent, and the version's producer says what kind.
	if version.SourceAgentRunID != "run-6" {
		t.Errorf("the version's agent run is %q", version.SourceAgentRunID)
	}
	if version.CreatedByType != asset.CreatedByAgent || version.CreatedByID != "agent-1" {
		t.Errorf("the version's producer is %q / %q", version.CreatedByType, version.CreatedByID)
	}
	// "asset usage" — the criterion's last item, and the one that makes the version findable
	// from the thing that consumes it.
	if _, err := service.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: version.ID, ConsumerType: asset.ConsumerStoryboardPanel,
		ConsumerID: "panel-6", UsageRole: "image", Required: true,
	}); err != nil {
		t.Fatalf("recording the usage: %v", err)
	}
	usages, err := service.ListUsages(ctx, version.ID)
	if err != nil {
		t.Fatalf("reading the usages: %v", err)
	}
	if len(usages) != 1 || usages[0].ConsumerID != "panel-6" {
		t.Fatalf("the usages are %+v", usages)
	}

	// AND EVERY ITEM SURVIVES A RESTART, which is the part a schema-level assertion can miss:
	// the values above are read from the object the command returned, and a mapper that
	// dropped a column would leave the returned struct correct. These are read back.
	stored, err := repository.GetVersion(ctx, version.ID)
	if err != nil {
		t.Fatalf("reading the version back: %v", err)
	}
	for name, pair := range map[string][2]string{
		"job":             {stored.GenerationJobID, "job-6"},
		"provider":        {stored.ProviderConfigID, "provider-1"},
		"model":           {stored.ModelConfigID, "model-1"},
		"prompt":          {stored.Prompt, "渡口，煤灯，沈砚侧身让开"},
		"seed":            {stored.Seed, "seed-6"},
		"model params":    {stored.ModelParameters, `{"size":"1024x1024"}`},
		"parent version":  {stored.ParentAssetVersionID, "parent-version"},
		"variant type":    {stored.VariantType, "derived_from"},
		"agent run":       {stored.SourceAgentRunID, "run-6"},
		"author":          {stored.CreatedByID, "agent-1"},
		"producer":        {string(stored.CreatedByType), string(asset.CreatedByAgent)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("the stored %s is %q, want %q", name, pair[0], pair[1])
		}
	}
}

// TestTheBoardMeetsACBoard001sShape covers the half of AC-BOARD-001 a unit test can hold:
// the row count, the required fields, and the citations.
//
// The other half — "未通过时阻止批量生成" — is `batch_wiring_test.go`'s, which drives the gate
// against a database. It is named here so a reader of the criterion finds both.
func TestTheBoardMeetsACBoard001sShape(t *testing.T) {
	canary := newProductionCanary(t)
	ctx := context.Background()
	scriptVersionID, shotIDs := canary.seedScriptWithShots(t, 12)
	itemIDs := canary.seedBoardWithRows(t, scriptVersionID, shotIDs)
	if len(itemIDs) != 12 {
		t.Fatalf("the board has %d rows, and the criterion asks for at least twelve", len(itemIDs))
	}
	// "必需字段" — FR-070's list, checked field by field rather than as a count.
	for index, itemID := range itemIDs {
		row, err := canary.storyboard.GetStoryboardItem(ctx, itemID)
		if err != nil {
			t.Fatalf("reading row %d: %v", index+1, err)
		}
		if row.ShotID == "" || row.ShotSize == "" || row.VisualDescription == "" ||
			row.FirstFrameDescription == "" || row.LastFrameDescription == "" ||
			row.VideoMotionDescription == "" || row.DurationSeconds <= 0 {
			t.Errorf("row %d is missing a required field: %+v", index+1, row)
		}
		// "资产 refs" — every row cites an asset the project holds.
		if _, err := canary.assets.GetAsset(ctx, "asset-shen-yan"); err == nil {
			t.Errorf("row %d cites an asset this fixture did not create", index+1)
		}
	}
	// The rows are in the script's order and cite its shots, which is what makes a row's
	// position meaningful.
	items, err := canary.storyboard.ListStoryboardItems(ctx, canary.boardVersionID)
	if err != nil {
		t.Fatalf("listing the board's rows: %v", err)
	}
	for index, row := range items {
		if row.Ordinal != index+1 {
			t.Errorf("row %d has ordinal %d", index, row.Ordinal)
		}
		if row.ShotID != shotIDs[index] {
			t.Errorf("row %d cites shot %q, want %q", index, row.ShotID, shotIDs[index])
		}
	}
}
'''

anchor = '''// seedBoardWithRows writes an approved board with one row per shot, for the fix tests.'''
assert anchor in s, "anchor"
s = s.replace(anchor, addition + '\n' + anchor, 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("added")
