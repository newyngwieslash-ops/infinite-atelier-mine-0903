import io

p = 'docs/implementation/STATUS.md'
s = io.open(p, encoding='utf-8').read()

old = '''> Last updated: 2026-09-22
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-09 — ScriptAgent：骨架、策略与剧本**
> Status: **COMPLETE for the 15 ROADMAP items, with the two acceptance items and three verification limits named in section 0i.** The Script Agent layer is built end to end: the three execution stages with their supervisors and the decision agent, eight real skill documents, the whole-content structure write with explicit ceilings, field locks enforced at the write path for all three version families, the version diff, the canvas projection, the FIX loop that reads a user's findings back from the decision row, the stage→agent map the registry's last-segment heuristic cannot derive, the per-layer model policy, the desktop bindings, and the Script UI. Section 0i states what this package delivered, the defects TWO INDEPENDENT REVIEWS found in it, and — plainly — the three things its tests do not cover. WP-01 through WP-07 remain COMPLETE for their recorded scopes.'''

new = '''> Last updated: 2026-09-22
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-09 — ProductionAgent：资产、导演与分镜**
> Status: **COMPLETE for the 14 ROADMAP items, with three PARTIAL acceptance clauses and two verification limits named in section 0j.** The production half of the pipeline is built end to end: the stage mechanism extracted so two layers share one implementation, the director plan and the asset gap analysis and the storyboard table and the panel stages with their supervisors, nine real skill documents, migration 000018's FR-070 columns and the versioned gap report, the asset provenance the mapper had been dropping, the impact edge that makes an approval switch reach a panel, the image batch with its gate and the candidate collection and the single-row fix, the deterministic image mock that lets AC-BOARD-003 run in CI, the MONOFORM envelope, the two Studio sections, and the shot projection. Section 0j states what this package delivered, the defects TWO INDEPENDENT REVIEWS found in it, and — plainly — the three criteria that are PARTIAL and why. WP-01 through WP-08 remain COMPLETE for their recorded scopes.'''

assert old in s, "header anchor"
s = s.replace(old, new, 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("header corrected")
