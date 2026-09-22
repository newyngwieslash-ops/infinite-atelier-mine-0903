import io

p = 'internal/application/agentassembly/assembly_test.go'
s = io.open(p, encoding='utf-8').read()

broken = 'sections := strings.Count(document, "\n# ")'
fixed = 'sections := strings.Count(document, "\\n# ")'
assert broken in s, "the broken line is not there"
s = s.replace(broken, fixed)

io.open(p, 'w', encoding='utf-8').write(s)
print("fixed the escape")
