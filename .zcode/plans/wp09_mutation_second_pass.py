#!/usr/bin/env python
"""Second pass: the mutations that did not compile first time (rewritten so they
do), plus extra load-bearing mutations the first battery did not cover.

Same contract as the main harness: hash, anchor-verify, vet, test, restore,
assert the hash matches.
"""

import hashlib
import json
import os
import sys

import importlib.util

ROOT = r"F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903"
HARNESS = os.path.join(ROOT, ".zcode", "plans", "wp09_mutation_harness.py")

spec = importlib.util.spec_from_file_location("harness", HARNESS)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

M = mod.MUTATIONS
mutation = mod.mutation

BATCH = "internal/application/productionpipeline/batch.go"
CAND = "internal/application/productionpipeline/candidate.go"
ROUTE = "internal/desktop/script_binding.go"
MECH = "internal/application/stagepipeline/service.go"
HELPERS = "internal/application/stagepipeline/helpers.go"
GAP = "internal/application/assets/gap.go"
LAYER = "internal/application/productionpipeline/layer.go"
SVC = "internal/application/productionpipeline/service.go"
IMG = "internal/infrastructure/providers/mock_image.go"
BRIDGE = "web/src/services/desktop/monoform-bridge.ts"

FIRST_PASS = {m["name"] for m in M}

# ---- G03 rewritten so it compiles: keep `index` used ----
mutation(
    "G03b-ordinal-always-one",
    GAP,
    [(
        "\t\t\tOrdinal:         index + 1,\n",
        "\t\t\tOrdinal:         1 + index*0,\n",
        1,
    )],
    "every gap line is written with ordinal 1 (index kept used so it compiles)",
    "TestTheOrdinalsAndIdentifiersAreAssignedRatherThanAccepted",
)

# ---- extra batch clauses ----
mutation(
    "B09-prompt-drops-the-items-own-fields",
    BATCH,
    [(
        "\tfor _, part := range []string{\n"
        "\t\titem.VisualDescription,\n"
        "\t\titem.ActionDescription,\n"
        "\t\titem.ShotSize,\n"
        "\t\titem.CameraAngle,\n"
        "\t\titem.FirstFrameDescription,\n"
        "\t} {\n",
        "\tfor _, part := range []string{\n"
        "\t} {\n",
        1,
    )],
    "imagePromptFor renders no clause from the item, so every shot's prompt is the suffix alone",
    "(no test named)",
)

mutation(
    "B10-prompt-suffix-replaces-the-item",
    BATCH,
    [(
        "\tif trimmed := strings.TrimSpace(suffix); trimmed != \"\" {\n"
        "\t\tparts = append(parts, trimmed)\n"
        "\t}\n"
        "\treturn strings.Join(parts, \". \")\n",
        "\treturn strings.TrimSpace(suffix)\n",
        1,
    )],
    "the caller's prompt suffix REPLACES the shot's own descriptions",
    "(no test named)",
)

mutation(
    "B11-per-shot-selection-ignored",
    BATCH,
    [(
        "\t\tif len(wanted) == 0 || wanted[item.ShotID] || wanted[item.ID] {\n"
        "\t\t\tselected = append(selected, item)\n"
        "\t\t}\n",
        "\t\tselected = append(selected, item)\n",
        1,
    )],
    "ShotIDs narrows nothing: every shot in the board is submitted",
    "TestTheBatchNarrowsToOneShotForASingleShotRedo",
)

mutation(
    "B12-batch-makes-its-own-storyboard-id",
    BATCH,
    [(
        "\t\treturn RunImageBatchResult{}, agent.InvalidError(\"A batch must name the project its jobs belong to.\")\n",
        "",
        1,
    )],
    "the ProjectID requirement is gone, so jobs are filed under an empty project",
    "(no test named)",
)

mutation(
    "B13-gate-first-arg-swapped",
    BATCH,
    [(
        "\tif request.StoryboardVersionID != \"\" {\n",
        "\tif false && request.StoryboardVersionID != \"\" {\n",
        1,
    )],
    "the version-status arm of the gate is unreachable",
    "TestTheBatchRefusesAnUnapprovedBoard",
)

# ---- extra candidate clauses ----
mutation(
    "C05-asset-by-item-ignored",
    CAND,
    [(
        "\t\tassetID := strings.TrimSpace(request.AssetByItem[itemID])\n"
        "\t\tif assetID == \"\" {\n"
        "\t\t\treturn collected, agent.InvalidError(\"A collected job must name the asset its candidate belongs to.\")\n"
        "\t\t}\n",
        "\t\tassetID := \"\"\n"
        "\t\tfor _, candidate := range request.AssetByItem {\n"
        "\t\t\tassetID = candidate\n"
        "\t\t\tbreak\n"
        "\t\t}\n",
        1,
    )],
    "the item→asset mapping is ignored: the first asset in the map receives every candidate",
    "(no test named)",
)

mutation(
    "C06-usage-not-written",
    CAND,
    [(
        "\t\tif _, err := s.assets.AddUsage(ctx, appassets.AddUsageRequest{\n"
        "\t\t\tAssetVersionID: version.ID,\n"
        "\t\t\tConsumerType:   asset.ConsumerStoryboardPanel,\n"
        "\t\t\tConsumerID:     itemID,\n"
        "\t\t\tUsageRole:      role,\n"
        "\t\t\tRequired:       false,\n"
        "\t\t}); err != nil {\n"
        "\t\t\treturn collected, err\n"
        "\t\t}\n",
        "",
        1,
    )],
    "the collection writes no usage, so no panel can find its candidates",
    "TestACollectedJobBecomesACandidateVersion",
)

mutation(
    "C07-seed-dropped",
    CAND,
    [("\t\t\tSeed:             input.Seed,\n", "\t\t\tSeed:             \"\",\n", 1)],
    "the seed the batch recorded is dropped from the candidate version (AC-ASSET-002)",
    "(no test named)",
)

mutation(
    "C08-prompt-dropped",
    CAND,
    [("\t\t\tPrompt:           input.Prompt,\n", "\t\t\tPrompt:           \"\",\n", 1)],
    "the prompt is dropped from the candidate version (AC-ASSET-002)",
    "(no test named)",
)

mutation(
    "C09-approve-ignores-the-candidate-list",
    CAND,
    [(
        "\t\tCandidateVersionIDs:         request.CandidateVersionIDs,\n",
        "\t\tCandidateVersionIDs:         []string{strings.TrimSpace(request.ApprovedImageAssetVersionID)},\n",
        1,
    )],
    "approval passes a one-element candidate list, so §9.5's membership rule is vacuous",
    "impact_wiring_test.go's panel approval",
)

# ---- extra routing clauses ----
mutation(
    "R04-script-stage-routes-to-production",
    ROUTE,
    [(
        "func (b *DramaBinding) pipelineFor(stage string) StagePipeline {\n"
        "\tif appscriptpipeline.IsScriptStage(stage) {\n"
        "\t\treturn b.stagePipeline()\n"
        "\t}\n"
        "\tif appproductionpipeline.IsProductionStage(stage) {\n"
        "\t\treturn b.productionStagePipeline()\n"
        "\t}\n",
        "func (b *DramaBinding) pipelineFor(stage string) StagePipeline {\n"
        "\tif appscriptpipeline.IsScriptStage(stage) {\n"
        "\t\treturn b.productionStagePipeline()\n"
        "\t}\n"
        "\tif appproductionpipeline.IsProductionStage(stage) {\n"
        "\t\treturn b.stagePipeline()\n"
        "\t}\n",
        1,
    )],
    "the two slots are swapped: every stage runs on the other layer's pipeline",
    "(no test named)",
)

mutation(
    "R05-manual-edit-ignores-the-attempts-stage",
    MECH,
    [(
        "\tif request.Stage != \"\" && request.Stage != attempt.Stage {\n"
        "\t\treturn StageResult{}, agent.InvalidError(\"That attempt belongs to a different stage than the one named.\")\n"
        "\t}\n",
        "",
        1,
    )],
    "a manual edit naming the wrong stage is accepted, so content lands in another stage's artifact",
    "revision_test.go (partial)",
)

# ---- extra mechanism clauses ----
mutation(
    "M05-episode-scope-check-removed",
    MECH,
    [(
        "\tif err := s.assertEpisodeInProject(ctx, request.ProjectID, request.EpisodeID); err != nil {\n"
        "\t\treturn StageResult{}, err\n"
        "\t}\n",
        "",
        1,
    )],
    "a manual edit no longer checks the episode belongs to the project (SECURITY boundary)",
    "(no test named)",
)

mutation(
    "M06-control-inert-comment",
    MECH,
    [(
        "\tparked := workflow.StageReviewing\n",
        "\t// HARNESS CONTROL: an inert change, to prove the harness reports no false kill.\n"
        "\tparked := workflow.StageReviewing\n",
        1,
    )],
    "control: an inert comment, which must SURVIVE (a KILL here would be a false positive)",
    "(control — SURVIVED is the expected and correct outcome)",
)

mutation(
    "M07-revision-with-no-decision-allowed",
    MECH,
    [(
        "\tif !ok {\n"
        "\t\t// A revision with no decision is a caller error rather than an empty revision: the\n"
        "\t\t// whole point of naming the attempt is that a user decided something about it.\n"
        "\t\treturn nil, nil, agent.InvalidError(\"That stage attempt has no user decision to revise against.\")\n"
        "\t}\n",
        "\tif !ok {\n"
        "\t\treturn nil, nil, nil\n"
        "\t}\n",
        1,
    )],
    "a revision naming an attempt nobody decided about is accepted, so a FIX runs with nothing to fix",
    "TestRevisionContextReadsTheFindingsAndRefusesWithoutADecision",
)

mutation(
    "M08-no-supervisor-still-parked-for-review",
    MECH,
    [(
        "\tparked := workflow.StageReviewing\n"
        "\tif strings.TrimSpace(agents.Supervision) == \"\" {\n"
        "\t\tparked = workflow.StageWaitingUser\n"
        "\t}\n",
        "\tparked := workflow.StageWaitingUser\n"
        "\tif strings.TrimSpace(agents.Supervision) == \"\" {\n"
        "\t\tparked = workflow.StageReviewing\n"
        "\t}\n",
        1,
    )],
    "the branch is INVERTED: a supervised stage skips its review, an unsupervised one waits for one",
    "TestCanaryProductionStagesToAnApprovedBoard",
)

# ---- extra gap clauses ----
mutation(
    "G04-unresolved-returns-line-ids",
    GAP,
    [(
        "\treturn asset.UnresolvedRequiredItems(items), nil\n",
        "\treturn items, nil\n",
        1,
    )],
    "UnresolvedRequiredItems returns EVERY line, including the satisfied and optional ones",
    "TestUnresolvedRequiredItemsRefusesWhenNothingIsApproved",
)

# ---- extra mock-image clauses ----
mutation(
    "I04-render-not-deterministic",
    IMG,
    [(
        "\thasher := fnv.New64a()\n"
        "\t_, _ = hasher.Write([]byte(prompt))\n"
        "\tseed := hasher.Sum64() + uint64(index)*2654435761\n",
        "\tseed := uint64(time.Now().UnixNano())\n",
        1,
    )],
    "render is no longer deterministic, so a retry cannot reproduce the first attempt",
    "TestTheMockImageProducesDecodableDeterministicBytes",
)

mutation(
    "I05-mock-image-rejects-nil",
    IMG,
    [(
        "\tif m == nil {\n"
        "\t\treturn appjobs.ImageOutcome{}, provider.NewUnsupportedError()\n"
        "\t}\n",
        "",
        1,
    )],
    "a nil adapter panics instead of failing closed",
    "(no test named)",
)

# ---- extra bridge clauses ----
mutation(
    "BR05-source-check-removed",
    BRIDGE,
    [(
        "    if (envelope.source !== MONOFORM_SOURCE) {\n"
        "        return { ok: false, reason: \"wrong-source\" };\n"
        "    }\n",
        "",
        1,
    )],
    "the source marker check is gone",
    "a message whose source is not the studio is refused",
    runner="npm",
)

mutation(
    "BR06-camera-focal-range-removed",
    BRIDGE,
    [(
        "    if (focalLength < 1 || focalLength > 2000) {\n"
        "        return null;\n"
        "    }\n",
        "",
        1,
    )],
    "a camera with an unrenderable focal length is accepted",
    "a shot_updated message requires a real camera",
    runner="npm",
)

mutation(
    "BR07-camera-finite-check-removed",
    BRIDGE,
    [(
        "        if (typeof component !== \"number\" || !Number.isFinite(component)) {\n"
        "            return null;\n"
        "        }\n",
        "",
        1,
    )],
    "a camera component that is NaN or a string is accepted",
    "a shot_updated message requires a real camera",
    runner="npm",
)

mutation(
    "BR08-host-message-posts-to-star",
    BRIDGE,
    [("\treturn selfOrigin;\n", '\treturn "*";\n', 1)],
    "the host posts to '*' rather than the window's own origin",
    "the host's messages carry the envelope, and the target origin is exact",
    runner="npm",
)

SECOND_PASS = [m for m in M if m["name"] not in FIRST_PASS and m["runner"] in ("go", "npm")]

print("second-pass mutations: %d" % len(SECOND_PASS))
for m in SECOND_PASS:
    print("  " + m["name"])
print()

sys.argv = sys.argv[:1] + [m["name"] for m in SECOND_PASS]
mod.main()
