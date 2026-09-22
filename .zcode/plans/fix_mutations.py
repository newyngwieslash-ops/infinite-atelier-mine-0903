import io

p = ".zcode/plans/mutations_wp08.py"
s = io.open(p, encoding="utf-8").read()

fixes = [
    # "compares a field with itself" left `newValue` unused. Bind it with a blank assignment.
    (
        '"new": "\\t\\tif !scriptdomain.LocksEqual(lock.Field, oldValue, oldValue) {",',
        '"new": "\\t\\t_ = newValue\\n\\t\\tif !scriptdomain.LocksEqual(lock.Field, oldValue, oldValue) {",',
    ),
    # "joinedEvents keeps the caller's order" left `sorted` unused. It is now used, so this mutation
    # is rewritten to sort the ORIGINAL slice in place — the exact wrong behaviour.
    (
        '"new": "\\t_ = sorted\\n\\treturn strings.Join(ids, \\"\\\\n\\")",',
        '"new": "\\treturn strings.Join(ids, \\"\\\\n\\")",',
    ),
    # "skeleton selection order is not preserved": the ordinal column is 0, which is legal.
    # The compile failure was the unused variable in the ORIGINAL — no, it was the 0 literal.
    # Rewrite to use index*0 so `index` stays used.
    (
        '"new": "\\t\\t\\tversionID, eventID, 0, formatTime(createdAt)); err != nil {",',
        '"new": "\\t\\t\\tversionID, eventID, index*0, formatTime(createdAt)); err != nil {",',
    ),
    (
        '"new": "\\t\\t\\tversionID, link.StoryEventID, string(link.Treatment), link.Ordinal,",',
        '"new": "\\t\\t\\t_ = position\\n\\t\\t\\tversionID, link.StoryEventID, string(link.Treatment), link.Ordinal,",',
    ),
    (
        '"new": "\\t\\t\\t\\tOrdinal:                  1,",',
        '"new": "\\t\\t\\t\\tOrdinal:                  1 + sceneIndex*0,",',
    ),
]

for old, new in fixes:
    if old not in s:
        print("NOT FOUND:", old[:80])
        continue
    s = s.replace(old, new, 1)

io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("done")
