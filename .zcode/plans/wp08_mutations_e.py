# WP-08 mutations, part E: the domain's diff, and the remaining agentruntime increments.
#
# A SECOND, INDEPENDENT PASS over the domain and the runtime. The package the WP-08 code adds to
# `internal/domain/script` is `diff.go`, and its tests are the domain's own — so the mutations here
# ask whether the diff's reported CONTENT is asserted, not only its counts.

PKG_DOMAIN = ["go", "test", "./internal/domain/script/", "-count=1"]
PKG_AGENTRUNTIME = ["go", "test", "./internal/application/agentruntime/", "-count=1"]
PKG_FULL_ROOT = ["go", "test", ".", "-count=1"]

MUTATIONS = [
    {
        "name": "E1 the nested line's lock is dropped from the diff",
        "file": "internal/domain/script/diff.go",
        "old": "\t\tlineChanges = appendFieldChange(lineChanges, \"locked\", boolText(before.Locked), boolText(after.Locked))",
        "new": "\t\t_ = boolText(before.Locked)",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E2 the scene's own marked-as-original flag is dropped from the diff",
        "file": "internal/domain/script/diff.go",
        "old": "\tchanges = appendFieldChange(changes, \"isOriginalAdaptation\",\n\t\tboolText(from.IsOriginalAdaptation), boolText(to.IsOriginalAdaptation))",
        "new": "\t_ = boolText(from.IsOriginalAdaptation)",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E3 the scene's cited source event is dropped from the diff",
        "file": "internal/domain/script/diff.go",
        "old": "\tchanges = appendFieldChange(changes, \"sourceStoryEventId\", from.SourceStoryEventID, to.SourceStoryEventID)",
        "new": "\t_ = to.SourceStoryEventID",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E4 a removed line is reported with an empty before-text",
        "file": "internal/domain/script/diff.go",
        "old": "\t\tchanges = append(changes, FieldChange{\n\t\t\tField: \"line.\" + itoa(ordinal), Before: line.Text, After: \"\",\n\t\t})",
        "new": "\t\tchanges = append(changes, FieldChange{\n\t\t\tField: \"line.\" + itoa(ordinal), Before: \"\", After: line.Text,\n\t\t})",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E5 Changed ignores a modification that carries no field list",
        "file": "internal/domain/script/diff.go",
        "old": "\treturn d.Added > 0 || d.Removed > 0 || d.Modified > 0 || len(d.Fields) > 0",
        "new": "\treturn d.Added > 0 || d.Removed > 0 || len(d.Fields) > 0",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E6 the script diff is not marked with its family",
        "file": "internal/domain/script/diff.go",
        "old": "\tdiff := VersionDiff{Family: FamilyScript, FromID: from.ScriptVersionID, ToID: to.ScriptVersionID}",
        "new": "\tdiff := VersionDiff{Family: FamilyStorySkeleton, FromID: from.ScriptVersionID, ToID: to.ScriptVersionID}",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E7 an added scene's ordinal is not reported",
        "file": "internal/domain/script/diff.go",
        "old": "\t\t\tdiff.Items = append(diff.Items, ItemChange{\n\t\t\t\tKind: ChangeAdded, Ordinal: after.Ordinal, ID: after.ID,\n\t\t\t\tLocked: structureLocked,\n\t\t\t})",
        "new": "\t\t\tdiff.Items = append(diff.Items, ItemChange{\n\t\t\t\tKind: ChangeAdded, Ordinal: 0, ID: after.ID,\n\t\t\t\tLocked: structureLocked,\n\t\t\t})",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E8 a locked line's own flag is not carried into the diff item",
        "file": "internal/domain/script/diff.go",
        "old": "\t\tchange := ItemChange{Ordinal: after.Ordinal, ID: after.ID, Locked: structureLocked}",
        "new": "\t\tchange := ItemChange{Ordinal: after.Ordinal, ID: after.ID, Locked: false || structureLocked && false}",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E9 the script draft's scene count counts something else",
        "file": "internal/domain/script/draft.go",
        "old": "func (d ScriptStructureDraft) SceneCount() int { return len(d.Scenes) }",
        "new": "func (d ScriptStructureDraft) SceneCount() int { return cap(d.Scenes) }",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E10 itoa in the domain drops all but the last digit",
        "file": "internal/domain/script/diff.go",
        "old": "\t\tdigits = string(rune('0'+value%10)) + digits\n\t\tvalue /= 10",
        "new": "\t\tdigits = string(rune('0'+value%10))\n\t\tvalue /= 10",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E11 LockableFields drops the skeleton's ending hook",
        "file": "internal/domain/script/structure.go",
        "old": "\t\t\tLockSkeletonOpeningHook, LockSkeletonCoreConflict, LockSkeletonTurningPoints,\n\t\t\tLockSkeletonClimax, LockSkeletonEndingHook, LockSkeletonSelectedEvents,",
        "new": "\t\t\tLockSkeletonOpeningHook, LockSkeletonCoreConflict, LockSkeletonTurningPoints,\n\t\t\tLockSkeletonClimax, LockSkeletonSelectedEvents,",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E12 a strategy's risks stop being lockable",
        "file": "internal/domain/script/structure.go",
        "old": "\t\t\tLockStrategySummary, LockStrategyMode, LockStrategyMergedEvents,\n\t\t\tLockStrategyOriginalAdds, LockStrategyRationale, LockStrategyRisks,",
        "new": "\t\t\tLockStrategySummary, LockStrategyMode, LockStrategyMergedEvents,\n\t\t\tLockStrategyOriginalAdds, LockStrategyRationale,",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E13 a structure with no scenes validates",
        "file": "internal/domain/script/structure.go",
        "old": "\tif len(s.Scenes) == 0 {\n\t\treturn InvalidError(\"A script version must contain at least one scene.\")\n\t}",
        "new": "\tif false {\n\t\treturn InvalidError(\"A script version must contain at least one scene.\")\n\t}",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E14 the ordinals are checked against the previous scene rather than the position",
        "file": "internal/domain/script/structure.go",
        "old": "\t\tif scene.Ordinal != index+1 {",
        "new": "\t\t_ = index\n\t\tif scene.Ordinal < 1 || scene.Ordinal > len(s.Scenes) {",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E15 a line nested under another scene validates",
        "file": "internal/domain/script/structure.go",
        "old": "\t\tif line.SceneID != scene.ID {\n\t\t\treturn InvalidError(\"A dialogue line must name the scene it is nested under.\")\n\t\t}",
        "new": "\t\tif false {\n\t\t\treturn InvalidError(\"A dialogue line must name the scene it is nested under.\")\n\t\t}",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E16 a shot's ordinal is not checked against its position",
        "file": "internal/domain/script/structure.go",
        "old": "\t\tif shot.Ordinal != index+1 {",
        "new": "\t\t_ = index\n\t\tif shot.Ordinal < 1 || shot.Ordinal > len(scene.Shots) {",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E17 a field lock on a foreign family validates",
        "file": "internal/domain/script/structure.go",
        "old": "\tif !IsLockableField(l.Family, l.Field) {\n\t\treturn InvalidError(\"That field cannot be locked on this version family.\")\n\t}",
        "new": "\tif false {\n\t\treturn InvalidError(\"That field cannot be locked on this version family.\")\n\t}",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E18 every family admits no lockable field",
        "file": "internal/domain/script/structure.go",
        "old": "\tcase FamilyScript:\n\t\treturn []LockableField{LockScriptSummary, LockScriptStructure}",
        "new": "\tcase FamilyScript:\n\t\treturn []LockableField{LockScriptSummary}",
        "cmd": PKG_DOMAIN,
    },
    {
        "name": "E19 a script version with an unknown status validates",
        "file": "internal/domain/script/script.go",
        "old": "\tif !versioning.IsValidStatus(v.Status) {\n\t\treturn InvalidError(\"The script version status is not recognised.\")\n\t}",
        "new": "\tif false {\n\t\treturn InvalidError(\"The script version status is not recognised.\")\n\t}",
        "cmd": PKG_DOMAIN,
    },
]
