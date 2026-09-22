import io
import re
from collections import defaultdict

# Per-file statement coverage, computed from the cover profile the run then -cover prints.
FILE_LINE = re.compile(r"^(?P<loc>\S+?):(?P<start>\d+)\.\d+,(?P<end>\d+)\.\d+\s+(?P<num>\d+)\s+(?P<count>\d+)$")
FUNC_LINE = re.compile(r"^(?P<loc>\S+?):\d+:\s+(?P<fn>\S+)\s+(?P<pct>[\d.]+)%$")

statements = defaultdict(int)
covered = defaultdict(int)
for line in io.open("wp08.cover", encoding="utf-8"):
    line = line.strip()
    if not line or line.startswith("mode:"):
        continue
    match = FILE_LINE.match(line)
    if not match:
        continue
    path = match.group("loc")
    statements[path] += int(match.group("num"))
    if int(match.group("count")) > 0:
        covered[path] += int(match.group("num"))

byfile_funcs = defaultdict(list)
for line in io.open("wp08.func.txt", encoding="utf-8"):
    match = FUNC_LINE.match(line.rstrip("\n").rstrip("\r"))
    if not match or match.group("fn") == "(statements)":
        continue
    byfile_funcs[match.group("loc")].append((match.group("fn"), float(match.group("pct"))))

out = []
for path in sorted(statements):
    pct = 100.0 * covered[path] / statements[path] if statements[path] else 0.0
    out.append("%6.1f%%  %s   (%d/%d statements)" % (pct, path, covered[path], statements[path]))
    low = sorted([f for f in byfile_funcs[path] if f[1] < 100.0], key=lambda x: x[1])
    if pct < 60.0:
        for fn, fpct in low:
            out.append("             %5.1f%%  %s" % (fpct, fn))

io.open("wp08.coverage_summary.txt", "w", encoding="utf-8").write("\n".join(out) + "\n")
print("\n".join(out))
