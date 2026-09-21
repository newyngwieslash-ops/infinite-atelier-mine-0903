package database

import (
	"context"
	"testing"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// attach_wp09_test.go covers the path AC-ASSET-002 describes: a generation job's output
// becomes an asset version whose every provenance fact is stored.
//
// It runs against the real schema because that is the only place the five lineage columns
// exist, and whether they are WRITTEN is the whole question.

// TestAttachJobResultRecordsEveryProvenanceFact covers AC-ASSET-002's list through the
// command path and back out of the database.
//
// The list is "physical file, hash, job, provider/model, prompt, parent refs, agent/stage,
// asset usage", and the assertion is on what a subsequent READ returns: a command that
// set a field on the struct it returned without storing it would pass an assertion made
// against that struct and fail this one.
func TestAttachJobResultRecordsEveryProvenanceFact(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeImage, Name: "Panel 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	version, files, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID:              record.ID,
		JobID:                "job-1",
		Prompt:               "a corridor at dusk, medium shot",
		ProviderConfigID:     "provider-1",
		ModelConfigID:        "model-1",
		ModelParameters:      `{"size":"1024x1024"}`,
		Seed:                 "seed-42",
		ParentAssetVersionID: "parent-version-1",
		VariantType:          "derived_from",
		SourceAgentRunID:     "run-1",
		CreatedByID:          "agent-1",
		Files:                []appassets.AttachJobFile{{FileHash: testHashA, Role: asset.RolePrimary}},
	})
	if err != nil {
		t.Fatalf("attaching the job result: %v", err)
	}
	// The status is CANDIDATE, which is the point of the command: §9.5's panel approval
	// reads a candidate, and before WP-09 nothing in the build wrote this status at all.
	if version.Status != asset.VersionCandidate {
		t.Fatalf("the version is %q, want candidate", version.Status)
	}
	if len(files) != 1 || files[0].FileHash != testHashA {
		t.Fatalf("the files are %+v", files)
	}
	// Read back through the REPOSITORY rather than the returned struct: everything below
	// is a column, and the command's own return value would not show a mapper that
	// dropped one on the way in.
	stored, err := repository.GetVersion(ctx, version.ID)
	if err != nil {
		t.Fatalf("reading the version back: %v", err)
	}
	for name, pair := range map[string][2]string{
		"job":          {stored.GenerationJobID, "job-1"},
		"provider":     {stored.ProviderConfigID, "provider-1"},
		"model":        {stored.ModelConfigID, "model-1"},
		"prompt":       {stored.Prompt, "a corridor at dusk, medium shot"},
		"seed":         {stored.Seed, "seed-42"},
		"parent":       {stored.ParentAssetVersionID, "parent-version-1"},
		"variant type": {stored.VariantType, "derived_from"},
		"agent run":    {stored.SourceAgentRunID, "run-1"},
		"author":       {stored.CreatedByID, "agent-1"},
		"model params": {stored.ModelParameters, `{"size":"1024x1024"}`},
		"created by":   {string(stored.CreatedByType), string(asset.CreatedByAgent)},
	} {
		if pair[0] != pair[1] {
			t.Errorf("the %s column came back as %q, want %q", name, pair[0], pair[1])
		}
	}
	// The FILE link is real, so the "physical file / hash" half of the criterion is
	// satisfied by rows rather than by the version's own fields.
	links, err := repository.ListFiles(ctx, version.ID)
	if err != nil {
		t.Fatalf("reading the files: %v", err)
	}
	if len(links) != 1 || links[0].FileHash != testHashA || links[0].Role != asset.RolePrimary {
		t.Fatalf("the stored files are %+v", links)
	}
}

// TestAttachJobResultNumbersTheVersionAfterTheHighest covers the numbering rule.
//
// A candidate is appended to whatever the asset already has, including the version
// `CreateAsset` wrote. Reusing a number would be refused by the schema's unique
// constraint, and the refusal would reach the user as a storage failure rather than as
// the thing they could act on.
func TestAttachJobResultNumbersTheVersionAfterTheHighest(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, first, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeImage, Name: "Panel 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-1",
		Files: []appassets.AttachJobFile{{FileHash: testHashA}},
	})
	if err != nil {
		t.Fatalf("attaching the first result: %v", err)
	}
	third, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-2",
		Files: []appassets.AttachJobFile{{FileHash: testHashB}},
	})
	if err != nil {
		t.Fatalf("attaching the second result: %v", err)
	}
	if first.VersionNumber != 1 || second.VersionNumber != 2 || third.VersionNumber != 3 {
		t.Fatalf("the version numbers are %d, %d, %d", first.VersionNumber, second.VersionNumber, third.VersionNumber)
	}
	// Two candidates coexist: attaching a result does NOT supersede the previous one,
	// which is what makes AC-BOARD-003's "每 Shot 2 candidates" possible at all.
	stored, err := repository.GetVersion(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != asset.VersionCandidate {
		t.Fatalf("the first candidate became %q when a second was attached", stored.Status)
	}
}

// TestAttachJobResultRefusesWhatCannotBeAudited covers the three refusals.
//
// Each names a different missing fact, and each would otherwise produce a version that
// looks fine and cannot be traced — which is exactly what AC-ASSET-002 exists to stop.
func TestAttachJobResultRefusesWhatCannotBeAudited(t *testing.T) {
	service, _, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeImage, Name: "Panel 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// No job: a version claiming to be generated with nothing that generated it.
	if _, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, Files: []appassets.AttachJobFile{{FileHash: testHashA}},
	}); err == nil {
		t.Fatal("a generated version with no job was accepted")
	}
	// No file: a version nothing can display or approve.
	if _, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-1",
	}); err == nil {
		t.Fatal("a generated version with no file was accepted")
	}
	// No asset: a version belonging to nothing.
	if _, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		JobID: "job-1", Files: []appassets.AttachJobFile{{FileHash: testHashA}},
	}); err == nil {
		t.Fatal("a generated version naming no asset was accepted")
	}
	// And a file hash that was never committed is refused by the schema's foreign key,
	// which is the one check a caller cannot talk their way past.
	if _, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-1",
		Files: []appassets.AttachJobFile{{FileHash: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}},
	}); err == nil {
		t.Fatal("a version advertising uncommitted bytes was accepted")
	}
}

// TestTheUserStartedJobIsRecordedAsTheSystem covers the producer distinction.
//
// A job the user started by hand and a job an agent asked for are different answers to
// "where did this image come from", and FR-100's audit reads the difference. A version
// recorded as an agent's when no run exists would put a provider's output in the model's
// column.
func TestTheUserStartedJobIsRecordedAsTheSystem(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeImage, Name: "Panel 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	version, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-1",
		Files: []appassets.AttachJobFile{{FileHash: testHashA}},
	})
	if err != nil {
		t.Fatalf("attaching a user-started job's result: %v", err)
	}
	stored, err := repository.GetVersion(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CreatedByType != asset.CreatedBySystem {
		t.Fatalf("a job with no agent run is recorded as %q", stored.CreatedByType)
	}
	if stored.SourceAgentRunID != "" {
		t.Fatalf("the version cites the run %q", stored.SourceAgentRunID)
	}
}

// TestAttachJobResultDefaultsTheFirstFileToPrimary covers the role default a caller
// relies on when it states only the hashes.
func TestAttachJobResultDefaultsTheFirstFileToPrimary(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeImage, Name: "Panel 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	version, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-1",
		Files: []appassets.AttachJobFile{{FileHash: testHashA}, {FileHash: testHashB}},
	})
	if err != nil {
		t.Fatalf("attaching two files: %v", err)
	}
	links, err := repository.ListFiles(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("the stored files are %+v", links)
	}
	byHash := map[string]asset.FileRole{}
	for _, link := range links {
		byHash[link.FileHash] = link.Role
	}
	if byHash[testHashA] != asset.RolePrimary || byHash[testHashB] != asset.RoleReference {
		t.Fatalf("the default roles are %+v", byHash)
	}
}
