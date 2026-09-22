# Independent mutation specification for WP-08 (the drama Script pipeline).
#
# Each mutation is a PLAUSIBLE DEFECT: a mistake a contributor could make while believing the code
# still does what it says. Every one is written so the result still COMPILES — a mutant that does not
# compile proves nothing and the harness reports it as invalid rather than counting it as a kill.
#
# The "cmd" per mutation is the targeted test command; the harness runs it and treats a nonzero exit
# as a kill.

PKG_SCRIPT = ["go", "test", "./internal/application/script/", "-count=1"]
PKG_TOOLS = ["go", "test", "./internal/application/agenttools/", "-count=1"]
PKG_MOCK = ["go", "test", "./internal/infrastructure/providers/", "-count=1"]
PKG_DB = ["go", "test", "./internal/infrastructure/database/", "-count=1"]
PKG_DESKTOP = ["go", "test", "./internal/desktop/", "-count=1"]
PKG_ROOT = ["go", "test", ".", "-count=1"]
PKG_DOMAIN = ["go", "test", "./internal/domain/script/", "-count=1"]
PKG_PIPELINE = ["go", "test", "./internal/application/scriptpipeline/", "-count=1"]

MUTATIONS = []

# ---------------------------------------------------------------------------
# A1. application/script/enforcement.go — the lock comparison
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "A1 readLocks reads the locks of no version",
        "file": "internal/application/script/enforcement.go",
        "old": "\treturn s.repository.ListScriptFieldLocks(ctx, base)",
        "new": "\t_ = base\n\treturn nil, nil",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A2 compareFieldSets refuses a lock of another family by skipping it",
        "file": "internal/application/script/enforcement.go",
        "old": """		if lock.Family != family {
			// A lock of another family cannot belong to this version, so this is a corrupt row rather
			// than a user's intent. Fail closed rather than skip: a lock that reads as a protection
			// and enforces nothing is worse than no lock, which is the rule the lock vocabulary
			// itself states.
			return scriptdomain.ConflictError(
				"A lock on this version names a different artifact family, so this write cannot be checked against it.")
		}""",
        "new": """		if lock.Family != family {
			continue
		}""",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A3 compareFieldSets skips a field one side does not carry",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !oldPresent || !newPresent {",
        "new": "\t\tif !oldPresent && !newPresent {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A4 compareFieldSets compares the base with itself",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !scriptdomain.LocksEqual(lock.Field, oldValue, newValue) {",
        "new": "\t\t_ = newValue\n\t\tif !scriptdomain.LocksEqual(lock.Field, oldValue, oldValue) {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A5 assertLocksCovered never refuses a lock no comparison claims",
        "file": "internal/application/script/enforcement.go",
        "old": "\t\tif !claimed[lock.Field] {",
        "new": "\t\t_ = claimed\n\t\tif false {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A6 assertLocksCovered accepts a lock of another family",
        "file": "internal/application/script/enforcement.go",
        "old": """		if lock.Family != family {
			return scriptdomain.ConflictError(
				"A lock on this version names a different artifact family, so this write cannot be checked against it.")
		}
		if !claimed[lock.Field] {
			return scriptdomain.ConflictError(
				"A field locked on this version is not one this write can compare, so the revision cannot be verified.")
		}
	}
	return nil
}""",
        "new": """		if !claimed[lock.Field] {
			return scriptdomain.ConflictError(
				"A field locked on this version is not one this write can compare, so the revision cannot be verified.")
		}
	}
	return nil
}""",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A7 joinedEvents keeps the caller's order instead of sorting",
        "file": "internal/application/script/enforcement.go",
        "old": """	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return strings.Join(sorted, "\\n")""",
        "new": """	sorted := append([]string(nil), ids...)
	_ = sort.SearchStrings(sorted, "")
	return strings.Join(ids, "\\n")""",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A8 renderTreatments drops the treatment and keeps only the event",
        "file": "internal/application/script/enforcement.go",
        "old": """		rendered = append(rendered, strings.TrimSpace(link.StoryEventID)+"="+string(link.Treatment))""",
        "new": """		rendered = append(rendered, strings.TrimSpace(link.StoryEventID))""",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A9 assertWritable never refuses a frozen version",
        "file": "internal/application/script/enforcement.go",
        "old": "\tif versioning.IsContentFrozen(status) {",
        "new": "\tif false {\n\t\t_ = versioning.StatusDraft",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A10 readLocks does not trim the version id",
        "file": "internal/application/script/enforcement.go",
        "old": "\tbase := strings.TrimSpace(versionID)\n\tif base == \"\" {",
        "new": "\tbase := versionID\n\tif base == \"\" {",
        "cmd": PKG_SCRIPT,
    },
]

# ---------------------------------------------------------------------------
# A2. application/script/structure.go
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "A11 enforceScriptLocks reads the locks from the NEW version",
        "file": "internal/application/script/structure.go",
        "old": "\tlocks, err := s.readLocks(ctx, version.BasedOnVersionID)\n\tif err != nil {\n\t\treturn err\n\t}\n\tif len(locks) == 0 {\n\t\treturn nil\n\t}",
        "new": "\tlocks, err := s.readLocks(ctx, version.ID)\n\tif err != nil {\n\t\treturn err\n\t}\n\tif len(locks) == 0 {\n\t\treturn nil\n\t}",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A12 the summary lock is routed to no comparison",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif lock.Field == scriptdomain.LockScriptSummary {",
        "new": "\t\t_ = lock\n\t\tif false {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A13 a locked structure's scene count is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\tif len(base.Scenes) != len(incoming.Scenes) {",
        "new": "\tif false {\n\t\t_ = incoming.Scenes",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A14 a locked structure's slugline is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif !scriptdomain.LocksEqual(scriptdomain.LockScriptStructure, before.Slugline, after.Slugline) ||",
        "new": "\t\t_ = after.Slugline\n\t\tif false ||",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A15 a locked structure's scene summary is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t!scriptdomain.LocksEqual(scriptdomain.LockScriptStructure, before.Summary, after.Summary) ||",
        "new": "\t\t\tfalse ||",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A16 a locked structure's scene duration is not compared",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\tbefore.EstimatedDurationSeconds != after.EstimatedDurationSeconds {",
        "new": "\t\t\tfalse {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A17 dialogue locks are not carried forward",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\tlines[lineIndex].Locked = baseScene.DialogueLines[lineIndex].Locked",
        "new": "\t\t\tlines[lineIndex].Locked = lineIndex == lineIndex && false",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A18 carryDialogueLocks matches by scene id rather than by position",
        "file": "internal/application/script/structure.go",
        "old": "\t\tbaseScene := baseStructure.Scenes[sceneIndex]\n\t\tlines := structure.Scenes[sceneIndex].DialogueLines",
        "new": "\t\tbaseScene := baseStructure.Scenes[len(baseStructure.Scenes)-1]\n\t\tlines := structure.Scenes[sceneIndex].DialogueLines",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A19 a draft's scene ordinals are all one",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tOrdinal:                  sceneIndex + 1,",
        "new": "\t\t\t\tOrdinal:                  1 + sceneIndex*0,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A20 a draft's lines are attached to no scene",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tSceneID:            sceneID,\n\t\t\t\tOrdinal:            lineIndex + 1,",
        "new": "\t\t\t\tSceneID:            \"\",\n\t\t\t\tOrdinal:            lineIndex + 1,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A21 a draft's lines are numbered from zero",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tSceneID:            sceneID,\n\t\t\t\tOrdinal:            lineIndex + 1,",
        "new": "\t\t\t\tSceneID:            sceneID,\n\t\t\t\tOrdinal:            lineIndex,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A22 a draft's shots are all one, in the written structure",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tOrdinal:                  shotIndex + 1,",
        "new": "\t\t\t\tOrdinal:                  1 + shotIndex*0,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A23 a draft's shot is written as approved",
        "file": "internal/application/script/structure.go",
        "old": "\t\t\t\tStatus:    versioning.StatusDraft,\n\t\t\t\tCreatedAt: now,\n\t\t\t\tUpdatedAt: now,\n\t\t\t\tRevision:  1,",
        "new": "\t\t\t\tStatus:    versioning.StatusApproved,\n\t\t\t\tCreatedAt: now,\n\t\t\t\tUpdatedAt: now,\n\t\t\t\tRevision:  1,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A24 a draft's missing interior marking is left empty",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif interior == \"\" {\n\t\t\tinterior = scriptdomain.InteriorOTHER\n\t\t}",
        "new": "\t\tif interior == \"\" {\n\t\t\tinterior = scriptdomain.InteriorExterior(\"\")\n\t\t}",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A25 a revision never inherits its base's summary",
        "file": "internal/application/script/structure.go",
        "old": "\tinherited, err := s.repository.GetScriptVersion(ctx, base)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\treturn inherited.Summary, nil",
        "new": "\tif _, err := s.repository.GetScriptVersion(ctx, base); err != nil {\n\t\treturn \"\", err\n\t}\n\treturn \"\", nil",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A26 a structure's references are not checked at all",
        "file": "internal/application/script/structure.go",
        "old": "\tif err := s.assertStructureReferences(ctx, request.ProjectID, structure); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A27 a missing cited entity is not reported",
        "file": "internal/application/script/structure.go",
        "old": "\t\tif len(missing) > 0 {\n\t\t\treturn scriptdomain.InvalidError(\n\t\t\t\t\"The script cites story entities this project does not have: \" + strings.Join(missing, \", \") +",
        "new": "\t\tif false {\n\t\t\treturn scriptdomain.InvalidError(\n\t\t\t\t\"The script cites story entities this project does not have: \" + strings.Join(missing, \", \") +",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A28 the script summary lock is not one the write path claims",
        "file": "internal/application/script/structure.go",
        "old": "\t\tscriptdomain.LockScriptSummary,\n\t\tscriptdomain.LockScriptStructure,\n\t})",
        "new": "\t\tscriptdomain.LockScriptStructure,\n\t})",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A29 the structure lock is compared without being claimed",
        "file": "internal/application/script/structure.go",
        "old": "\tif hasLock(locks, scriptdomain.LockScriptStructure) {\n\t\tif err := compareStructures(baseStructure, incoming); err != nil {",
        "new": "\tif false {\n\t\tif err := compareStructures(baseStructure, incoming); err != nil {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A30 a diff reads the structure lock from the wrong side",
        "file": "internal/application/script/structure.go",
        "old": "\t\tstructureLocked, err := s.hasStructureLock(ctx, fromID)",
        "new": "\t\tstructureLocked, err := s.hasStructureLock(ctx, toID)",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A31 a projection labels the node with the scene number only",
        "file": "internal/application/script/structure.go",
        "old": "\t\tlabel := strings.TrimSpace(scene.Slugline)\n\t\tif label == \"\" {\n\t\t\tlabel = strings.TrimSpace(scene.SceneNumber)\n\t\t}",
        "new": "\t\tlabel := strings.TrimSpace(scene.SceneNumber)\n\t\tif label == \"\" {\n\t\t\tlabel = strings.TrimSpace(scene.Slugline)\n\t\t}",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A32 the summary comparison is fed the base row's change reason",
        "file": "internal/application/script/structure.go",
        "old": "\t\tfieldValues{scriptdomain.LockScriptSummary: base.Summary},",
        "new": "\t\tfieldValues{scriptdomain.LockScriptSummary: base.ChangeReason},",
        "cmd": PKG_SCRIPT,
    },
]

# ---------------------------------------------------------------------------
# A3. application/script/service.go
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "A33 the skeleton write enforces no locks",
        "file": "internal/application/script/service.go",
        "old": "\tif err := s.assertSkeletonLocks(ctx, skeleton, selected); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A34 the strategy write enforces no locks",
        "file": "internal/application/script/service.go",
        "old": "\tif err := s.assertStrategyLocks(ctx, record, links); err != nil {",
        "new": "\tif err := error(nil); err != nil {",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A35 a skeleton selection naming a missing event is not reported",
        "file": "internal/application/script/service.go",
        "old": "\tif len(missing) > 0 {\n\t\treturn scriptdomain.InvalidError(\n\t\t\t\"These story events do not exist in this project: \" + strings.Join(missing, \", \") + \".\")",
        "new": "\tif false {\n\t\treturn scriptdomain.InvalidError(\n\t\t\t\"These story events do not exist in this project: \" + strings.Join(missing, \", \") + \".\")",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A36 a repeated event in a selection is dropped rather than refused",
        "file": "internal/application/script/service.go",
        "old": "\t\tif seen[trimmed] {\n\t\t\treturn nil, scriptdomain.InvalidError(\"That story event is selected twice: \" + trimmed + \".\")\n\t\t}",
        "new": "\t\tif seen[trimmed] {\n\t\t\tcontinue\n\t\t}",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A37 a strategy link is stamped with the caller's version id",
        "file": "internal/application/script/service.go",
        "old": "\t\trecord := scriptdomain.StrategyEventLink{\n\t\t\tStrategyVersionID: versionID,",
        "new": "\t\trecord := scriptdomain.StrategyEventLink{\n\t\t\tStrategyVersionID: strings.TrimSpace(link.StrategyVersionID),",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A38 a strategy link's ordinal is zero-based",
        "file": "internal/application/script/service.go",
        "old": "\t\t\t// The ordinal is the POSITION, so a caller cannot state one that disagrees with the\n\t\t\t// order it wrote — which is the only thing \"reordered\" can mean.\n\t\t\tOrdinal: index + 1,",
        "new": "\t\t\t// The ordinal is the POSITION, so a caller cannot state one that disagrees with the\n\t\t\t// order it wrote — which is the only thing \"reordered\" can mean.\n\t\t\tOrdinal: index,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A39 renderTreatments is fed the base links in the revision's comparison",
        "file": "internal/application/script/service.go",
        "old": "\tbaseLinks, err := s.repository.ListStrategyEventLinks(ctx, base.ID)\n\tif err != nil {\n\t\treturn err\n\t}\n\treturn compareFieldSets(locks, scriptdomain.FamilyAdaptationStrategy,\n\t\tfieldValues{\n\t\t\tscriptdomain.LockStrategySummary:      base.StrategySummary,\n\t\t\tscriptdomain.LockStrategyMode:         string(base.AdaptationMode),\n\t\t\tscriptdomain.LockStrategyMergedEvents: renderTreatments(baseLinks),",
        "new": "\tbaseLinks, err := s.repository.ListStrategyEventLinks(ctx, base.ID)\n\tif err != nil {\n\t\treturn err\n\t}\n\t_ = baseLinks\n\treturn compareFieldSets(locks, scriptdomain.FamilyAdaptationStrategy,\n\t\tfieldValues{\n\t\t\tscriptdomain.LockStrategySummary:      base.StrategySummary,\n\t\t\tscriptdomain.LockStrategyMode:         string(base.AdaptationMode),\n\t\t\tscriptdomain.LockStrategyMergedEvents: base.MergedEventGroupsJSON,",
        "cmd": PKG_SCRIPT,
    },
    {
        "name": "A40 the skeleton comparison reads the incoming selection from the base",
        "file": "internal/application/script/service.go",
        "old": "\t\t\tscriptdomain.LockSkeletonSelectedEvents: joinedEvents(selectedEventIDs),",
        "new": "\t\t\tscriptdomain.LockSkeletonSelectedEvents: joinedEvents(baseSelection),",
        "cmd": PKG_SCRIPT,
    },
]

# ---------------------------------------------------------------------------
# A4. application/scriptpipeline/
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "A41 the pipeline drives no stage at all",
        "file": "internal/application/scriptpipeline/pipeline.go",
        "old": "\tagents, ok := stageAgents[stage]\n\treturn agents, ok",
        "new": "\tagents, ok := stageAgents[stage]\n\treturn agents, ok && false",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A42 the generation stage's supervisor is the skeleton's",
        "file": "internal/application/scriptpipeline/pipeline.go",
        "old": "\t\tSupervision:  \"script.supervision.script\",",
        "new": "\t\tSupervision:  \"script.supervision.story_skeleton\",",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A43 the generation stage names the skeleton artifact type",
        "file": "internal/application/scriptpipeline/pipeline.go",
        "old": "\t\tArtifactType: \"script_version\",",
        "new": "\t\tArtifactType: \"story_skeleton_version\",",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A44 the generation stage claims the skeleton family",
        "file": "internal/application/scriptpipeline/pipeline.go",
        "old": "\t\tArtifactType: \"script_version\",\n\t\tFamily:       script.FamilyScript,",
        "new": "\t\tArtifactType: \"script_version\",\n\t\tFamily:       script.FamilyStorySkeleton,",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A45 the pipeline answers for a stage it does not drive",
        "file": "internal/application/scriptpipeline/pipeline.go",
        "old": "\tagents, ok := stageAgents[stage]\n\treturn agents, ok",
        "new": "\tif stage == \"\" {\n\t\treturn StageAgents{}, false\n\t}\n\treturn stageAgents[stage], true",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A46 the state layer renders an unstated field blank",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\tif version := trimOrEmpty(request.ScriptVersionID); version != \"\" {\n\t\tfields = append(fields, fieldScript+version)\n\t}",
        "new": "\tfields = append(fields, fieldScript+trimOrEmpty(request.ScriptVersionID))",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A47 the state layer drops the selected events",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\tif len(request.SelectedEventIDs) > 0 {\n\t\tfields = append(fields, fieldSelected+strings.Join(request.SelectedEventIDs, \",\"))\n\t}",
        "new": "\tif false {\n\t\tfields = append(fields, fieldSelected+strings.Join(request.SelectedEventIDs, \",\"))\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A48 the state layer renders the projection's identifier as the version",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\tfieldScript     = \"script_version=\"",
        "new": "\tfieldScript     = \"script_versions=\"",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A49 itoa renders every number as its last digit",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\t\tdigits = string(rune('0'+value%10)) + digits\n\t\tvalue /= 10",
        "new": "\t\tdigits = string(rune('0'+value%10))\n\t\tvalue /= 10",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A50 a malformed finding list is treated as no findings",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\tif err := json.Unmarshal([]byte(trimmed), &ids); err != nil {\n\t\treturn nil, agent.InvalidError(\"That decision's findings could not be read, so the revision cannot be run against them.\")\n\t}",
        "new": "\tif err := json.Unmarshal([]byte(trimmed), &ids); err != nil {\n\t\treturn nil, nil\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A51 blank finding identifiers are kept",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\tfor _, id := range ids {\n\t\tif trimmedID := strings.TrimSpace(id); trimmedID != \"\" {\n\t\t\tout = append(out, trimmedID)\n\t\t}\n\t}",
        "new": "\tfor _, id := range ids {\n\t\tout = append(out, id)\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A52 an unreadable pin list is treated as no pins",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\tif err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {\n\t\treturn nil, agent.InvalidError(\"That decision's pinned references could not be read.\")\n\t}",
        "new": "\tif err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {\n\t\treturn nil, nil\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A53 a pin with no identifier is passed on rather than dropped",
        "file": "internal/application/scriptpipeline/state.go",
        "old": "\t\tentityID := strings.TrimSpace(ref.EntityID)\n\t\tif entityID == \"\" {\n\t\t\t// A pin with no identifier names nothing, so it is dropped rather than passed on: a\n\t\t\t// reference the prompt states but no tool can resolve would send the model looking for a\n\t\t\t// row that does not exist.\n\t\t\tcontinue\n\t\t}",
        "new": "\t\tentityID := strings.TrimSpace(ref.EntityID)",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A54 a revision with no decision is silently an empty revision",
        "file": "internal/application/scriptpipeline/run.go",
        "old": "\tif !ok {\n\t\t// A revision with no decision is a caller error rather than an empty revision: the whole point\n\t\t// of naming the attempt is that a user decided something about it.\n\t\treturn nil, nil, agent.InvalidError(\"That stage attempt has no user decision to revise against.\")\n\t}",
        "new": "\tif !ok {\n\t\treturn nil, nil, nil\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A55 a lock of another family is skipped rather than refused",
        "file": "internal/application/scriptpipeline/run.go",
        "old": "\t\tif lock.Family != agents.Family {\n\t\t\t// The lock table has no foreign key across three version tables, so this is a corrupt row.\n\t\t\t// Fail closed rather than skip: a pin that reads as a protection and enforces nothing is\n\t\t\t// worse than no pin, which is the rule the lock vocabulary itself states.\n\t\t\treturn nil, agent.SecurityError(\"A lock on that version names a different artifact family.\")\n\t\t}",
        "new": "\t\tif lock.Family != agents.Family {\n\t\t\tcontinue\n\t\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A56 the artifact id is read from the model's answer",
        "file": "internal/application/scriptpipeline/run.go",
        "old": "\tids := make([]string, 0, len(outcome.ToolCalls))\n\tfor _, call := range outcome.ToolCalls {\n\t\tif id := entityIDOf(call.OutputJSON); id != \"\" {\n\t\t\tids = append(ids, id)\n\t\t}\n\t}",
        "new": "\tids := make([]string, 0, len(outcome.ToolCalls))\n\tif id := entityIDOf(string(outcome.Output)); id != \"\" {\n\t\tids = append(ids, id)\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A57 entityIDOf reads the first artifact whatever its id",
        "file": "internal/application/scriptpipeline/run.go",
        "old": "\tfor _, artifact := range decoded.Artifacts {\n\t\tif id := strings.TrimSpace(artifact.EntityID); id != \"\" {\n\t\t\treturn id\n\t\t}\n\t}\n\treturn \"\"",
        "new": "\tif len(decoded.Artifacts) == 0 {\n\t\treturn \"\"\n\t}\n\treturn strings.TrimSpace(decoded.Artifacts[0].EntityID)",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A58 versionOfDecision accepts any tool call's result",
        "file": "internal/application/scriptpipeline/run.go",
        "old": "\t\t\tif call.ToolKey != writeKey {\n\t\t\t\tcontinue\n\t\t\t}",
        "new": "\t\t\t_ = writeKey",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A59 a waiver is treated as an approving decision",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\tcase workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip:",
        "new": "\tcase workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip, workflow.GateWaive:",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A60 a cancel is treated as an approving decision",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\tcase workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip:",
        "new": "\tcase workflow.GateApprove, workflow.GateManualEdit, workflow.GateSkip, workflow.GateCancel:",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A61 the artifact is approved when the decision is a FIX",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\tif isApprovingDecision(request.Decision) {\n\t\tif err := s.approveArtifact(ctx, attempt, request); err != nil {",
        "new": "\tif request.Decision != workflow.GateCancel {\n\t\tif err := s.approveArtifact(ctx, attempt, request); err != nil {",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A62 a manual edit is recorded as an AGENT's work",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\t\tbody.CreatedByType = versioning.CreatedByUser\n\t\tbody.CreatedByID = trimOrEmpty(request.CreatedByID)\n\t\tversion, err := s.script.CreateStorySkeletonVersion(ctx, body)",
        "new": "\t\tbody.CreatedByType = versioning.CreatedByAgent\n\t\tbody.CreatedByID = trimOrEmpty(request.CreatedByID)\n\t\tversion, err := s.script.CreateStorySkeletonVersion(ctx, body)",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A63 the episode boundary is not enforced on a manual edit",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\tif err := s.assertEpisodeInProject(ctx, request.ProjectID, request.EpisodeID); err != nil {\n\t\treturn StageResult{}, err\n\t}",
        "new": "\tif err := error(nil); err != nil {\n\t\treturn StageResult{}, err\n\t}",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A64 a review report that names no ruleset is accepted",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\tif report.RulesetVersion == \"\" {",
        "new": "\tif false {",
        "cmd": PKG_PIPELINE,
    },
    {
        "name": "A65 a finding with an unknown severity is accepted",
        "file": "internal/application/scriptpipeline/supervise.go",
        "old": "\t\tif !workflow.IsValidSeverity(severity) {",
        "new": "\t\tif false {\n\t\t\t_ = severity",
        "cmd": PKG_PIPELINE,
    },
]

# ---------------------------------------------------------------------------
# A5. infrastructure/database/linked_versions.go
# ---------------------------------------------------------------------------

MUTATIONS += [
    {
        "name": "A66 the skeleton selection is written with no link rows",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\treturn repo.linkSkeletonEvents(ctx, record.ID, eventIDs, record.CreatedAt)",
        "new": "\t\treturn nil",
        "cmd": PKG_DB,
    },
    {
        "name": "A67 the skeleton link ordinals are all one",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\t\tversionID, eventID, index+1, formatTime(createdAt)); err != nil {",
        "new": "\t\t\tversionID, eventID, 1+index*0, formatTime(createdAt)); err != nil {",
        "cmd": PKG_DB,
    },
    {
        "name": "A68 the strategy treatments are written with no link rows",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\treturn repo.linkStrategyEvents(ctx, record.ID, links, record.CreatedAt)",
        "new": "\t\treturn nil",
        "cmd": PKG_DB,
    },
    {
        "name": "A69 the strategy link ordinals are all one",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\t\tversionID, link.StoryEventID, string(link.Treatment), position+1,",
        "new": "\t\t\tversionID, link.StoryEventID, string(link.Treatment), 1+position*0,",
        "cmd": PKG_DB,
    },
    {
        "name": "A70 the strategy link ignores the caller's treatment",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\t\tversionID, link.StoryEventID, string(link.Treatment), position+1,",
        "new": "\t\t\tversionID, link.StoryEventID, string(script.TreatmentRetained), position+1,",
        "cmd": PKG_DB,
    },
    {
        "name": "A71 a soft-deleted event counts as present",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\tAND deleted_at = '' AND id IN (%s)`, projectID, eventIDs)",
        "new": "\t\tAND id IN (%s)`, projectID, eventIDs)",
        "cmd": PKG_DB,
    },
    {
        "name": "A72 the event reference query ignores the project boundary",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\tAND deleted_at = '' AND id IN (%s)`, projectID, eventIDs)",
        "new": "\t\tAND deleted_at = '' AND id IN (%s)`, \"drama-project\", eventIDs)",
        "cmd": PKG_DB,
    },
    {
        "name": "A73 the entity reference query ignores the project boundary",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\treturn r.missingStoryIDs(ctx, `SELECT id FROM story_entities WHERE project_id = ?\n\t\tAND deleted_at = '' AND id IN (%s)`, projectID, entityIDs)",
        "new": "\treturn r.missingStoryIDs(ctx, `SELECT id FROM story_entities WHERE project_id = ?\n\t\tAND deleted_at = '' AND id IN (%s)`, \"drama-project\", entityIDs)",
        "cmd": PKG_DB,
    },
    {
        "name": "A74 the caller's duplicates are reported twice",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\t\tif trimmed == \"\" || seen[trimmed] {\n\t\t\tcontinue\n\t\t}\n\t\tseen[trimmed] = true",
        "new": "\t\tif trimmed == \"\" {\n\t\t\tcontinue\n\t\t}\n\t\tseen[trimmed] = true",
        "cmd": PKG_DB,
    },
    {
        "name": "A75 the missing list comes back sorted rather than in the caller's order",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\tmissing := make([]string, 0, len(wanted))\n\tfor _, id := range wanted {\n\t\tif !found[id] {\n\t\t\tmissing = append(missing, id)\n\t\t}\n\t}\n\treturn missing, nil",
        "new": "\tmissing := make([]string, 0, len(wanted))\n\tfor _, id := range wanted {\n\t\tif !found[id] {\n\t\t\tmissing = append(missing, id)\n\t\t}\n\t}\n\tfor outer := 0; outer < len(missing); outer++ {\n\t\tfor inner := outer + 1; inner < len(missing); inner++ {\n\t\t\tif missing[inner] < missing[outer] {\n\t\t\t\tmissing[outer], missing[inner] = missing[inner], missing[outer]\n\t\t\t}\n\t\t}\n\t}\n\treturn missing, nil",
        "cmd": PKG_DB,
    },
]
