import io

p = 'internal/application/agenttools/tools_test.go'
s = io.open(p, encoding='utf-8').read()

# 1. The allOptional map gains the three new tools.
old_map = '''		"script.create_story_skeleton_version":       false,
		"script.create_adaptation_strategy_version":  false,
		"script.create_script_version":               false,
		"storyboard.read_director_plan":              false,
		"storyboard.read_storyboard":                 false,
		"storyboard.create_director_plan_version":    false,
		"storyboard.create_storyboard_version":       false,
		"storyboard.create_storyboard_panel_version": false,
		"asset.create_candidate_version":             false,
	}'''
new_map = '''		"script.create_story_skeleton_version":       false,
		"script.create_adaptation_strategy_version":  false,
		"script.create_script_version":               false,
		"storyboard.read_director_plan":              false,
		"storyboard.read_storyboard":                 false,
		"storyboard.create_director_plan_version":    false,
		"storyboard.create_storyboard_version":       false,
		"storyboard.create_storyboard_panel_version": false,
		"asset.create_candidate_version":             false,
		// WP-09's three. `script.read_shots` and `asset.create_gap_report` both require an
		// identifier; `asset.read_gap_report` is FALSE against the empty object because its
		// schema requires NOTHING and its handler refuses a call that named neither a
		// report nor an episode. The two are consistent: the schema constrains a shape and
		// the handler checks the values it uses, which is the division this table is
		// checked against.
		"script.read_shots":       false,
		"asset.create_gap_report": false,
		"asset.read_gap_report":   true,
	}'''
assert old_map in s, "allOptional anchor"
s = s.replace(old_map, new_map, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("tools_test.go updated")
