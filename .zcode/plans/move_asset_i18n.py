import io

BLOCK_EN = '''            versions: "Versions",
            approve: "Approve",
            approved: "Approved",
            superseded: "Superseded",
            supersededHint: "Kept, not deleted: the version a later approval replaced.",
            usedBy: "Used by",
            required: "required",
            job: "Job",
            lineage: "Lineage",
            impactTitle: "What approving this version disturbs",
            impactReplaces: "This replaces {{version}}, which these consumers use:",
            impactNone: "Nothing uses the version in force.",
            impactFirst: "Nothing is approved yet, so this switch replaces nothing.",
            approveAnyway: "Approve",
            approving: "Approving…",
            approvedVersion: "The version is now in force.",
'''

BLOCK_ZH = '''            versions: "版本",
            approve: "批准",
            approved: "已批准",
            superseded: "已被取代",
            supersededHint: "保留而非删除：被后续批准取代的版本。",
            usedBy: "使用方",
            required: "必需",
            job: "任务",
            lineage: "血缘",
            impactTitle: "批准此版本会影响什么",
            impactReplaces: "此操作将取代 {{version}}，以下使用方正在引用它：",
            impactNone: "当前生效版本没有任何使用方。",
            impactFirst: "尚无已批准版本，因此本次切换不取代任何内容。",
            approveAnyway: "批准",
            approving: "批准中…",
            approvedVersion: "该版本现已生效。",
'''

for path, block in (("web/src/i18n/locales/en-US.ts", BLOCK_EN), ("web/src/i18n/locales/zh-CN.ts", BLOCK_ZH)):
    s = io.open(path, encoding='utf-8').read()
    # The block was inserted at the SCRIPT section's `noVersion:` anchor, where it collides
    # with that section's own keys. It is removed by exact text and re-inserted at the ASSETS
    # section's anchor, which is the one whose `noVersion` reads "no approved version yet".
    assert block in s, "the misplaced block is not there in " + path
    s = s.replace(block, "", 1)
    # The assets section's own anchor, which the drawer's keys belong to.
    if 'en-US' in path:
        anchor = '            empty: "No {{type}} assets yet. Create one above.",\n'
    else:
        anchor = '            empty: "尚无{{type}}资产。请在下方创建。",\n'
    assert anchor in s, "the assets anchor is not there in " + path
    s = s.replace(anchor, anchor + block, 1)
    io.open(path, 'w', encoding='utf-8').write(s)
    print("moved in " + path)
