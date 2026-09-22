"""Mutation harness for WP-09's asset provenance, lineage and impact work.

Each mutation is (file, old, new, package, pattern, label). A mutation that does not
APPLY or does not COMPILE proves nothing, so this verifies both before reporting a
survivor, and hash-checks its restore.
"""
import hashlib
import io
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ENV = dict(os.environ, GOTOOLCHAIN="go1.25.0", GOSUMDB="sum.golang.org")

DB = "./internal/infrastructure/database/"
APP = "./internal/application/assets/"
DOM = "./internal/domain/staleness/"
ROOTPKG = "."

MUTATIONS = [
    (
        "internal/infrastructure/database/assets.go",
        """		version.BasedOnVersionID, version.ParentAssetVersionID, version.VariantType,
		version.Prompt, version.NegativePrompt, version.ProviderConfigID,
		version.ModelConfigID, version.ModelParameters, version.Seed, version.GenerationJobID,
		version.SourceAgentRunID, version.Metadata, string(version.CreatedByType),
		version.CreatedByID, version.LegacyMetadata, formatTime(version.CreatedAt))""",
        """		version.BasedOnVersionID, version.ParentAssetVersionID, version.VariantType,
		version.Prompt, version.NegativePrompt, version.ProviderConfigID,
		version.ModelConfigID, version.ModelParameters, version.Seed, version.GenerationJobID,
		version.SourceAgentRunID, version.Metadata, string(version.CreatedByType),
		version.CreatedByType, version.LegacyMetadata, formatTime(version.CreatedAt))""",
        DB, "TestAttachJobResultRecordsEveryProvenanceFact",
        "the created_by_id column is written from the producer type",
    ),
    (
        "internal/infrastructure/database/assets.go",
        """		&version.ModelConfigID, &version.ModelParameters, &version.Seed, &version.GenerationJobID,
		&version.SourceAgentRunID, &version.Metadata, &version.CreatedByType,
		&version.CreatedByID, &version.LegacyMetadata, &createdAt); err != nil {""",
        """		&version.ModelConfigID, &version.ModelParameters, &version.Seed, &version.GenerationJobID,
		&version.SourceAgentRunID, &version.Metadata, &version.CreatedByType,
		&version.Seed, &version.LegacyMetadata, &createdAt); err != nil {""",
        DB, "TestAttachJobResultRecordsEveryProvenanceFact",
        "the created_by_id column is read back into seed",
    ),
    (
        "internal/application/assets/attach.go",
        """	createdBy := asset.CreatedBySystem
	if strings.TrimSpace(request.SourceAgentRunID) != "" {
		createdBy = asset.CreatedByAgent
	}""",
        """	createdBy := asset.CreatedByAgent""",
        DB, "TestTheUserStartedJobIsRecordedAsTheSystem",
        "every generated version is attributed to an agent",
    ),
    (
        "internal/application/assets/attach.go",
        """	version := asset.Version{
		ID:                   id,
		AssetID:              assetID,
		VersionNumber:        highest + 1,
		Status:               asset.VersionCandidate,""",
        """	version := asset.Version{
		ID:                   id,
		AssetID:              assetID,
		VersionNumber:        highest + 1,
		Status:               asset.VersionDraft,""",
        DB, "TestAttachJobResultRecordsEveryProvenanceFact",
        "a generated version is stored as a draft rather than a candidate",
    ),
    (
        "internal/application/assets/attach.go",
        """	if len(request.Files) == 0 {
		// A generated version with no file is a version nothing can display or approve,
		// and `CanApprove` would refuse it later. Refusing here names the actual problem:
		// the job's result had no committed object in it.
		return asset.Version{}, nil, asset.InvalidError("A generated version needs at least one committed file.")
	}
""",
        "",
        DB, "TestAttachJobResultRefusesWhatCannotBeAudited",
        "a generated version with no file is accepted",
    ),
    (
        "internal/application/assets/attach.go",
        """			role = asset.RoleReference
			if index == 0 {
				role = asset.RolePrimary
			}""",
        """			role = asset.RoleReference
			_ = index""",
        DB, "TestAttachJobResultDefaultsTheFirstFileToPrimary",
        "the first file no longer defaults to primary",
    ),
    (
        "internal/application/assets/propagate.go",
        """	replaced := strings.TrimSpace(replacedVersionID)
	if replaced == "" {
		return nil
	}""",
        """	replaced := strings.TrimSpace(replacedVersionID)
	if replaced == "" {
		replaced = "no-such-version"
	}""",
        ROOTPKG, "TestTheApprovalImpactNames",
        "an empty replaced id becomes a version that does not exist",
    ),
    (
        "internal/domain/staleness/staleness.go",
        """	ArtifactStoryboardPanel: {ArtifactStoryboardItem, ArtifactAssetVersion},""",
        """	ArtifactStoryboardPanel: {ArtifactStoryboardItem},""",
        DOM, "TestDirectDependentsAreTheSchemaEdges",
        "the panel no longer consumes the asset version it approved",
    ),
    (
        "internal/infrastructure/database/staleness.go",
        """		dependent: staleness.ArtifactStoryboardPanel,
		upstream:  staleness.ArtifactAssetVersion,
		queries: []string{
			`SELECT id FROM storyboard_panel_versions WHERE approved_image_asset_version_id = ?`,
		},""",
        """		dependent: staleness.ArtifactStoryboardPanel,
		upstream:  staleness.ArtifactAssetVersion,
		queries: []string{
			`SELECT id FROM storyboard_panel_versions WHERE id = ?`,
		},""",
        ROOTPKG, "TestApprovingAnAssetVersionMarks",
        "the panel query ignores which image was approved",
    ),
]


def digest(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def run(args):
    return subprocess.run(args, cwd=ROOT, env=ENV, capture_output=True, text=True)


def main():
    survivors = []
    for index, (rel, old, new, package, pattern, label) in enumerate(MUTATIONS, 1):
        path = os.path.join(ROOT, rel.replace("/", os.sep))
        original = io.open(path, encoding="utf-8", newline="").read()
        before = digest(path)
        if old not in original:
            print("%d. %s -- DID NOT APPLY" % (index, label))
            continue
        io.open(path, "w", encoding="utf-8", newline="").write(original.replace(old, new, 1))
        try:
            vet = run(["go", "vet", package])
            if vet.returncode != 0:
                print("%d. %s -- DID NOT COMPILE" % (index, label))
                continue
            test = run(["go", "test", package, "-count=1", "-run", pattern])
            if test.returncode != 0:
                first = [line for line in test.stdout.splitlines() if line.startswith("--- FAIL")]
                print("%d. %s -- KILLED (%s)" % (index, label, first[0] if first else "failed"))
            else:
                print("%d. %s -- SURVIVED" % (index, label))
                survivors.append(label)
        finally:
            io.open(path, "w", encoding="utf-8", newline="").write(original)
            assert digest(path) == before, "restore failed for " + rel
    print()
    if survivors:
        print("SURVIVORS (%d):" % len(survivors))
        for label in survivors:
            print("  - " + label)
        return 1
    print("no survivors: every mutation was killed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
