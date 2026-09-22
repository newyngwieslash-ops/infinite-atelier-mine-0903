import io

p = 'scripts/gen-skill-packs.mjs'
s = io.open(p, encoding='utf-8').read()

replacements = [
    # asset_analysis must be able to WRITE the report and READ the approved assets.
    (
        '''{ key: "production.execution.asset_analysis", layer: "execution", skill: "execution/asset_analysis.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["story.read_events", "asset.read_approved_assets"] },''',
        '''{ key: "production.execution.asset_analysis", layer: "execution", skill: "execution/asset_analysis.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["story.read_events", "asset.read_approved_assets", "asset.read_gap_report", "asset.create_gap_report"] },''',
    ),
    # The storyboard table needs the shots it is boarding.
    (
        '''{ key: "production.execution.storyboard_table", layer: "execution", skill: "execution/storyboard_table.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["script.read_script_version", "storyboard.read_director_plan", "storyboard.create_storyboard_version"] },''',
        '''{ key: "production.execution.storyboard_table", layer: "execution", skill: "execution/storyboard_table.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_shots", "storyboard.read_director_plan", "asset.read_approved_assets", "asset.read_gap_report", "storyboard.create_storyboard_version"] },''',
    ),
    # The storyboard table's SUPERVISOR judges continuity against the shots and the
    # approved assets, so it reads both.
    (
        '''{ key: "production.supervision.storyboard_table", layer: "supervision", skill: "supervision/storyboard_table.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "script.read_script_version"] },''',
        '''{ key: "production.supervision.storyboard_table", layer: "supervision", skill: "supervision/storyboard_table.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 16, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "script.read_script_version", "script.read_shots", "asset.read_approved_assets", "asset.read_gap_report"] },''',
    ),
    # A panel's writer reads the board it draws from and the approved assets it must use.
    (
        '''{ key: "production.execution.storyboard_panel", layer: "execution", skill: "execution/storyboard_panel.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "storyboard.create_storyboard_panel_version"] },''',
        '''{ key: "production.execution.storyboard_panel", layer: "execution", skill: "execution/storyboard_panel.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "asset.read_approved_assets", "asset.read_gap_report", "storyboard.create_storyboard_panel_version"] },''',
    ),
    # The panel's supervisor checks the panel against the board and the assets it cites.
    (
        '''{ key: "production.supervision.storyboard_panel", layer: "supervision", skill: "supervision/storyboard_panel.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "asset.read_approved_assets"] },''',
        '''{ key: "production.supervision.storyboard_panel", layer: "supervision", skill: "supervision/storyboard_panel.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 16, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "script.read_shots", "asset.read_approved_assets", "asset.read_gap_report"] },''',
    ),
    # The director plan reads the script's shots too: FR-060's shot-size distribution
    # and camera language are decisions ABOUT the shots.
    (
        '''{ key: "production.execution.director_plan", layer: "execution", skill: "execution/director_plan.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["script.read_script_version", "storyboard.create_director_plan_version"] },''',
        '''{ key: "production.execution.director_plan", layer: "execution", skill: "execution/director_plan.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 10, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_shots", "storyboard.read_director_plan", "storyboard.create_director_plan_version"] },''',
    ),
    (
        '''{ key: "production.supervision.director_plan", layer: "supervision", skill: "supervision/director_plan.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["script.read_script_version", "storyboard.read_director_plan"] },''',
        '''{ key: "production.supervision.director_plan", layer: "supervision", skill: "supervision/director_plan.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 14, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_shots", "storyboard.read_director_plan"] },''',
    ),
]

for old, new in replacements:
    assert old in s, "anchor not found: " + old[:80]
    s = s.replace(old, new, 1)

# The pack comment says WP-09 fills the production documents in; it is no longer a skeleton
# pack, so the note about which package fills them changes with it.
old_comment = '''// pack, WP-09/11 for production) fill them in.'''
new_comment = '''// pack, WP-11 for the remaining production stages) fill them in.'''
assert old_comment in s, "comment anchor"
s = s.replace(old_comment, new_comment, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("skill pack table extended")
