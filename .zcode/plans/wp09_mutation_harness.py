#!/usr/bin/env python
"""WP-09 mutation-testing harness.

For every mutation:
  1. read the file, hash it (sha256)
  2. verify each anchor occurs exactly the expected number of times (a silent
     str.replace no-op proves nothing)
  3. write the mutated file
  4. run `go vet ./...` (whole module: it also compiles _test.go files, so a
     mutation that breaks a test file is caught)
  5. run `go test -count=1 -json ./...` (or npm test for TS) and collect the
     failing test names
  6. restore the file and assert the hash matches the original

Usage: python .zcode/plans/wp09_mutation_harness.py [name-substring ...]
"""

import hashlib
import json
import os
import re
import subprocess
import sys
import time

ROOT = r"F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903"
WEB = os.path.join(ROOT, "web")

GOTOOLCHAIN = "go1.25.0"
GOSUMDB = "sum.golang.org"

ENV = dict(os.environ)
ENV["GOTOOLCHAIN"] = GOTOOLCHAIN
ENV["GOSUMDB"] = GOSUMDB


def sha256(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def read(path):
    with open(path, "r", encoding="utf-8", newline="") as handle:
        return handle.read()


def write(path, content):
    with open(path, "w", encoding="utf-8", newline="") as handle:
        handle.write(content)


def run(cmd, cwd, timeout=1800):
    started = time.time()
    proc = subprocess.run(
        cmd,
        cwd=cwd,
        env=ENV,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=timeout,
        shell=False,
    )
    return proc.returncode, proc.stdout.decode("utf-8", "replace"), time.time() - started


def adapt_edits(content, edits):
    """Rewrite anchors and replacements in the file's own line ending.

    `web/src/services/desktop/monoform-bridge.ts` is CRLF while every Go file is
    LF, and an anchor written with bare \\n against a CRLF file is a silent
    no-op. This makes the anchor text line-ending agnostic.
    """
    if "\r\n" not in content:
        return edits
    return [
        (anchor.replace("\n", "\r\n"), replacement.replace("\n", "\r\n"), expected)
        for anchor, replacement, expected in edits
    ]


# --------------------------------------------------------------------------
# mutations
# --------------------------------------------------------------------------
# Each mutation: name, file (relative to ROOT), runner ("go" | "npm"), list of
# (anchor, replacement, expected_count), a description, and the test this
# mutation is *supposed* to be caught by.

MUTATIONS = []


def mutation(name, path, edits, note, expect, runner="go"):
    MUTATIONS.append(
        {
            "name": name,
            "path": path,
            "edits": edits,
            "note": note,
            "expect": expect,
            "runner": runner,
        }
    )


BATCH = "internal/application/productionpipeline/batch.go"
CAND = "internal/application/productionpipeline/candidate.go"
ROUTE = "internal/desktop/script_binding.go"
MECH = "internal/application/stagepipeline/service.go"
HELPERS = "internal/application/stagepipeline/helpers.go"
GAP = "internal/application/assets/gap.go"
IMG = "internal/infrastructure/providers/mock_image.go"
BRIDGE = "web/src/services/desktop/monoform-bridge.ts"

# ---- the batch -----------------------------------------------------------

mutation(
    "B01-batch-gate-call-removed",
    BATCH,
    [((
        "\tif err := s.CheckStoryboardGate(ctx, GateCheckRequest{\n"
        "\t\tEpisodeID: request.EpisodeID, StoryboardVersionID: versionID,\n"
        "\t}); err != nil {\n"
        "\t\treturn RunImageBatchResult{}, err\n"
        "\t}\n"
    ), "", 1)],
    "RunImageBatch no longer calls CheckStoryboardGate",
    "TestTheGateBlocksABatchWhileTheEpisodeHasNoAnalysedAssets / TestTheGateRefusesAnEpisodeWithNoGapAnalysis",
)

mutation(
    "B02-candidate-index-always-1",
    BATCH,
    [("\t\tCandidateIndex: candidate,\n\t\tShotID:         item.ShotID,", "\t\tCandidateIndex: 1,\n\t\tShotID:         item.ShotID,", 1)],
    "submitCandidate writes CandidateIndex 1 into every job input, so both candidates hash to one job",
    "TestTheBatchSubmitsTwoCandidatesPerShotAndDoesNotRepeatThem",
)

mutation(
    "B03-max-batch-candidates-refusal-removed",
    BATCH,
    [(
        "\tif candidates > MaxBatchCandidates {\n"
        "\t\treturn RunImageBatchResult{}, agent.InvalidError(\"A batch may not ask for that many candidates per shot.\")\n"
        "\t}\n",
        "",
        1,
    )],
    "the `candidates > MaxBatchCandidates` bound is gone",
    "(no test named)",
)

mutation(
    "B04-gap-read-removed-from-gate",
    BATCH,
    [(
        "\t// The gap report is checked FIRST because it is the one the user is most likely to\n"
        "\t// have forgotten: the other three are artifacts they created on purpose.\n"
        "\tif _, err := s.gaps.UnresolvedRequiredItems(ctx, episodeID); err != nil {\n"
        "\t\treturn err\n"
        "\t}\n",
        "",
        1,
    )],
    "CheckStoryboardGate no longer reads UnresolvedRequiredItems",
    "TestTheGateRefusesAnEpisodeWithNoGapAnalysis",
)

mutation(
    "B05-approved-status-check-removed",
    BATCH,
    [
        (
            "\t\tversion, err := s.storyboard.GetStoryboardVersion(ctx, request.StoryboardVersionID)\n"
            "\t\tif err != nil {\n"
            "\t\t\treturn err\n"
            "\t\t}\n"
            "\t\tif version.Status != versioning.StatusApproved {\n"
            "\t\t\treturn agent.InvalidError(\"That storyboard version is not approved, so its shots are still under review.\")\n"
            "\t\t}\n",
            "\t\tif _, err := s.storyboard.GetStoryboardVersion(ctx, request.StoryboardVersionID); err != nil {\n"
            "\t\t\treturn err\n"
            "\t\t}\n",
            1,
        ),
        (
            "\t\"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning\"\n",
            "",
            1,
        ),
    ],
    "the storyboard version's StatusApproved check is gone (import also dropped so it compiles)",
    "TestTheBatchRefusesAnUnapprovedBoard",
)

mutation(
    "B06-semaphore-removed",
    BATCH,
    [
        (
            "\tlimit := s.maxImageConcurrency\n"
            "\tif limit <= 0 {\n"
            "\t\tlimit = DefaultMaxImageConcurrency\n"
            "\t}\n",
            "",
            1,
        ),
        ("\tslots := make(chan struct{}, limit)\n", "", 1),
        ("\t\t\tslots <- struct{}{}\n", "", 1),
        ("\t\t\t\tdefer func() { <-slots }()\n", "", 1),
    ],
    "submissions are unbounded: the semaphore acquire and release are removed",
    "TestTheBatchRespectsItsConcurrencyLimit",
)

mutation(
    "B07-approved-storyboard-lookup-removed",
    BATCH,
    [(
        "\tapprovedID, err := s.storyboard.ApprovedStoryboardVersionID(ctx, storyboardID)\n"
        "\tif err != nil {\n"
        "\t\treturn err\n"
        "\t}\n"
        "\tif trimmed(approvedID) == \"\" {\n"
        "\t\treturn agent.InvalidError(\"This episode has no approved storyboard version, so there is nothing to image.\")\n"
        "\t}\n",
        "",
        1,
    )],
    "the StoryboardID branch of the gate is gone",
    "(no test named)",
)

mutation(
    "B08-empty-shot-selection-refusal-removed",
    BATCH,
    [(
        "\tif len(selected) == 0 {\n"
        "\t\treturn RunImageBatchResult{}, agent.InvalidError(\"None of the named shots belong to that storyboard version.\")\n"
        "\t}\n",
        "",
        1,
    )],
    "a batch naming only shots outside the board now succeeds with an empty result",
    "TestTheBatchNarrowsToOneShotForASingleShotRedo",
)

# ---- candidate collection -------------------------------------------------

mutation(
    "C01-succeeded-skip-removed",
    CAND,
    [
        (
            "\t\tif record.Status != job.StatusSucceeded {\n"
            "\t\t\t// Not a refusal: the job is queued, running or failed, and the caller's next step\n"
            "\t\t\t// for each of those is different and its own.\n"
            "\t\t\tcontinue\n"
            "\t\t}\n",
            "",
            1,
        ),
        (
            "\t\"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job\"\n",
            "",
            1,
        ),
    ],
    "CollectBatchResults writes a version for a job that has NOT succeeded",
    "(no test named)",
)

mutation(
    "C02-isDuplicateCandidate-always-false",
    CAND,
    [(
        "func isDuplicateCandidate(err error) bool {\n"
        "\tdomainErr, ok := asset.AsError(err)\n"
        "\treturn ok && domainErr.Duplicate\n"
        "}",
        "func isDuplicateCandidate(err error) bool {\n"
        "\treturn false\n"
        "}",
        1,
    )],
    "a duplicate candidate write is reported as a failure instead of a duplicate",
    "TestACollectedJobBecomesACandidateVersion",
)

mutation(
    "C03-usage-consumer-shot",
    CAND,
    [("\t\t\tConsumerType:   asset.ConsumerStoryboardPanel,", "\t\t\tConsumerType:   asset.ConsumerShot,", 1)],
    "the usage row records ConsumerShot rather than ConsumerStoryboardPanel",
    "TestACollectedJobBecomesACandidateVersion",
)

mutation(
    "C04-no-file-refusal-removed",
    CAND,
    [(
        "\t\tif len(files) == 0 {\n"
        "\t\t\t// A succeeded job with no readable file is a job whose result the collector\n"
        "\t\t\t// cannot turn into an image. Refused rather than skipped, because the caller's\n"
        "\t\t\t// next step is to look at the job rather than to call again.\n"
        "\t\t\treturn collected, agent.InvalidError(\"A succeeded job's result named no committed file.\")\n"
        "\t\t}\n",
        "",
        1,
    )],
    "a succeeded job naming no file is collected rather than refused",
    "(no test named)",
)

# ---- stage routing --------------------------------------------------------

mutation(
    "R01-pipelineFor-script-always",
    ROUTE,
    [(
        "func (b *DramaBinding) pipelineFor(stage string) StagePipeline {\n"
        "\tif appscriptpipeline.IsScriptStage(stage) {\n"
        "\t\treturn b.stagePipeline()\n"
        "\t}\n"
        "\tif appproductionpipeline.IsProductionStage(stage) {\n"
        "\t\treturn b.productionStagePipeline()\n"
        "\t}\n"
        "\t// An unknown stage goes to the SCRIPT pipeline when it exists, so the refusal names the\n"
        "\t// stage rather than a missing pipeline: a caller that misspelled a stage should learn\n"
        "\t// that, not that the build has no agent stack.\n"
        "\tif pipeline := b.stagePipeline(); pipeline != nil {\n"
        "\t\treturn pipeline\n"
        "\t}\n"
        "\treturn b.productionStagePipeline()\n"
        "}",
        "func (b *DramaBinding) pipelineFor(stage string) StagePipeline {\n"
        "\treturn b.stagePipeline()\n"
        "}",
        1,
    )],
    "pipelineFor always returns the script pipeline, so a production stage runs the wrong layer",
    "(no test named)",
)

mutation(
    "R02-stageStateFor-script-always",
    ROUTE,
    [(
        "\tif appproductionpipeline.IsProductionStage(request.Stage) {\n"
        "\t\treturn appproductionpipeline.StateFields{\n"
        "\t\t\tEpisodeID:             request.EpisodeID,\n"
        "\t\t\tScriptVersionID:       request.ScriptVersionID,\n"
        "\t\t\tDirectorPlanVersionID: request.DirectorPlanVersionID,\n"
        "\t\t\tStoryboardID:          request.StoryboardID,\n"
        "\t\t\tStoryboardVersionID:   request.StoryboardVersionID,\n"
        "\t\t\tStoryboardItemID:      request.StoryboardItemID,\n"
        "\t\t\tAssetGapReportID:      request.AssetGapReportID,\n"
        "\t\t\tShotIDs:               request.ShotIDs,\n"
        "\t\t}\n"
        "\t}\n",
        "",
        1,
    )],
    "stageStateFor always returns the script StateFields, so a production stage gets no state",
    "(no test named)",
)

mutation(
    "R03-pipelineForStageRun-script-always",
    ROUTE,
    [(
        "\tattempt, err := workflow.GetStage(b.context(), strings.TrimSpace(stageRunID))\n"
        "\tif err != nil {\n"
        "\t\treturn nil\n"
        "\t}\n"
        "\treturn b.pipelineFor(string(attempt.Stage))\n",
        "\tif _, err := workflow.GetStage(b.context(), strings.TrimSpace(stageRunID)); err != nil {\n"
        "\t\treturn nil\n"
        "\t}\n"
        "\treturn b.stagePipeline()\n",
        1,
    )],
    "pipelineForStageRun always returns the script pipeline, so supervision/gate/revision reach the wrong layer",
    "(no test named)",
)

# ---- the mechanism --------------------------------------------------------

mutation(
    "M01-park-always-reviewing",
    MECH,
    [(
        "\tparked := workflow.StageReviewing\n"
        "\tif strings.TrimSpace(agents.Supervision) == \"\" {\n"
        "\t\tparked = workflow.StageWaitingUser\n"
        "\t}\n",
        "\tparked := workflow.StageReviewing\n",
        1,
    )],
    "every attempt parks at reviewing, removing the unsupervised-stage fix",
    "TestCanaryProductionStagesToAnApprovedBoard",
)

mutation(
    "M02-evidence-json-field-name",
    HELPERS,
    [("\t\t\t} `json:\"evidence\"`\n", "\t\t\t} `json:\"evidenceJson\"`\n", 1)],
    "IssuesFromOutcome reads the old `evidenceJson` name, so evidence is always dropped",
    "TestTheSupervisorLocatesTheRowAtFault",
)

mutation(
    "M03-encode-evidence-empty",
    HELPERS,
    [(
        "\tif len(evidence) == 0 {\n"
        "\t\treturn \"\"\n"
        "\t}\n"
        "\tencoded, err := json.Marshal(evidence)\n"
        "\tif err != nil {\n"
        "\t\t// Marshalling a slice of two-field structs cannot fail; an empty string is returned\n"
        "\t\t// rather than a panic, so a bug here is a missing citation rather than a dead run.\n"
        "\t\treturn \"\"\n"
        "\t}\n"
        "\treturn string(encoded)\n",
        "\treturn \"\"\n",
        1,
    )],
    "EncodeEvidence always returns an empty string",
    "TestTheSupervisorLocatesTheRowAtFault",
)

mutation(
    "M04-approving-decision-skips-approval",
    MECH,
    [("\tif IsApprovingDecision(request.Decision) {\n", "\tif false && IsApprovingDecision(request.Decision) {\n", 1)],
    "ApplyUserGate records the decision but never approves the artifact",
    "(no test named)",
)

# ---- the gap report -------------------------------------------------------

mutation(
    "G01-empty-report-refusal-removed",
    GAP,
    [(
        "\t// An analysis with NO lines is refused here rather than at approval, so the model\n"
        "\t// learns it before a user is shown a report that claims to have analysed nothing.\n"
        "\t// The refusal is stated once, in the domain, because the approval path makes it too.\n"
        "\tif len(items) == 0 {\n"
        "\t\treturn asset.GapReport{}, nil, asset.InvalidError(\"A gap report must analyse at least one story fact.\")\n"
        "\t}\n",
        "",
        1,
    )],
    "CreateGapReport accepts an analysis with no lines",
    "TestAnAnalysisWithNoLinesIsRefused",
)

mutation(
    "G02-unresolved-returns-empty",
    GAP,
    [(
        "\tif !found {\n"
        "\t\treturn nil, asset.ConflictError(\"This episode has no approved gap report, so nothing says which assets its script needs.\")\n"
        "\t}\n",
        "\tif !found {\n"
        "\t\treturn []asset.GapItem{}, nil\n"
        "\t}\n",
        1,
    )],
    "UnresolvedRequiredItems reports 'nothing missing' when no report is approved",
    "TestUnresolvedRequiredItemsRefusesWhenNothingIsApproved",
)

mutation(
    "G03-ordinal-always-one",
    GAP,
    [("\t\t\tOrdinal:         index + 1,\n", "\t\t\tOrdinal:         1,\n", 1)],
    "every gap line is written with ordinal 1",
    "TestTheOrdinalsAndIdentifiersAreAssignedRatherThanAccepted",
)

# ---- the mock image -------------------------------------------------------

mutation(
    "I01-render-ignores-index",
    IMG,
    [("\tseed := hasher.Sum64() + uint64(index)*2654435761\n", "\tseed := hasher.Sum64()\n", 1)],
    "render ignores the candidate index, so two candidates of one prompt produce identical bytes",
    "TestTheMockImageProducesDecodableDeterministicBytes",
)

mutation(
    "I02-generate-ignores-count",
    IMG,
    [(
        "\tcount := request.Count\n"
        "\tif count <= 0 {\n"
        "\t\tcount = 1\n"
        "\t}\n",
        "\tcount := 1\n",
        1,
    )],
    "Generate ignores request.Count and always produces one image",
    "TestTheMockImageProducesDecodableDeterministicBytes",
)

mutation(
    "I03-empty-prompt-refusal-removed",
    IMG,
    [(
        "\tprompt := strings.TrimSpace(request.Prompt)\n"
        "\tif prompt == \"\" {\n"
        "\t\treturn appjobs.ImageOutcome{}, provider.NewInvalidInputError()\n"
        "\t}\n",
        "\tprompt := strings.TrimSpace(request.Prompt)\n",
        1,
    )],
    "an empty prompt is accepted and rendered",
    "TestTheMockImageRefusesAnEmptyPrompt",
)

# ---- the bridge (TypeScript) ---------------------------------------------

mutation(
    "BR01-origin-check-removed",
    BRIDGE,
    [(
        "    if (!isAllowedMonoformOrigin(input.origin, input.selfOrigin)) {\n"
        "        return { ok: false, reason: \"wrong-origin\" };\n"
        "    }\n",
        "",
        1,
    )],
    "validateMonoformMessage no longer checks the origin",
    "a message from another origin is refused before its payload is read",
    runner="npm",
)

mutation(
    "BR02-nonce-check-removed",
    BRIDGE,
    [(
        "    if (typeof envelope.nonce !== \"string\" || envelope.nonce !== input.nonce) {\n"
        "        return { ok: false, reason: \"wrong-nonce\" };\n"
        "    }\n",
        "",
        1,
    )],
    "validateMonoformMessage no longer checks the nonce",
    "a wrong nonce is refused, so a message replayed from a previous mount is rejected",
    runner="npm",
)

mutation(
    "BR03-version-check-removed",
    BRIDGE,
    [(
        "    if (envelope.schemaVersion !== MONOFORM_SCHEMA_VERSION) {\n"
        "        return { ok: false, reason: \"wrong-version\" };\n"
        "    }\n",
        "",
        1,
    )],
    "validateMonoformMessage no longer checks the schema version",
    "a different schema version is refused rather than parsed optimistically",
    runner="npm",
)

mutation(
    "BR04-payload-bound-removed",
    BRIDGE,
    [(
        "    if (blob.size <= 0 || blob.size > limit) {\n"
        "        return null;\n"
        "    }\n",
        "    if (blob.size <= 0) {\n"
        "        return null;\n"
        "    }\n",
        1,
    )],
    "the payload size bound is gone",
    "an oversized payload is refused, and the bound is what refuses it",
    runner="npm",
)


# --------------------------------------------------------------------------
# execution
# --------------------------------------------------------------------------

GO_FAIL_RE = re.compile(r"^--- FAIL: (\S+)", re.MULTILINE)
VET_ERROR_RE = re.compile(r"(^# |\.go:\d+:\d+:|error:|ERROR:)")


def run_go_gate():
    code, out, secs = run(["go", "vet", "./..."], ROOT)
    return code, out, secs


def run_go_tests():
    code, out, secs = run(["go","test","-count=1","-json","./..."], ROOT)
    failures = []
    build_failures = set()
    messages = {}
    # `go test -json` distinguishes the two cases explicitly:
    #   - a package whose TESTS failed emits Action "fail" with a "Test" field
    #     on the per-test events, and no "FailedBuild";
    #   - a package that failed to BUILD emits Action "build-fail" (and a
    #     package-level "fail" carrying "FailedBuild").
    # Matching "build failed" in the output text would confuse the two, because
    # the test-failure output contains that phrase too.
    for line in out.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            event = json.loads(line)
        except ValueError:
            continue
        action = event.get("Action")
        package = event.get("Package", "")
        if action == "build-fail" or event.get("FailedBuild"):
            build_failures.add(event.get("ImportPath") or package)
        elif action == "fail" and event.get("Test"):
            failures.append(package.split("/")[-1] + "." + event["Test"])
        elif action == "output" and event.get("Test"):
            text = event.get("Output", "")
            if "    " in text and (".go:" in text or "want" in text):
                messages.setdefault(event["Test"], text.strip()[:240])
    return code, failures, sorted(build_failures), out, secs, messages


def run_npm_tests():
    code, out, secs = run(["npm.cmd", "test"], WEB, timeout=900)
    failures = []
    seen = set()
    # Node's TAP summary prints each failure twice (once in the detail block with
    # a trailing duration, once in the trailing "failing tests:" list), so the
    # collected names are de-duplicated on the name without its "(1.2ms)" tail.
    for match in re.finditer(r"^(?:not ok \d+ - |✖ )(.+)$", out, re.MULTILINE):
        name = re.sub(r"\s*\(\d+(?:\.\d+)?ms\)\s*$", "", match.group(1)).strip()
        if name and not name.startswith("failing tests") and name not in seen:
            seen.add(name)
            failures.append(name)
    build_failed = "Transform failed" in out or ("ERROR:" in out and "esbuild" in out)
    return code, failures, build_failed, out, secs


def main():
    only = sys.argv[1:]
    selected = [m for m in MUTATIONS if not only or any(o in m["name"] for o in only)]

    print("=" * 78)
    print("BASELINE: go vet ./...")
    code, out, secs = run_go_gate()
    print("  exit=%d in %.1fs" % (code, secs))
    if code != 0:
        print(out[-4000:])
        sys.exit("baseline vet is not green; stopping")

    print("BASELINE: go test -count=1 ./...")
    code, failures, build_failures, out, secs, _ = run_go_tests()
    print("  exit=%d in %.1fs failures=%s build=%s" % (code, secs, failures, build_failures))
    if code != 0:
        sys.exit("baseline tests are not green; stopping")

    print("BASELINE: npm test")
    code, failures, build_failed, out, secs = run_npm_tests()
    print("  exit=%d in %.1fs failures=%s" % (code, secs, failures))
    if code != 0:
        sys.exit("baseline npm tests are not green; stopping")
    print("=" * 78)

    results = []
    for item in selected:
        full = os.path.join(ROOT, item["path"])
        original = read(full)
        original_hash = sha256(full)
        record = {
            "name": item["name"],
            "path": item["path"],
            "note": item["note"],
            "expect": item["expect"],
            "runner": item["runner"],
            "hash_before": original_hash,
        }

        # ---- apply ----
        content = original
        applied = True
        apply_note = ""
        for anchor, replacement, expected_count in adapt_edits(original, item["edits"]):
            found = content.count(anchor)
            if found != expected_count:
                applied = False
                apply_note = "anchor found %d times, expected %d: %r" % (
                    found,
                    expected_count,
                    anchor[:90],
                )
                break
            content = content.replace(anchor, replacement, expected_count)
        if not applied or content == original:
            record["verdict"] = "DID-NOT-APPLY"
            record["detail"] = apply_note or "the edit produced no change"
            results.append(record)
            print("\n[%s] DID-NOT-APPLY: %s" % (item["name"], record["detail"]))
            continue
        write(full, content)
        try:
            # ---- compile gate: go vet compiles the test files too ----
            if item["runner"] == "go":
                code, out, secs = run_go_gate()
                if code != 0:
                    record["verdict"] = "DID-NOT-COMPILE"
                    record["detail"] = out.strip()[-1200:]
                    print("\n[%s] DID-NOT-COMPILE (go vet, %.1fs)" % (item["name"], secs))
                    print(indent(out.strip()[-1200:]))
                    # still restore below
                    continue

            # ---- test ----
            if item["runner"] == "go":
                code, failures, build_failures, out, secs, messages = run_go_tests()
                if build_failures:
                    record["verdict"] = "DID-NOT-COMPILE"
                    record["detail"] = "packages failed to build: " + ", ".join(build_failures[:10])
                    print("\n[%s] DID-NOT-COMPILE (build, %.1fs): %s" % (item["name"], secs, build_failures[:6]))
                    continue
                record["exit_code"] = code
                record["failures"] = failures
                record["messages"] = {k: messages[k] for k in failures if k in messages}
                record["seconds"] = round(secs, 1)
                if code != 0:
                    record["verdict"] = "KILLED"
                else:
                    record["verdict"] = "SURVIVED"
                print(
                    "\n[%s] %s (%.1fs, %d failing tests)"
                    % (item["name"], record["verdict"], secs, len(failures))
                )
                for name in failures[:12]:
                    print("    FAIL " + name)
            else:
                code, failures, build_failed, out, secs = run_npm_tests()
                if build_failed:
                    record["verdict"] = "DID-NOT-COMPILE"
                    record["detail"] = "esbuild failed"
                    print("\n[%s] DID-NOT-COMPILE (esbuild, %.1fs)" % (item["name"], secs))
                    continue
                record["exit_code"] = code
                record["failures"] = failures
                record["seconds"] = round(secs, 1)
                record["verdict"] = "KILLED" if code != 0 else "SURVIVED"
                print("\n[%s] %s (%.1fs)" % (item["name"], record["verdict"], secs))
                for name in failures[:6]:
                    print("    FAIL " + name)
        finally:
            write(full, original)
            restored = sha256(full)
            record["hash_after"] = restored
            record["restored"] = restored == original_hash
            if not record["restored"]:
                print("!!! RESTORE FAILED for %s" % full)

        results.append(record)

    # ---- summary ----
    print("\n" + "=" * 78)
    print("SUMMARY")
    print("=" * 78)
    for record in results:
        print(
            "%-38s %-16s %s"
            % (
                record["name"],
                record["verdict"],
                ", ".join(record.get("failures", [])[:4]) or record.get("detail", "")[:70],
            )
        )
    bad_restore = [r["name"] for r in results if not r.get("restored", True)]
    print("\nfiles not restored: %s" % (bad_restore or "none"))

    out_path = os.path.join(ROOT, ".zcode", "plans", "wp09_mutation_results.json")
    with open(out_path, "w", encoding="utf-8") as handle:
        json.dump(results, handle, indent=2)
    print("results written to %s" % out_path)


def indent(text):
    return "\n".join("    | " + line for line in text.splitlines())


if __name__ == "__main__":
    main()
