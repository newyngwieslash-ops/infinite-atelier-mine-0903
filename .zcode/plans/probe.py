import io
import os
import subprocess

ENV = dict(os.environ)
ENV["GOTOOLCHAIN"] = "go1.25.0"
ENV["GOSUMDB"] = "sum.golang.org"

# For each surviving mutation, ask: is there ANY observable difference? Run the whole suite of the
# two packages and see whether the mutation is caught at all, and print the failing test names when
# it is.
PROBES = [
    ("compareFieldSets skips a field whose value is missing",
     "internal/application/script/enforcement.go",
     "\t\tif !oldPresent || !newPresent {", "\t\tif !oldPresent && !newPresent {"),
    ("skeleton selection is not checked against the story graph",
     "internal/application/script/service.go",
     "\tif err := s.assertStoryEventsExist(ctx, episode.ProjectID, selected); err != nil {",
     "\tif err := error(nil); err != nil {"),
    ("skeleton selection order is not preserved",
     "internal/infrastructure/database/linked_versions.go",
     "\t\t\tversionID, eventID, index+1, formatTime(createdAt)); err != nil {",
     "\t\t\tversionID, eventID, index*0, formatTime(createdAt)); err != nil {"),
    ("strategy treatments are not checked against the story graph",
     "internal/application/script/service.go",
     "\tif err := s.assertStoryEventsExist(ctx, episode.ProjectID, treatedEvents); err != nil {",
     "\tif err := error(nil); err != nil {"),
    ("a missing treatment is defaulted rather than refused",
     "internal/application/script/service.go",
     "\t\tif treatment == \"\" {", "\t\tif treatment == \"\" && false {"),
    ("an empty payload is accepted as a version with no scenes",
     "internal/application/script/structure.go",
     "\tcase !hasStructure && !hasDraft:", "\tcase !hasStructure && !hasDraft && false:"),
    ("the project id is compared but the event ids are not trimmed",
     "internal/application/script/enforcement.go",
     "\tbase := strings.TrimSpace(versionID)\n\tif base == \"\" {",
     "\tbase := versionID\n\tif base == \"\" {"),
    ("the reference query ignores the project boundary",
     "internal/infrastructure/database/linked_versions.go",
     "\t\tAND deleted_at = '' AND id IN (%s)`, projectID, eventIDs)\n}\n\n// MissingStoryEntityIDs",
     "\t\tAND deleted_at = '' AND id IN (%s)`, \"drama-project\", eventIDs)\n}\n\n// MissingStoryEntityIDs"),
]

for name, rel, old, new in PROBES:
    path = os.path.join(".", rel)
    original = io.open(path, encoding="utf-8").read()
    if old not in original:
        print("== %s == PATTERN NOT FOUND" % name)
        continue
    io.open(path, "w", encoding="utf-8", newline="\n").write(original.replace(old, new, 1))
    try:
        result = subprocess.run(
            ["go", "test", "./internal/application/script/", "./internal/infrastructure/database/",
             "-count=1", "-v"], env=ENV, capture_output=True, text=True)
        failures = [line.split()[1] for line in result.stdout.splitlines()
                    if line.startswith("--- FAIL")]
        print("== %s ==" % name)
        if result.returncode == 0:
            print("   SURVIVED (no test fails at all)")
        else:
            print("   killed by: %s" % ", ".join(failures[:6]))
    finally:
        io.open(path, "w", encoding="utf-8", newline="\n").write(original)
