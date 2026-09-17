const fs = require("fs");
const p = "internal/infrastructure/database/legacy.go";
let s = fs.readFileSync(p, "utf8");
const from = "SELECT COUNT(*) FROM legacy_project_imports WHERE fingerprint = ?";
const to = "SELECT COUNT(*) FROM legacy_imports WHERE source_fingerprint = ? AND status = 'completed'";
if (!s.includes(from)) { console.error("anchor missing"); process.exit(1); }
fs.writeFileSync(p, s.replace(from, to));
console.log("mutation applied: run-level fingerprint restored");
