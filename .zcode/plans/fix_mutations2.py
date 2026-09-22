import io

p = ".zcode/plans/mutations_wp08.py"
s = io.open(p, encoding="utf-8").read()

# --- Replace or drop mutations whose target no longer exists, and fix the non-compiling ones. ---

# assertLocksCovered now indexes a map built from a SLICE, so the pattern moved.
old = '''        "name": "assertLocksCovered accepts a field no comparison covers",
        "file": "internal/application/script/enforcement.go",
        "old": "\\t\\tif !covered[lock.Field] {",
        "new": "\\t\\tif false && !covered[lock.Field] {",'''
new = '''        "name": "assertLocksCovered accepts a field no comparison covers",
        "file": "internal/application/script/enforcement.go",
        "old": "\\t\\tif !claimed[lock.Field] {",
        "new": "\\t\\tif false && !claimed[lock.Field] {",'''
assert old in s, "assertLocksCovered"
s = s.replace(old, new, 1)

# stringField is gone: it became `fieldValues` plus direct indexing, so a "claims present" mutation
# is no longer expressible. Replace it with the divergence the new design still allows.
old = '''    {
        "name": "stringField claims every field is present",
        "file": "internal/application/script/enforcement.go",
        "old": "\\t\\tvalue, present := values[field]\\n\\t\\treturn value, present",
        "new": "\\t\\tvalue := values[field]\\n\\t\\treturn value, true",
    },'''
new = '''    {
        "name": "assertLocksCovered derives coverage from the values instead of the list",
        "file": "internal/application/script/enforcement.go",
        "old": "\\t\\tif !claimed[lock.Field] {",
        "new": "\\t\\tif len(claimed) < 0 && !claimed[lock.Field] {",
    },'''
assert old in s, "stringField"
s = s.replace(old, new, 1)

# joinedEvents: keep the caller's order without leaving `sort` unused.
old = '''        "old": "\\tsort.Strings(sorted)\\n\\treturn strings.Join(sorted, \\"\\\\n\\")",
        "new": "\\treturn strings.Join(ids, \\"\\\\n\\")",'''
new = '''        "old": "\\tsort.Strings(sorted)\\n\\treturn strings.Join(sorted, \\"\\\\n\\")",
        "new": "\\t_ = sort.SearchStrings(sorted, \\"\\")\\n\\treturn strings.Join(ids, \\"\\\\n\\")",'''
assert old in s, "joinedEvents"
s = s.replace(old, new, 1)

# The strategy ordinal: keep `position` used while taking the caller's ordinal.
old = '''        "new": "\\t\\t\\t_ = position\\n\\t\\t\\tversionID, link.StoryEventID, string(link.Treatment), link.Ordinal,",'''
new = '''        "new": "\\t\\t\\t_ = position\\n\\t\\t\\t_ = record.ID\\n\\t\\t\\tversionID, link.StoryEventID, string(link.Treatment), link.Ordinal,",'''
assert old in s, "strategy ordinal"
s = s.replace(old, new, 1)

# renderTreatments moved to enforcement.go.
old = '''        "name": "renderTreatments drops the treatment and keeps only the event",
        "file": "internal/application/script/service.go",'''
new = '''        "name": "renderTreatments drops the treatment and keeps only the event",
        "file": "internal/application/script/enforcement.go",'''
assert old in s, "renderTreatments file"
s = s.replace(old, new, 1)

# The "query order" mutation was a no-op: the loop over `found` did nothing. Replace it with a real
# order-destroying edit.
old = '''    {
        "name": "the missing list is returned in query order rather than the caller's",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\\tmissing := make([]string, 0, len(wanted))\\n\\tfor _, id := range wanted {\\n\\t\\tif !found[id] {\\n\\t\\t\\tmissing = append(missing, id)\\n\\t\\t}\\n\\t}\\n\\treturn missing, nil",
        "new": "\\tmissing := make([]string, 0, len(wanted))\\n\\tfor id := range found {\\n\\t\\t_ = id\\n\\t}\\n\\tfor _, id := range wanted {\\n\\t\\tif !found[id] {\\n\\t\\t\\tmissing = append(missing, id)\\n\\t\\t}\\n\\t}\\n\\treturn missing, nil",
    },'''
new = '''    {
        "name": "the missing list is sorted rather than returned in the caller's order",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\\tmissing := make([]string, 0, len(wanted))\\n\\tfor _, id := range wanted {\\n\\t\\tif !found[id] {\\n\\t\\t\\tmissing = append(missing, id)\\n\\t\\t}\\n\\t}\\n\\treturn missing, nil",
        "new": "\\tmissing := make([]string, 0, len(wanted))\\n\\tfor _, id := range wanted {\\n\\t\\tif !found[id] {\\n\\t\\t\\tmissing = append(missing, id)\\n\\t\\t}\\n\\t}\\n\\tsort.Strings(missing)\\n\\treturn missing, nil",
    },
    {
        "name": "the caller's duplicates are reported twice rather than collapsed",
        "file": "internal/infrastructure/database/linked_versions.go",
        "old": "\\t\\tif trimmed == \\"\\" || seen[trimmed] {\\n\\t\\t\\tcontinue\\n\\t\\t}\\n\\t\\tseen[trimmed] = true",
        "new": "\\t\\tif trimmed == \\"\\" {\\n\\t\\t\\tcontinue\\n\\t\\t}\\n\\t\\tseen[trimmed] = true",
    },'''
assert old in s, "query order"
s = s.replace(old, new, 1)

io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("spec updated")
