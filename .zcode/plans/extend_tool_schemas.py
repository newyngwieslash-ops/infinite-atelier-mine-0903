import io

p = 'scripts/gen-tool-schemas.mjs'
s = io.open(p, encoding='utf-8').read()

# 1. script.read_shots — the tool batch G2 says does not exist.
old_script_read = '''    {
        key: "script.create_script_structure",'''
new_script_read = '''    {
        key: "script.read_shots",
        title: "Read a script version's shots",
        description:
            "Returns one script version's scenes and their SHOTS, in shooting order, with the story event each shot dramatises. The storyboard table's rows are one per shot, so an agent that has to write them must be able to READ them: without this tool it could only see a scene count and would have to invent the shots it was asked to board.",
        properties: {
            scriptVersionId: id("The script version whose shots to read."),
            sceneId: str("Narrow the answer to one scene. Empty returns every scene's shots."),
            limit: { type: "integer", description: "How many shots to return.", minimum: 1, maximum: 400 },
        },
        required: ["scriptVersionId"],
    },
    {
        key: "script.create_script_structure",'''
assert old_script_read in s, "script read anchor"
s = s.replace(old_script_read, new_script_read, 1)

# 2. asset.create_gap_report and asset.read_gap_report.
old_assets = '''    {
        key: "asset.create_candidate_version",'''
new_assets = '''    {
        key: "asset.create_gap_report",
        title: "Record an asset gap report",
        description:
            "Records one analysis of what an episode's script needs and the asset library does not have, as a new DRAFT report. It cannot approve: section 10.1 gives asset_analysis a required user gate, so the report a user approves is the one the gate acts on. Every line is either a story fact an existing asset satisfies or a story fact with no asset yet.",
        properties: {
            episodeId: id("The episode this analysis is about."),
            scriptVersionId: id("The script version that was analysed."),
            summary: str("What the analysis found, in a sentence or two.", 2000),
            basedOnVersionId: str("The report this analysis replaces, or empty for the first."),
            items: {
                type: "array",
                description:
                    "One line per story fact the script needs. The order is the report's order and the ordinals are assigned from it.",
                minItems: 1,
                maxItems: 400,
                items: {
                    type: "object",
                    additionalProperties: false,
                    required: ["assetType", "status"],
                    properties: {
                        assetType: str("One of the asset types, such as character, location, prop or costume.", 60),
                        storyEntityId: str("The story entity the asset is needed for, when the analysis can name one."),
                        storyEntityName: str("The story fact's name, which is what a MISSING line has instead of an identifier."),
                        assetId: str("The asset that satisfies this line. Required when status is satisfied."),
                        status: str("Either 'missing' or 'satisfied'."),
                        usageRole: str("What the asset is used as, such as reference or subject. Empty means reference."),
                        required: { type: "boolean", description: "Whether the production cannot proceed without it. A missing REQUIRED asset blocks a batch." },
                        notes: str("Anything a reviewer should know, such as why it is optional.", 2000),
                    },
                },
            },
        },
        required: ["episodeId", "scriptVersionId", "items"],
    },
    {
        key: "asset.read_gap_report",
        title: "Read an asset gap report",
        description:
            "Returns one gap report and its lines in the report's order, or the episode's APPROVED report when the caller asks by episode. It is the read an agent makes before writing a batch, because section 9.5's rule is that a required asset which is missing stops the batch rather than being noticed later.",
        properties: {
            reportId: id("The report to read. Empty reads the episode's approved report."),
            episodeId: id("The episode whose approved report to read. Required when reportId is empty."),
        },
    },
    {
        key: "asset.create_candidate_version",'''
assert old_assets in s, "assets anchor"
s = s.replace(old_assets, new_assets, 1)

# 3. storyboard.create_storyboard_version gains items[].
old_storyboard = '''        properties: {
            episodeId: id("The episode this version belongs to."),
            scriptVersionId: id("The script version this board was built from."),
            directorPlanVersionId: id("The director plan version this board follows."),
            basedOnVersionId: str("The version this one revises, or empty for the first."),
            changeReason: str("Why this version exists, in one sentence.", 500),
        },
        required: ["episodeId", "scriptVersionId", "directorPlanVersionId"],'''
new_storyboard = '''        properties: {
            episodeId: id("The episode this version belongs to."),
            scriptVersionId: id("The script version this board was built from."),
            directorPlanVersionId: id("The director plan version this board follows."),
            basedOnVersionId: str("The version this one revises, or empty for the first."),
            changeReason: str("Why this version exists, in one sentence.", 500),
            items: {
                type: "array",
                description:
                    "The board's rows, one per shot, in shooting order. FR-070 requires every one of these fields per shot, and the ordinals are assigned from this array's order rather than supplied.",
                minItems: 1,
                maxItems: 400,
                items: {
                    type: "object",
                    additionalProperties: false,
                    required: ["shotId"],
                    properties: {
                        shotId: id("The script shot this row boards. It must belong to the version's script."),
                        shotSize: str("The framing: CU, MCU, MS, WS and so on, as FR-070 requires.", 60),
                        cameraAngle: str("Where the camera is, such as eye level or high angle.", 200),
                        cameraMovement: str("How the camera moves: static, pan, dolly, and so on.", 200),
                        durationSeconds: { type: "integer", description: "How long the shot runs.", minimum: 0, maximum: 3600 },
                        visualDescription: str("What the frame shows.", 2000),
                        actionDescription: str("What happens in the shot.", 2000),
                        dialogueAudioSummary: str("The dialogue or narration this shot carries.", 2000),
                        continuityNotes: str("What this shot must keep consistent with its neighbours.", 2000),
                        firstFrameDescription: str("What the first frame shows, which is what a video model is given to start from.", 2000),
                        lastFrameDescription: str("What the last frame shows.", 2000),
                        videoMotionDescription: str("The motion a video model is asked to produce across the shot.", 2000),
                        assetRefs: {
                            type: "array",
                            description:
                                "The assets this shot uses, by asset identifier and role. The codes check that each names an APPROVED version of the project.",
                            maxItems: 40,
                            items: {
                                type: "object",
                                additionalProperties: false,
                                required: ["assetId", "usageRole"],
                                properties: {
                                    assetId: id("The asset this shot uses."),
                                    usageRole: str("What the asset is used as, such as costume or location.", 60),
                                },
                            },
                        },
                    },
                },
            },
        },
        required: ["episodeId", "scriptVersionId", "directorPlanVersionId"],'''
assert old_storyboard in s, "storyboard anchor"
s = s.replace(old_storyboard, new_storyboard, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("tool schemas extended")
