"""Mutation harness for WP-09's production batch and gate.

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

BATCH = "./internal/application/productionpipeline/batch.go"
ROOTPKG = "."

MUTATIONS = [
    (
        BATCH,
        """	input := imageBatchInput{
		Prompt:         prompt,
		Model:          request.ModelName,
		ProviderID:     request.ProviderID,
		Count:          1,
		CandidateIndex: candidate,
		ShotID:         item.ShotID,
		ItemID:         item.ID,
	}""",
        """	input := imageBatchInput{
		Prompt:         prompt,
		Model:          request.ModelName,
		ProviderID:     request.ProviderID,
		Count:          1,
		CandidateIndex: 1,
		ShotID:         item.ShotID,
		ItemID:         item.ID,
	}""",
        ROOTPKG, "TestTheBatchSubmitsTwoCandidatesPerShot",
        "every candidate carries index 1, so a shot's candidates collide into one job",
    ),
    (
        BATCH,
        """		EntityType:       "storyboard_item",
		EntityID:         item.ID,""",
        """		EntityType:       "storyboard_version",
		EntityID:         item.StoryboardVersionID,""",
        ROOTPKG, "TestTheBatchSubmitsTwoCandidatesPerShot",
        "the job is filed under the board rather than the item",
    ),
    (
        BATCH,
        """	if err := s.CheckStoryboardGate(ctx, GateCheckRequest{
		EpisodeID: request.EpisodeID, StoryboardVersionID: versionID,
	}); err != nil {
		return RunImageBatchResult{}, err
	}""",
        """	_ = request.EpisodeID""",
        ROOTPKG, "TestTheGateBlocksABatchWhileTheEpisodeHasNoAnalysedAssets",
        "the batch does not run the gate at all",
    ),
    (
        BATCH,
        """		if version.Status != versioning.StatusApproved {
			return agent.InvalidError("That storyboard version is not approved, so its shots are still under review.")
		}""",
        """		if version.Status == versioning.StatusApproved {
			return agent.InvalidError("That storyboard version is not approved, so its shots are still under review.")
		}""",
        ROOTPKG, "TestTheBatchRefusesAnUnapprovedBoard",
        "an unapproved board is batched anyway",
    ),
    (
        BATCH,
        """		if len(wanted) == 0 || wanted[item.ShotID] || wanted[item.ID] {
			selected = append(selected, item)
		}""",
        """		if len(wanted) == 0 || wanted[item.ShotID] || wanted[item.ID] || true {
			selected = append(selected, item)
		}""",
        ROOTPKG, "TestTheBatchNarrowsToOneShotForASingleShotRedo",
        "the shot narrowing is ignored, so a redo batches the whole board",
    ),
    (
        BATCH,
        """	candidates := request.PerShotCandidates
	if candidates <= 0 {
		candidates = 1
	}
	if candidates > MaxBatchCandidates {
		return RunImageBatchResult{}, agent.InvalidError("A batch may not ask for that many candidates per shot.")
	}""",
        """	candidates := 1""",
        ROOTPKG, "TestTheBatchSubmitsTwoCandidatesPerShot",
        "the candidate count is ignored and every shot gets one",
    ),
    (
        BATCH,
        """	// The gap report is checked FIRST because it is the one the user is most likely to
	// have forgotten: the other three are artifacts they created on purpose.
	if _, err := s.gaps.UnresolvedRequiredItems(ctx, episodeID); err != nil {
		return err
	}""",
        """	_ = episodeID""",
        ROOTPKG, "TestTheGateBlocksABatchWhileTheEpisodeHasNoAnalysedAssets",
        "the gate does not read the gap report",
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
                last = [line for line in vet.stderr.splitlines() if line.strip()]
                if last:
                    print("   " + last[-1])
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
