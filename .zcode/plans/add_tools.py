import io

p = 'internal/application/agenttools/tools.go'
s = io.open(p, encoding='utf-8').read()

# Deps gains the gap service, which two new tools need.
old_deps = '''type Deps struct {
	Story      *appstory.Service
	Script     *appscript.Service
	Storyboard *appstoryboard.Service
	Workflow   *appworkflow.Service
	Memory     *appmemory.Service
	Assets     *appassets.Service
	Projects   *appprojects.Service'''
new_deps = '''type Deps struct {
	Story      *appstory.Service
	Script     *appscript.Service
	Storyboard *appstoryboard.Service
	Workflow   *appworkflow.Service
	Memory     *appmemory.Service
	Assets     *appassets.Service
	// Gaps serves the asset gap report. It is a service of the ASSETS package but a
	// separate one, because a report is a different aggregate from an asset and a build
	// that has no episode or script store composes the assets service without it.
	Gaps     *appassets.GapService
	Projects *appprojects.Service'''
assert old_deps in s, "deps anchor"
s = s.replace(old_deps, new_deps, 1)

old_avail = '''func (d Deps) Available() bool {
	return d.Story != nil && d.Script != nil && d.Storyboard != nil &&
		d.Workflow != nil && d.Memory != nil && d.Assets != nil &&
		d.Projects != nil && d.Chapters != nil
}'''
new_avail = '''func (d Deps) Available() bool {
	return d.Story != nil && d.Script != nil && d.Storyboard != nil &&
		d.Workflow != nil && d.Memory != nil && d.Assets != nil &&
		d.Gaps != nil && d.Projects != nil && d.Chapters != nil
}'''
assert old_avail in s, "available anchor"
s = s.replace(old_avail, new_avail, 1)

# The three new registrations.
old_table = '''	{"script.create_script_structure", agent.ToolWrite, "project", 16 * 1024, bindCreateScriptStructure},'''
new_table = '''	{"script.create_script_structure", agent.ToolWrite, "project", 16 * 1024, bindCreateScriptStructure},

	// The shots, which are what a storyboard's rows are one per. The schema's storyboard
	// stage had only `script.read_script_version` — a summary that answers "how many
	// scenes" — so an agent asked to board a script could not see the shots it was
	// boarding and would have had to invent them.
	{"script.read_shots", agent.ToolRead, "project", 256 * 1024, bindReadShots},'''
assert old_table in s, "table anchor"
s = s.replace(old_table, new_table, 1)

old_assets = '''	{"asset.read_approved_assets", agent.ToolRead, "project", 128 * 1024, bindReadApprovedAssets},'''
new_assets = '''	{"asset.read_approved_assets", agent.ToolRead, "project", 128 * 1024, bindReadApprovedAssets},
	// The gap report's write and read. Section 10.1 gives asset_analysis a required user
	// gate and a supervisor of NONE, so the model WRITES the analysis and a person
	// approves it — which is why the write tool creates a draft and cannot approve.
	{"asset.create_gap_report", agent.ToolWrite, "project", 16 * 1024, bindCreateGapReport},
	{"asset.read_gap_report", agent.ToolRead, "project", 128 * 1024, bindReadGapReport},'''
assert old_assets in s, "assets anchor"
s = s.replace(old_assets, new_assets, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("tools.go extended")
