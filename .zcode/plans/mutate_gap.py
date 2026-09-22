"""Mutation harness for WP-09's migration 000018 and the gap domain/service.

Each mutation is (file, old, new, test package, test pattern). A mutation that does not
APPLY or does not COMPILE proves nothing, so this script verifies both before reporting
a survivor, and hash-checks its restore.
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
DOM = "./internal/domain/asset/"

MUTATIONS = [
    (
        "internal/infrastructure/database/storyboard.go",
        """		&item.ContinuityNotes, &item.FirstFrameDescription, &item.LastFrameDescription,
		&item.VideoMotionDescription, &status, &createdAt, &updatedAt, &item.Revision); err != nil {""",
        """		&item.ContinuityNotes, &item.LastFrameDescription, &item.FirstFrameDescription,
		&item.VideoMotionDescription, &status, &createdAt, &updatedAt, &item.Revision); err != nil {""",
        DB, "TestStoryboardRepositoryRoundTrip",
        "the last-frame and first-frame scans are swapped",
    ),
    (
        "internal/infrastructure/database/storyboard.go",
        """		item.DialogueAudioSummary, item.ContinuityNotes, item.FirstFrameDescription,
		item.LastFrameDescription, item.VideoMotionDescription, string(item.Status),""",
        """		item.DialogueAudioSummary, item.ContinuityNotes, item.FirstFrameDescription,
		item.LastFrameDescription, item.ContinuityNotes, string(item.Status),""",
        DB, "TestStoryboardRepositoryRoundTrip",
        "the video-motion column is written from the continuity notes",
    ),
    (
        "internal/domain/asset/gap.go",
        """	satisfied := strings.TrimSpace(i.AssetID) != ""
	if i.Status == GapSatisfied && !satisfied {
		return InvalidError("A satisfied gap item must name the asset that satisfies it.")
	}""",
        """	satisfied := strings.TrimSpace(i.AssetID) != ""
	_ = satisfied""",
        DOM, "TestASatisfiedItemMustNameTheAssetThatSatisfiesIt",
        "a satisfied line no longer has to name its asset",
    ),
    (
        "internal/domain/asset/gap.go",
        """func (i GapItem) Unsatisfied() bool {
	return i.Required && i.Status == GapMissing
}""",
        """func (i GapItem) Unsatisfied() bool {
	return i.Status == GapMissing
}""",
        DOM, "TestTheBlockingPredicateNeedsBothHalves",
        "the blocking predicate drops the required half",
    ),
    (
        "internal/domain/asset/gap.go",
        """	if len(items) == 0 {
		return InvalidError("A gap report with no items cannot be approved, because it analysed nothing.")
	}
""",
        "",
        DOM, "TestAGapReportCannotBeApprovedWhileSomethingRequiredIsMissing",
        "a report with no items is approvable",
    ),
    (
        "internal/infrastructure/database/asset_gap.go",
        """		if affected == 0 {
			// Nothing moved, so the caller's copy of the status was stale. Refused
			// rather than reported as success: an approval that changed no row is not
			// an approval.
			return asset.ConflictError("This gap report changed in another window. Reload it and try again.")
		}""",
        """		if affected == 0 {
			return nil
		}""",
        DB, "TestApproveGapReportRefusesAStaleStatus",
        "a stale status is reported as a successful approval",
    ),
    (
        "internal/infrastructure/database/asset_gap.go",
        """		_, err := conn.ExecContext(ctx, `UPDATE asset_gap_reports
			SET status = 'superseded', updated_at = ?, revision = revision + 1
			WHERE episode_id = ? AND status = 'approved'`, formatTime(at), episodeID)
		if err != nil {
			return storageError("ASSET_WRITE_FAILED", "The previous gap report could not be superseded.", err)
		}
""",
        "",
        DB, "TestApproveGapReportSupersedesThePreviousAndRecordsTheTrace",
        "the previous report is not superseded, so two reports stay approved",
    ),
    (
        "internal/application/assets/gap.go",
        """	if len(items) == 0 {
		return asset.GapReport{}, nil, asset.InvalidError("A gap report must analyse at least one story fact.")
	}
""",
        "",
        APP, "TestAnAnalysisWithNoLinesIsRefused",
        "the service stores an analysis with no lines",
    ),
    (
        "internal/application/assets/gap.go",
        """	report, found, err := s.gaps.CurrentApprovedGapReport(ctx, episode)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, asset.ConflictError("This episode has no approved gap report, so nothing says which assets its script needs.")
	}""",
        """	report, found, err := s.gaps.CurrentApprovedGapReport(ctx, episode)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}""",
        APP, "TestUnresolvedRequiredItemsRefusesWhenNothingIsApproved",
        "an unanalysed episode reports no missing assets",
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
