# Mutation specification for WP-08's enforcement, link and draft code.
#
# Every mutation below is a plausible DEFECT rather than a random edit: each one is a mistake a
# contributor could make while believing the code still enforces what it says. A mutation that
# survives means the test suite does not distinguish the defect from the correct code.
#
# PASS 2.
# rather than the code's: `if false {` leaves the block's variables unused, a bare `return` in a
# switch arm is a syntax error, and one replacement was truncated. This file writes each mutation so
# the result still compiles, which is what makes a kill a fact about the tests.

TEST_CMD = ["go", "test", "./internal/application/script/", "./internal/infrastructure/database/", "-count=1"]

MUTATIONS = [
    # --- The lock enforcement itself: AC-SCRIPT-002's core ---
    {
        "name": "locks are read from no version at all",
        "file": "internal/application/script/enforcement.go",
        "old": "\treturn s.repository.ListScriptFieldLocks(ctx, base)",
        "new": "\treturn []scriptdomain.FieldLock{}, nil",
    },
    {
        "name": "compareFieldSets skips a field whose value is missing",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !oldPresent || !newPresent {",
        "new": "\t\tif !oldPresent && !newPresent {",
    },
    {
        "name": "compareFieldSets compares a field with itself",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !scriptdomain.LocksEqual(lock.Field, oldValue, newValue) {",
        "new": "\t\t_ = newValue\n\t\tif !scriptdomain.LocksEqual(lock.Field, oldValue, oldValue) {",
    },
    {
        "name": "assertLocksCovered accepts a field no comparison covers",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !claimed[lock.Field] {",
        "new": "\t\tif false && !claimed[lock.Field] {",
    },
    {
        "name": "assertLocksCovered accepts a lock of another family",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif lock.Family != family {",
        "new": "\t\tif false && lock.Family != family {",
    },
    {
        "name": "assertWritable never refuses a frozen version",
        "file": "internal/application/script/enforcement.go",
        "old": "\tif versioning.IsContentFrozen(status) {",
        "new": "\tif false && versioning.IsContentFrozen(status) {",
    },
    {
        "name": "joinedEvents keeps the events in the caller's order",
        "file": "internal/application/script/enforcement.go",
        "old": "\tsort.Strings(sorted)\n\treturn strings.Join(sorted, \"\\n\")",
        "new": "\t_ = sort.SearchStrings(sorted, \"\")\n\treturn strings.Join(ids, \"\\n\")",
    },
    {
        "name": "assertLocksCovered derives coverage from the values instead of the list",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !claimed[lock.Field] {",
        "new": "\t\tif len(claimed) < 0 && !claimed[lock.Field] {",
    },

    # --- The skeleton path ---
    {
        "name": "skeleton revision writes with no lock check",
        "file": "internal/application/script/service.go",
        "old": "\tif err := s.assertSkeletonLocks(ctx, skeleton, selected); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
    },
    {
        "name": "skeleton selection is not checked against the story graph",
        "file": "internal/application/script/service.go",
        "old": "\tif err := s.assertStoryEventsExist(ctx, episode.ProjectID, selected); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
    },
    {
        "name": "skeleton link set is not written",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\treturn repo.linkSkeletonEvents(ctx, record.ID, eventIDs, record.CreatedAt)",
        "new": "\t\treturn nil",
    },
    {
        "name": "skeleton selection order is not preserved",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\t\tversionID, eventID, index+1, formatTime(createdAt)); err != nil {",
        "new": "\t\t\tversionID, eventID, index*0, formatTime(createdAt)); err != nil {",
    },
    {
        "name": "a repeat in a selection is silently dropped rather than refused",
        "file": "internal/application/script/service.go",
        "old": "\t\tif seen[trimmed] {\n\t\t\treturn nil, scriptdomain.InvalidError(\"That story event is selected twice: \" + trimmed + \".\")\n\t\t}",
        "new": "\t\tif seen[trimmed] {\n\t\t\tcontinue\n\t\t}",
    },

    # --- The strategy path ---
    {
        "name": "strategy revision writes with no lock check",
        "file": "internal/application/script/service.go",
        "old": "\tif err := s.assertStrategyLocks(ctx, record, links); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
    },
    {
        "name": "strategy treatments are not checked against the story graph",
        "file": "internal/application/script/service.go",
        "old": "\tif err := s.assertStoryEventsExist(ctx, episode.ProjectID, treatedEvents); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
    },
    {
        "name": "strategy treatments are not written",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\treturn repo.linkStrategyEvents(ctx, record.ID, links, record.CreatedAt)",
        "new": "\t\treturn nil",
    },
    {
        "name": "strategy ordinal is the caller's rather than the slice position",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\t\tversionID, link.StoryEventID, string(link.Treatment), position+1,",
        "new": "			versionID, link.StoryEventID, string(link.Treatment), position," ,
    },
    {
        "name": "renderTreatments drops the treatment and keeps only the event",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\trendered = append(rendered, strings.TrimSpace(link.StoryEventID)+\"=\"+string(link.Treatment))",
        "new": "\t\trendered = append(rendered, strings.TrimSpace(link.StoryEventID))",
    },
    {
        "name": "a missing treatment is defaulted rather than refused",
        "file": "internal/application/script/service.go",
        "old": "\t\tif treatment == \"\" {",
        "new": "\t\tif treatment == \"\" && false {",
    },

    # --- The script structure path ---
    {
        "name": "the script summary lock is routed to no comparison",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif lock.Field == scriptdomain.LockScriptSummary {",
        "new": "\t\tif false && lock.Field == scriptdomain.LockScriptSummary {",
    },
    {
        "name": "a locked structure's scene count is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\tif len(base.Scenes) != len(incoming.Scenes) {",
        "new": "\tif false && len(base.Scenes) != len(incoming.Scenes) {",
    },
    {
        "name": "a locked structure's scene duration is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\tbefore.EstimatedDurationSeconds != after.EstimatedDurationSeconds {",
        "new": "\t\t\tfalse {",
    },
    {
        "name": "a locked structure's slugline is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif !scriptdomain.LocksEqual(scriptdomain.LockScriptStructure, before.Slugline, after.Slugline) ||",
        "new": "\t\tif false ||",
    },
    {
        "name": "dialogue locks are not carried forward",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\tlines[lineIndex].Locked = baseScene.DialogueLines[lineIndex].Locked",
        "new": "\t\t\t_ = lineIndex",
    },
    {
        "name": "draft scene ordinals are all one",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tOrdinal:                  sceneIndex + 1,",
        "new": "\t\t\t\tOrdinal:                  1 + sceneIndex*0,",
    },
    {
        "name": "draft lines are attached to no scene",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tSceneID:            sceneID,\n\t\t\t\tOrdinal:            lineIndex + 1,",
        "new": "\t\t\t\tSceneID:            \"\",\n\t\t\t\tOrdinal:            lineIndex + 1,",
    },
    {
        "name": "a draft's shot is written as approved",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tStatus:    versioning.StatusDraft,",
        "new": "\t\t\t\tStatus:    versioning.StatusApproved,",
    },
    {
        "name": "structure references are not checked",
        "file": "internal/application/script/structure.go",
        "old": "\tif err := s.assertStructureReferences(ctx, request.ProjectID, structure); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
    },
    {
        "name": "a missing entity is not reported",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif len(missing) > 0 {\n\t\t\treturn scriptdomain.InvalidError(\n\t\t\t\t\"The script cites story entities this project does not have: \"",
        "new": "\t\tif false && len(missing) > 0 {\n\t\t\treturn scriptdomain.InvalidError(\n\t\t\t\t\"The script cites story entities this project does not have: \"",
    },
    {
        "name": "a missing event is not reported",
        "file": "internal/application/script/service.go",
        "old": "\tif len(missing) > 0 {\n\t\treturn scriptdomain.InvalidError(\n\t\t\t\"These story events do not exist in this project: \" + strings.Join(missing, \", \") + \".\")",
        "new": "\tif false && len(missing) > 0 {\n\t\treturn scriptdomain.InvalidError(\n\t\t\t\"These story events do not exist in this project: \" + strings.Join(missing, \", \") + \".\")",
    },
    {
        "name": "both a structure and a draft are accepted",
        "file": "internal/application/script/structure.go",
        "old": "\tcase hasStructure && hasDraft:",
        "new": "\tcase hasStructure && hasDraft && false:",
    },
    {
        "name": "a structure with no scenes is accepted",
        "file": "internal/domain/script/structure.go",
        "old": "	if len(s.Scenes) == 0 {",
        "new": "	if false && len(s.Scenes) == 0 {",
    },
    {
        "name": "an explicit project is ignored so the reference check never runs",
        "file": "internal/application/script/structure.go",
        "old": "\tproject := strings.TrimSpace(projectID)\n\tif project == \"\" {\n\t\treturn nil\n\t}",
        "new": "\tproject := strings.TrimSpace(projectID)\n\tif true || project == \"\" {\n\t\treturn nil\n\t}",
    },
    {
        "name": "the project id is compared but the event ids are not trimmed",
        "file": "internal/application/script/enforcement.go",
        "old": "\tbase := strings.TrimSpace(versionID)\n\tif base == \"\" {",
        "new": "\tbase := versionID\n\tif base == \"\" {",
    },

    # --- The reference query ---
    {
        "name": "a soft-deleted event counts as present",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\tAND deleted_at = '' AND id IN (%s)`, projectID, eventIDs)\n}",
        "new": "\t\tAND id IN (%s)`, projectID, eventIDs)\n}",
    },
    {
        "name": "the reference query ignores the project boundary",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\tAND deleted_at = '' AND id IN (%s)`, projectID, eventIDs)\n}\n\n// MissingStoryEntityIDs",
        "new": "\t\tAND deleted_at = '' AND id IN (%s)`, \"drama-project\", eventIDs)\n}\n\n// MissingStoryEntityIDs",
    },
    {
        "name": "the missing list is sorted rather than returned in the caller's order",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\tmissing := make([]string, 0, len(wanted))\n\tfor _, id := range wanted {\n\t\tif !found[id] {\n\t\t\tmissing = append(missing, id)\n\t\t}\n\t}\n\treturn missing, nil",
        "new": "\tmissing := make([]string, 0, len(wanted))\n\tfor _, id := range wanted {\n\t\tif !found[id] {\n\t\t\tmissing = append(missing, id)\n\t\t}\n\t}\n\tfor outer := 0; outer < len(missing); outer++ {\n\t\tfor inner := outer + 1; inner < len(missing); inner++ {\n\t\t\tif missing[inner] < missing[outer] {\n\t\t\t\tmissing[outer], missing[inner] = missing[inner], missing[outer]\n\t\t\t}\n\t\t}\n\t}\n\treturn missing, nil",
    },
    {
        "name": "the caller's duplicates are reported twice rather than collapsed",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\tif trimmed == \"\" || seen[trimmed] {\n\t\t\tcontinue\n\t\t}\n\t\tseen[trimmed] = true",
        "new": "\t\tif trimmed == \"\" {\n\t\t\tcontinue\n\t\t}\n\t\tseen[trimmed] = true",
    },
]
