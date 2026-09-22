"""Mutation harness for the two tests recovered in the WP-09 extraction.

Each mutation is (file, old, new, test). A mutation that does not APPLY or does not COMPILE
proves nothing, so this script verifies both before reporting a survivor.
"""
import hashlib
import io
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ENV = dict(os.environ, GOTOOLCHAIN="go1.25.0", GOSUMDB="sum.golang.org")

MUTATIONS = [
    (
        "internal/application/stagepipeline/service.go",
        """		if err := s.layer.Approve(ctx, attempt.Stage, versionID, trimOrEmpty(request.CreatedByID)); err != nil {
			return workflow.StageRun{}, err
		}
	}
	return s.engine.ApplyGate(ctx, agentruntime.ApplyGateRequest{
		StageRunID: attempt.ID,
		Decision:   request.Decision,
		Actor:      userActor(request.CreatedByID),
	})""",
        """		if err := s.layer.Approve(ctx, attempt.Stage, versionID, trimOrEmpty(request.CreatedByID)); err != nil {
			return workflow.StageRun{}, err
		}
	}
	moved, err := s.engine.ApplyGate(ctx, agentruntime.ApplyGateRequest{
		StageRunID: attempt.ID,
		Decision:   request.Decision,
		Actor:      userActor(request.CreatedByID),
	})
	if err != nil {
		return workflow.StageRun{}, err
	}
	if IsApprovingDecision(request.Decision) {
		if err := s.layer.Approve(ctx, attempt.Stage, trimOrEmpty(request.ArtifactVersionID), trimOrEmpty(request.CreatedByID)); err != nil {
			return workflow.StageRun{}, err
		}
	}
	return moved, nil""",
        "the gate moves the stage before it approves the version",
    ),
    (
        "internal/application/stagepipeline/service.go",
        """		versionID := trimOrEmpty(request.ArtifactVersionID)
		if versionID == "" {
			// Refused rather than skipped, and the refusal says what is missing: a decision
			// that approves nothing is a gate that did not gate, and the caller's next step
			// is to name the version it reviewed.
			return workflow.StageRun{}, agent.InvalidError("An approving decision must name the artifact version it approves.")
		}
""",
        """		versionID := trimOrEmpty(request.ArtifactVersionID)
		if versionID == "" {
			return s.engine.ApplyGate(ctx, agentruntime.ApplyGateRequest{
				StageRunID: attempt.ID, Decision: request.Decision, Actor: userActor(request.CreatedByID),
			})
		}
""",
        "an approving decision with no version is skipped rather than refused",
    ),
    (
        "internal/application/stagepipeline/helpers.go",
        """	if err := json.Unmarshal([]byte(trimmed), &ids); err != nil {
		return nil, agent.InvalidError("That decision's findings could not be read, so the revision cannot be run against them.")
	}""",
        """	if err := json.Unmarshal([]byte(trimmed), &ids); err != nil {
		return nil, nil
	}""",
        "a malformed finding list reads as no findings",
    ),
    (
        "internal/application/stagepipeline/helpers.go",
        """	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, agent.InvalidError("That decision's pinned references could not be read.")
	}""",
        """	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, nil
	}""",
        "a malformed pin list reads as no pins",
    ),
    (
        "internal/application/stagepipeline/helpers.go",
        """		entityID := strings.TrimSpace(ref.EntityID)
		if entityID == "" {
			continue
		}""",
        """		entityID := strings.TrimSpace(ref.EntityID)""",
        "a pin with no identifier is passed on rather than dropped",
    ),
]


def digest(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def run(args):
    return subprocess.run(args, cwd=ROOT, env=ENV, capture_output=True, text=True)


def main():
    survivors = []
    for index, (rel, old, new, label) in enumerate(MUTATIONS, 1):
        path = os.path.join(ROOT, rel.replace("/", os.sep))
        original = io.open(path, encoding="utf-8", newline="").read()
        before = digest(path)
        if old not in original:
            print("%d. %s -- DID NOT APPLY" % (index, label))
            continue
        mutated = original.replace(old, new, 1)
        io.open(path, "w", encoding="utf-8", newline="").write(mutated)
        try:
            vet = run(["go", "vet", "./internal/application/stagepipeline/"])
            if vet.returncode != 0:
                print("%d. %s -- DID NOT COMPILE" % (index, label))
                print("   " + vet.stderr.strip().splitlines()[-1] if vet.stderr.strip() else "")
                continue
            test = run(["go", "test", "./internal/application/stagepipeline/",
                        "-count=1", "-run", "TestTheGateRefuses|TestIssuesOfADecision"])
            status = "KILLED" if test.returncode != 0 else "SURVIVED"
            if test.returncode != 0:
                first = [line for line in test.stdout.splitlines() if line.startswith("--- FAIL")]
                print("%d. %s -- %s (%s)" % (index, label, status, first[0] if first else "failed"))
            else:
                print("%d. %s -- %s" % (index, label, status))
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
