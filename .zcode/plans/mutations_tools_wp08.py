# Mutation specification for WP-08's two new tools.
#
# The tools are where a model's arguments become a domain value, so the mutations here are the
# mistakes that would let a model's statement through unexamined: a missing scope check, a default
# that stops being applied, a page boundary that is not enforced.

TEST_CMD = ["go", "test", "./internal/application/agenttools/", "-count=1"]

MUTATIONS = [
    {
        "name": "the structure write skips the project scope check",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\t// A caller that ALSO named an episode must be naming this one, otherwise the argument is a\n\t\t// second, unchecked statement of where the write goes.",
        "new": "\t\tif err := error(nil); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\t// A caller that ALSO named an episode must be naming this one, otherwise the argument is a\n\t\t// second, unchecked statement of where the write goes.",
    },
    {
        "name": "a mismatched episode argument is read past",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif stated := strings.TrimSpace(arguments.EpisodeID); stated != \"\" && stated != script.EpisodeID {",
        "new": "\t\tif stated := strings.TrimSpace(arguments.EpisodeID); false && stated != \"\" && stated != script.EpisodeID {",
    },
    {
        "name": "the empty interior marking reaches the database",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif interior == \"\" {\n\t\t\tmarking = scriptdomain.InteriorOTHER\n\t\t} else if !scriptdomain.IsValidInteriorExterior(marking) {",
        "new": "\t\tif interior == \"\" {\n\t\t\tmarking = scriptdomain.InteriorExterior(interior)\n\t\t} else if !scriptdomain.IsValidInteriorExterior(marking) {",
    },
    {
        "name": "the empty line type reaches the database",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tif stated == \"\" {\n\t\t\t\tlineType = scriptdomain.LineDialogue\n\t\t\t} else if !scriptdomain.IsValidLineType(lineType) {",
        "new": "\t\t\tif stated == \"\" {\n\t\t\t\tlineType = scriptdomain.LineType(stated)\n\t\t\t} else if !scriptdomain.IsValidLineType(lineType) {",
    },
    {
        "name": "the scene ceiling is not enforced",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif len(scenes) > scriptdomain.MaxScenesPerVersion {",
        "new": "\tif false && len(scenes) > scriptdomain.MaxScenesPerVersion {",
    },
    {
        "name": "a negative scene duration is accepted",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif entry.EstimatedDurationSeconds < 0 {",
        "new": "\t\tif false && entry.EstimatedDurationSeconds < 0 {",
    },
    {
        "name": "an empty scene list is accepted",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif len(scenes) == 0 {\n\t\treturn scriptdomain.ScriptStructureDraft{}, agent.InvalidError(\"The script version needs at least one scene.\")\n\t}",
        "new": "\tif false {\n\t\treturn scriptdomain.ScriptStructureDraft{}, agent.InvalidError(\"The script version needs at least one scene.\")\n\t}",
    },
    {
        "name": "the structure read skips the project scope check",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tstructure, err := deps.Script.GetScriptStructure(ctx, version.ID)",
        "new": "\t\tif err := error(nil); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tstructure, err := deps.Script.GetScriptStructure(ctx, version.ID)",
    },
    {
        "name": "an inverted scene range returns an empty page",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tif from > to {",
        "new": "\t\tif false && from > to {",
    },
    {
        "name": "the page reports the page's own duration rather than the version's",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\t\"estimatedDurationSeconds\": structure.TotalDurationSeconds(),",
        "new": "\t\t\t\"estimatedDurationSeconds\": structure.Scenes[0].EstimatedDurationSeconds,",
    },
    {
        "name": "the page is not clamped when it runs past the end",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\tif to <= 0 || to > total {\n\t\tto = total\n\t}",
        "new": "\tif to <= 0 {\n\t\tto = total\n\t}",
    },
    {
        "name": "the strategy's treatment vocabulary is not checked",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tif !scriptdomain.IsValidEventTreatment(treatment) {",
        "new": "\t\t\tif false && !scriptdomain.IsValidEventTreatment(treatment) {",
    },
    {
        "name": "the skeleton's selection is dropped on the way to the service",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tSelectedEventIDs: arguments.SelectedEventIDs,",
        "new": "\t\t\tSelectedEventIDs: nil,",
    },
    {
        "name": "the strategy's treatments are dropped on the way to the service",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\t\tEventLinks:            links,",
        "new": "\t\t\tEventLinks:            nil,",
    },
    {
        "name": "the derived duration is not reported",
        "file": "internal/application/agenttools/handlers.go",
        "old": "\t\tresult[\"estimatedDurationSeconds\"] = updated.EstimatedDurationSeconds",
        "new": "\t\tresult[\"estimatedDurationSeconds\"] = 0",
    },
]
