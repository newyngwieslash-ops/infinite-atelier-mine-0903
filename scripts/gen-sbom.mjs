#!/usr/bin/env node
// Generates the software bill of materials under sbom/.
//
// This exists because PRD section 18 blocks release while "许可证和第三方声明缺失", and
// SECURITY section 15 lists SBOM as a CI gate. A hand-written SBOM for 130-odd Go
// modules and 1,388 npm packages cannot stay true: it is stale the moment a
// dependency moves, and nothing notices. So the document is generated from the two
// lockfiles that are actually pinned, and `--check` fails when the checked-in output
// stops matching what those lockfiles say.
//
// FORMAT: CycloneDX 1.5 JSON, not SPDX. Both are standard and both were available.
// CycloneDX was chosen because its `components[].licenses[].license.id` field carries
// an SPDX identifier directly, so a licence policy check has one field to read, and
// because the npm and Go ecosystems both publish CycloneDX tooling — meaning a
// future release can switch to `cyclonedx-gomod`/`cyclonedx-npm` without changing
// the artifact's schema. It was NOT chosen to imply anything about the tooling:
// neither `syft`, `cyclonedx-gomod` nor `cyclonedx-npm` is installed on this host,
// and this generator uses neither.
//
// WHAT IS READ, AND WHAT IS DELIBERATELY NOT
//
// Only lockfiles and metadata already on disk:
//   - go.sum            — every module@version in the build graph (61 entries).
//   - go.mod            — which of those are direct requirements.
//   - the module cache  — each module's LICENCE FILE, read to identify the licence.
//   - web/package-lock.json — all 1,388 resolved packages with their declared licence.
//   - node_modules/*/package.json — the installed metadata npm audit reads.
//
// Nothing here reads the network, and nothing writes outside sbom/. In particular it
// does NOT run `go mod download`, because that command rewrites go.sum when a
// module in the graph has no build-list entry — which is a real modification to a
// tracked file, made by a generator whose job is to observe. A module whose licence
// file is absent from the cache is reported as `unknown` rather than fetched.
//
// LICENCE IDENTIFICATION IS BEST-EFFORT AND SAYS SO. Detection reads the licence
// file's text for well-known markers. A file matching no marker is recorded as
// `unknown` with its filename, never guessed at. Two things follow, and both are
// recorded in the generated document rather than only here:
//   - `unknown` is a FINDING, not a pass. sbom/licences.json lists them.
//   - A detected marker can be wrong (a permissively-licensed file quoting GPL text
//     in a comparison table, say). The file is named in the component so a human can
//     read it.
//
// Usage: node scripts/gen-sbom.mjs
//        node scripts/gen-sbom.mjs --check
//
// A NOTE FOR WHOEVER APPENDS TO THIRD_PARTY_NOTICES.md NEXT. `git diff --check` is an
// AGENTS section 4.4 gate, and eight upstream licence files wrap their text with a
// TRAILING SPACE, so reproducing a licence verbatim can fail that gate on content the
// project does not control. Trailing whitespace carries no legal meaning: strip it
// inside the fence, state that you did, and keep the gate meaningful. A gate everyone
// learns to ignore is worse than no gate.

import { readFileSync, writeFileSync, mkdirSync, existsSync, readdirSync, statSync } from "node:fs";import { execFileSync } from "node:child_process";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const sbomRoot = join(repoRoot, "sbom");

// --- Go module cache location -------------------------------------------------
//
// `go env GOMODCACHE` is asked rather than assumed, because the path is per-machine.
// When go is unavailable the Go half is emitted with no licence identifications and
// the reason is recorded, rather than the generator failing or inventing paths: a
// missing toolchain must not turn into a licence claim.
function goEnv(key) {
    try {
        return execFileSync("go", ["env", key], {
            cwd: repoRoot,
            encoding: "utf8",
            env: { ...process.env, GOTOOLCHAIN: process.env.GOTOOLCHAIN || "go1.25.0", GOSUMDB: process.env.GOSUMDB || "sum.golang.org" },
            stdio: ["ignore", "pipe", "ignore"],
        }).trim();
    } catch {
        return "";
    }
}

// The module cache escapes uppercase letters as `!` + lowercase, so a path can be
// case-insensitive on a case-insensitive filesystem without colliding.
function escapeModulePath(p) {
    return p.replace(/[A-Z]/g, (c) => "!" + c.toLowerCase());
}

function licenceFileIn(dir) {
    let entries;
    try {
        entries = readdirSync(dir);
    } catch {
        return null;
    }
    const matches = entries
        .filter((f) => /^(LICEN[CS]E|COPYING|NOTICE)/i.test(f))
        .filter((f) => statSync(join(dir, f)).isFile())
        .sort();
    if (matches.length === 0) {
        return null;
    }
    // `LICENSE` before `LICENSE.txt` before the rest: the plainest file is the one
    // most likely to be the licence itself rather than a notice about one.
    const rank = (f) => (/^licen[cs]e$/i.test(f) ? 0 : /^licen[cs]e\.(txt|md)$/i.test(f) ? 1 : 2);
    matches.sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
    return matches[0];
}

// --- Licence text → SPDX identifier -------------------------------------------
//
// Ordered most-specific first. GPL and AGPL are checked BEFORE the permissive
// markers because their text quotes nothing permissive but permissive texts
// sometimes mention GPL, and because a restrictive licence is the answer that
// matters for AGENTS section 6.
//
// EVERY marker runs against whitespace-normalised text. Licence files wrap at
// column 80, so "obtaining a copy" appears as "obtaining a\ncopy" often enough to
// matter: a pattern with literal spaces reported four permissively-licensed
// modules as `unknown` on this repository's own cache, which was found by reading
// the four files rather than by trusting the report.
// The HEADING markers are case-SENSITIVE on purpose, and that is the whole reason they
// are separate from the `-or-later` markers below. MPL-2.0's own text defines
// "Incompatible With Secondary Licenses" by referring to "the GNU Affero General Public
// License, Version 3.0" and "the GNU General Public License, Version 2.1" in ordinary
// sentence case. With a case-insensitive match — and whitespace flattened, so the phrase
// can span a line break — those two references reported `hashicorp/golang-lru/v2` and
// `cyphar/filepath-securejoin` as AGPL, which would have been a false RELEASE BLOCKER
// against two MPL packages. A real GPL/AGPL licence file states its name in an all-caps
// heading ("GNU AFFERO GENERAL PUBLIC LICENSE"), so requiring the caps separates the
// licence from a reference to it.
//
// The version still has to be read, though: an earlier attempt at this fix dropped the
// version from the heading patterns entirely and then reported a GPL-2.0 file and an
// LGPL-2.1 file as their version-3 cousins. The heading is matched case-sensitively AND
// followed by its version, which is what every real file of these licences contains.
// A negative control over synthetic AGPL/SSPL/GPL-3/GPL-2/LGPL-2.1 texts is what caught
// that regression, and it is why one exists.
const LICENCE_MARKERS = [
    { id: "AGPL-3.0-or-later", re: /GNU Affero General Public License[\s\S]{0,400}?version 3[\s\S]{0,200}?or \(at your option\) any later version/i },
    { id: "AGPL-3.0-only", re: /GNU AFFERO GENERAL PUBLIC LICENSE[\s\S]{0,120}?Version 3/ },
    { id: "SSPL-1.0", re: /Server Side Public License/i },
    { id: "GPL-3.0-or-later", re: /GNU General Public License[\s\S]{0,400}?either version 3[\s\S]{0,200}?any later version/i },
    { id: "GPL-3.0-only", re: /GNU GENERAL PUBLIC LICENSE[\s\S]{0,120}?Version 3/ },
    { id: "LGPL-3.0-only", re: /GNU LESSER GENERAL PUBLIC LICENSE[\s\S]{0,120}?Version 3/ },
    { id: "GPL-2.0-only", re: /GNU GENERAL PUBLIC LICENSE[\s\S]{0,120}?Version 2/ },
    { id: "LGPL-2.1-only", re: /GNU LESSER GENERAL PUBLIC LICENSE[\s\S]{0,120}?Version 2\.1/ },
    { id: "MPL-2.0", re: /Mozilla Public License[,]? [Vv]ersion 2\.0/i },
    { id: "Apache-2.0", re: /Apache License[\s\S]{0,40}?Version 2\.0/i },
    { id: "EPL-2.0", re: /Eclipse Public License[\s\S]{0,40}?- v 2\.0/i },
    // `modernc.org/*` writes the third clause as "Neither the NAMES of the authors nor
    // the names of the contributors may be used to endorse or promote", which a
    // pattern anchored on "Neither the name of" reported as BSD-2-Clause — a WRONG
    // identification, worse than an unknown, found by reading the file. Both wordings
    // are matched, and the clause is what identifies BSD-3 in either.
    { id: "BSD-3-Clause", re: /Neither the name[s]? (?:of|of the authors|of the copyright holders)[\s\S]{0,400}?endorse or promote/i },
    { id: "BSD-2-Clause", re: /Redistributions of source code must retain the above copyright/i },
    { id: "ISC", re: /Permission to use, copy, modify, and\s*\/?\s*or distribute this software for any purpose with or without fee/i },
    { id: "MIT", re: /Permission is hereby granted, free of charge, to any person obtaining a copy/i },
    { id: "Unlicense", re: /This is free and unencumbered software released into the public domain/i },
    { id: "CC0-1.0", re: /CC0 1\.0 Universal/i },
    { id: "0BSD", re: /Zero-Clause BSD/i },
    { id: "BlueOak-1.0.0", re: /Blue Oak Model License[\s\S]{0,40}?Version 1\.0\.0/i },
    { id: "CC-BY-4.0", re: /Creative Commons Attribution 4\.0/i },
    { id: "Python-2.0", re: /Python Software Foundation License/i },
];

function identifyLicence(text) {
    // An explicit SPDX declaration is the licence's OWN authoritative statement and
    // wins over any text scan. `cyphar/filepath-securejoin`'s COPYING.md opens with
    // "SPDX-License-Identifier: BSD-3-Clause AND MPL-2.0" and then embeds the full text
    // of both, so no marker order can answer it correctly — the file says which one it
    // is, and reading the declaration is both simpler and right.
    const spdx = /SPDX-License-Identifier:\s*([A-Za-z0-9.\-+ ]+(?:\s+(?:AND|OR|WITH)\s+[A-Za-z0-9.\-+ ]+)*)/.exec(text);
    if (spdx) {
        return spdx[1].trim().replace(/\s+/g, " ");
    }
    // Wrap-insensitive: every run of whitespace collapses to one space, so a marker
    // matches whether the phrase sits on one line or three.
    const flat = text.replace(/\s+/g, " ");
    // A dual-licensed file says `A OR B`; the markers are scanned in order so the
    // first match is the strictest one the text names. A file naming two licences is
    // recorded as the first identified and its filename is kept for a human to read.
    for (const m of LICENCE_MARKERS) {
        if (m.re.test(flat)) {
            return m.id;
        }
    }
    return "unknown";
}

// --- Go half ------------------------------------------------------------------
//
// Three tiers, because "which Go modules does this application contain" has three
// different true answers and collapsing them would make one of them false:
//
//   linked   — a module that provides a package in `go build ./...`'s dependency
//              closure. 18 modules. This is what the shipped executable links, and a
//              CVE here is a CVE in the product.
//   test     — a module that provides a package in `go list -deps -test ./...`'s
//              closure but not in the plain build closure. A CVE here affects the
//              test run, not the shipped binary.
//   graph    — every other module@version in `go list -m all`, which is the fully
//              resolved module graph. It includes build-time tools and modules only
//              reached through a file this program never compiles.
//
// All three are emitted, each tier named in a property, because a supply-chain
// reviewer wants the whole graph AND wants to know which part of it a user's input
// can reach. The tier is computed by RUNNING the build query, not by guessing from
// the lockfile: guessing is how a module that is linked but not source-hashed
// (a replace directive, or a vendor directory) would silently vanish.
//
// `go list` is asked with `-mod=readonly` so a query cannot rewrite go.mod/go.sum.
// That matters: this generator's whole job is to observe, and an observation that
// edits a tracked file is not an observation.

function goListSet(args) {
    let out;
    try {
        out = execFileSync("go", args, {
            cwd: repoRoot,
            encoding: "utf8",
            maxBuffer: 96 * 1024 * 1024,
            env: { ...process.env, GOTOOLCHAIN: process.env.GOTOOLCHAIN || "go1.25.0", GOSUMDB: process.env.GOSUMDB || "sum.golang.org", GOFLAGS: "-mod=readonly" },
            stdio: ["ignore", "pipe", "pipe"],
        });
    } catch (err) {
        // The failure is REPORTED rather than swallowed into a silently empty tier.
        // An earlier version caught here and returned null while the parse below
        // threw on a `new` of a generator function, so every module was reported as
        // tier=graph with an empty linked set — a wrong answer that looked like a
        // successful run. The message is carried to the caller's notes.
        goListSet.lastError = err?.stderr?.toString?.() || err?.message || String(err);
        return null;
    }
    goListSet.lastError = "";
    return parseGoListStream(out);
}

// `go list -json` emits a concatenated stream of JSON objects, not an array, so they
// are split on braces at depth zero. A string-aware scan is required: a `}` inside a
// package's source snippet would otherwise end an object early.
//
// Both shapes are accepted, because the two queries this makes use of different ones:
// `go list -m -json all` puts Path/Version at the TOP level (a module record), while
// `go list -json ./...` nests them under `Module` (a package record). Handling only
// the second is how an earlier version reported a 131-module graph as empty.
function parseGoListStream(text) {
    const mods = new Map();
    let depth = 0, start = -1, inString = false, escaped = false;
    for (let i = 0; i < text.length; i++) {
        const c = text[i];
        if (inString) {
            if (escaped) escaped = false;
            else if (c === "\\") escaped = true;
            else if (c === '"') inString = false;
            continue;
        }
        if (c === '"') { inString = true; continue; }
        if (c === "{") { if (depth === 0) start = i; depth++; }
        else if (c === "}") {
            depth--;
            if (depth === 0 && start >= 0) {
                let obj = null;
                try { obj = JSON.parse(text.slice(start, i + 1)); } catch { obj = null; }
                const m = obj?.Module || obj;
                if (m?.Path && m?.Version) mods.set(`${m.Path}@${m.Version}`, { path: m.Path, version: m.Version });
                start = -1;
            }
        }
    }
    return mods;
}

function readGoSum() {
    const path = join(repoRoot, "go.sum");
    const pairs = new Map();
    for (const line of readFileSync(path, "utf8").split(/\r?\n/)) {
        const parts = line.trim().split(/\s+/);
        if (parts.length < 2) continue;
        const key = `${parts[0]}@${parts[1]}`;
        const entry = pairs.get(key) || { path: parts[0], goModSum: "", sourceSum: "" };
        if (parts[1].endsWith("/go.mod")) entry.goModSum = parts[2] || "";
        else entry.sourceSum = parts[2] || "";
        pairs.set(key, entry);
    }
    return pairs;
}

function readGoModDirect() {
    const text = readFileSync(join(repoRoot, "go.mod"), "utf8");
    const direct = new Set();
    for (const line of text.split(/\r?\n/)) {
        const m = /^\s*([^\s/][^\s]*)\s+(v[^\s]+)(\s+\/\/\s*indirect)?\s*$/.exec(line);
        if (m && !m[3]) direct.add(m[1]);
    }
    return direct;
}

function readGoModuleName() {
    const text = readFileSync(join(repoRoot, "go.mod"), "utf8");
    const m = /^module\s+(\S+)/m.exec(text);
    return m ? m[1] : "unknown";
}

function goComponents(notes) {
    const sums = readGoSum();
    const direct = readGoModDirect();
    const listAll = goListSet(["list", "-m", "-json", "all"]);
    const listBuild = goListSet(["list", "-deps", "-json", "./..."]);
    const listTest = goListSet(["list", "-deps", "-test", "-json", "./..."]);

    if (!listAll || !listBuild || !listTest) {
        notes.push(`\`go list\` did not answer, so no Go component could be assigned a tier; all Go components are recorded as tier=graph. ${goListSet.lastError}`);
    }

    // The universe: the module graph when go answered, otherwise every sum entry.
    const universe = listAll ? new Map(listAll) : new Map(sums);
    const cache = goEnv("GOMODCACHE");
    if (!cache) {
        notes.push("go env GOMODCACHE returned nothing, so no Go licence file could be read; every Go component is recorded with license.id unknown.");
    }

    const components = [];
    for (const key of [...universe.keys()].sort()) {
        const { path: modPath, version } = universe.get(key);
        const sum = sums.get(key);
        let tier = "graph";
        if (listBuild && listBuild.has(key)) tier = "linked";
        else if (listTest && listTest.has(key)) tier = "test";

        const component = {
            type: "library",
            "bom-ref": `pkg:golang/${modPath}@${version}`,
            name: modPath,
            version,
            purl: `pkg:golang/${modPath}@${version}`,
            // scope follows the tier rather than go.mod's `indirect` marker: the
            // marker says whether a human wrote the requirement down, which is not
            // the same question as whether the code ships.
            scope: tier === "linked" ? "required" : "optional",
            properties: [
                { name: "infinite-atelier:ecosystem", value: "go" },
                { name: "infinite-atelier:tier", value: tier },
                { name: "infinite-atelier:direct", value: direct.has(modPath) ? "true" : "false" },
                { name: "infinite-atelier:go.sum-source-hash", value: sum?.sourceSum || "" },
                { name: "infinite-atelier:go.sum-go.mod-hash", value: sum?.goModSum || "" },
            ],
        };
        if (cache) {
            const dir = join(cache, `${escapeModulePath(modPath)}@${version}`);
            const file = licenceFileIn(dir);
            if (file) {
                const text = readFileSync(join(dir, file), "utf8");
                const id = identifyLicence(text);
                component.licenses = id === "unknown" ? [{ license: { name: "unknown" } }] : [{ license: { id } }];
                component.properties.push({ name: "infinite-atelier:license-source", value: `${file} in the module cache` });
                if (id === "unknown") {
                    component.properties.push({ name: "infinite-atelier:license-unidentified-file", value: file });
                }
            } else {
                component.licenses = [{ license: { name: "unknown" } }];
                component.properties.push({
                    name: "infinite-atelier:license-source",
                    value: "no source directory or licence file in the module cache; the generator does not fetch, because a fetch can rewrite go.sum",
                });
            }
        } else {
            component.licenses = [{ license: { name: "unknown" } }];
        }
        components.push(component);
    }
    return components;
}

// --- npm half -----------------------------------------------------------------
//
// Every resolved package in package-lock.json, not just the direct dependencies:
// a transitive package is in the shipped bundle and its licence is the distributor's
// problem exactly as much as a direct one's. The lockfile records `dev: true` for
// dev-only subtrees, which is how production and development are told apart here.

function npmComponents() {
    const lock = JSON.parse(readFileSync(join(repoRoot, "web", "package-lock.json"), "utf8"));
    const root = lock.packages[""] || {};
    const prodNames = new Set(Object.keys(root.dependencies || {}));
    const components = [];
    // One component per RESOLVED LOCKFILE SLOT, not one per name@version: npm nests a
    // second copy of a package when two dependents need incompatible ranges, and 52
    // name@version pairs occur more than once that way. Merging them would lose which
    // copy a given dependent resolves, so the bom-ref carries the install path.
    for (const [path, meta] of Object.entries(lock.packages)) {
        if (path === "") continue;
        const name = path.split("node_modules/").pop();
        const version = meta.version || "unknown";
        const declared = meta.license || "";
        // A declared licence may be an SPDX expression ("(MPL-2.0 OR Apache-2.0)"),
        // which CycloneDX carries as an expression rather than an id.
        const isExpression = /[()]|\s(OR|AND)\s/.test(declared);
        // `isDirect` is decided by asking whether this slot IS the root's dependency,
        // not by testing the path for a nested node_modules: a hoisted transitive
        // package also sits at the top level, and calling those direct would overstate
        // what this project chose.
        const isDirectProduction = path === `node_modules/${name}` && prodNames.has(name);
        const component = {
            type: "library",
            "bom-ref": `pkg:npm/${name.replace("@", "%40")}@${version}?path=${path.replace(/@/g, "%40")}`,
            name,
            version,
            purl: `pkg:npm/${name.replace("@", "%40")}@${version}`,
            scope: meta.dev ? "optional" : "required",
            properties: [
                { name: "infinite-atelier:ecosystem", value: "npm" },
                { name: "infinite-atelier:tier", value: meta.dev ? "dev" : "production" },
                { name: "infinite-atelier:direct", value: isDirectProduction ? "true" : "false" },
                { name: "infinite-atelier:lockfile-path", value: path },
            ],
        };
        if (declared) {
            component.licenses = isExpression ? [{ expression: declared }] : [{ license: { id: declared } }];
        } else {
            // The lockfile records no license field. Three sources are tried in order
            // of directness, because the packages in this state are NOT the same case
            // and recording them alike would call a clear declaration unidentified:
            //   1. the installed package.json's legacy `licenses` ARRAY, which is what
            //      the old npm format used and what `format@0.2.2` still carries;
            //   2. a licence FILE in the installed package, which `khroma@2.1.0` ships
            //      (plain MIT on disk, simply absent from the lockfile entry);
            //   3. nothing — recorded as `unknown` with the reason.
            const installed = join(repoRoot, "web", path);
            const pkgJson = join(installed, "package.json");
            let legacy = "";
            if (existsSync(pkgJson)) {
                try {
                    const meta2 = JSON.parse(readFileSync(pkgJson, "utf8"));
                    const arr = meta2.licenses;
                    if (Array.isArray(arr) && arr.length > 0) {
                        legacy = arr.map((l) => (typeof l === "string" ? l : l?.type)).filter(Boolean).join(" OR ");
                    } else if (typeof arr === "string") {
                        legacy = arr;
                    }
                } catch { /* an unreadable package.json falls through to the file check */ }
            }
            if (legacy) {
                component.licenses = [{ license: { id: legacy } }];
                component.properties.push({ name: "infinite-atelier:license-source", value: `package-lock.json records no license field; the installed package.json's legacy \`licenses\` field says ${legacy}` });
            } else {
                const file = existsSync(installed) ? licenceFileIn(installed) : null;
                if (file) {
                    const id = identifyLicence(readFileSync(join(installed, file), "utf8"));
                    component.licenses = id === "unknown" ? [{ license: { name: "unknown" } }] : [{ license: { id } }];
                    component.properties.push({ name: "infinite-atelier:license-source", value: `${file} in the installed package; package-lock.json records no license field` });
                } else {
                    component.licenses = [{ license: { name: "unknown" } }];
                    component.properties.push({ name: "infinite-atelier:license-source", value: "package-lock.json records no license field and the installed package ships no licence file" });
                }
            }
        }
        components.push(component);
    }
    return components.sort((a, b) => a.name.localeCompare(b.name) || a.version.localeCompare(b.version) || a["bom-ref"].localeCompare(b["bom-ref"]));
}

// --- Direct-dependency licence report -----------------------------------------
//
// The question AGENTS section 6 actually asks is about what this project chose to
// depend on, so the direct sets get their own report: every direct Go requirement
// and every production npm dependency, with its identified licence. A restrictive
// identifier here is a release blocker under section 6, not a note.

const RESTRICTIVE = new Set([
    "AGPL-3.0-only", "AGPL-3.0-or-later", "GPL-2.0-only", "GPL-2.0-or-later",
    "GPL-3.0-only", "GPL-3.0-or-later", "LGPL-2.1-only", "LGPL-2.1-or-later",
    "LGPL-3.0-only", "LGPL-3.0-or-later", "SSPL-1.0", "EUPL-1.2", "OSL-3.0",
]);

function directLicenceReport(goComponentsList, npmComponentsList) {
    const rows = [];
    for (const c of goComponentsList) {
        if (c.properties.find((p) => p.name === "infinite-atelier:direct")?.value !== "true") continue;
        const lic = c.licenses[0];
        rows.push({
            ecosystem: "go", name: c.name, version: c.version, relationship: "direct requirement",
            tier: c.properties.find((p) => p.name === "infinite-atelier:tier")?.value || "",
            license: lic.license?.id || lic.expression || lic.license?.name || "unknown",
            source: c.properties.find((p) => p.name === "infinite-atelier:license-source")?.value || "",
        });
    }
    for (const c of npmComponentsList) {
        if (c.properties.find((p) => p.name === "infinite-atelier:direct")?.value !== "true") continue;
        const lic = c.licenses[0];
        rows.push({
            ecosystem: "npm", name: c.name, version: c.version, relationship: "production dependency",
            tier: c.properties.find((p) => p.name === "infinite-atelier:tier")?.value || "",
            license: lic.license?.id || lic.expression || lic.license?.name || "unknown",
            source: `web/package-lock.json packages["${c.properties.find((p) => p.name === "infinite-atelier:lockfile-path")?.value}"].license`,
        });
    }
    for (const row of rows) {
        row.restrictive = RESTRICTIVE.has(row.license);
        row.unidentified = row.license === "unknown";
    }
    return rows.sort((a, b) => a.ecosystem.localeCompare(b.ecosystem) || a.name.localeCompare(b.name));
}

// --- Emission -----------------------------------------------------------------

function build() {
    const notes = [];
    const go = goComponents(notes);
    const npm = npmComponents();
    const direct = directLicenceReport(go, npm);

    const tierOf = (c) => c.properties.find((p) => p.name === "infinite-atelier:tier")?.value || "";
    const idOf = (c) => c.licenses[0].license?.id || c.licenses[0].license?.name || c.licenses[0].expression || "unknown";

    const unknownGo = go.filter((c) => idOf(c) === "unknown");
    const unknownNpm = npm.filter((c) => idOf(c) === "unknown");
    // An unidentified licence in the LINKED closure is the finding that can block a
    // release. One in the module graph alone is recorded and does not.
    const unknownLinkedGo = unknownGo.filter((c) => tierOf(c) === "linked");
    const restrictive = direct.filter((r) => r.restrictive);

    const bom = {
        bomFormat: "CycloneDX",
        specVersion: "1.5",
        version: 1,
        metadata: {
            timestamp: "1970-01-01T00:00:00Z",
            // A fixed timestamp, not the wall clock: a generated artifact that changes
            // on every run cannot be checked for drift, and the check is the only
            // reason this file is generated rather than written.
            tools: [{ vendor: "Infinite Atelier", name: "scripts/gen-sbom.mjs", version: "1" }],
            component: {
                type: "application",
                "bom-ref": `pkg:golang/${readGoModuleName()}`,
                name: "Infinite Atelier Drama Studio",
                version: readFileSync(join(repoRoot, "VERSION"), "utf8").trim(),
            },
            properties: [
                { name: "infinite-atelier:go-components", value: String(go.length) },
                { name: "infinite-atelier:go-linked", value: String(go.filter((c) => tierOf(c) === "linked").length) },
                { name: "infinite-atelier:go-test", value: String(go.filter((c) => tierOf(c) === "test").length) },
                { name: "infinite-atelier:go-graph", value: String(go.filter((c) => tierOf(c) === "graph").length) },
                { name: "infinite-atelier:npm-components", value: String(npm.length) },
                { name: "infinite-atelier:npm-production", value: String(npm.filter((c) => tierOf(c) === "production").length) },
                { name: "infinite-atelier:npm-dev", value: String(npm.filter((c) => tierOf(c) === "dev").length) },
                { name: "infinite-atelier:go-licenses-unknown", value: String(unknownGo.length) },
                { name: "infinite-atelier:go-linked-licenses-unknown", value: String(unknownLinkedGo.length) },
                { name: "infinite-atelier:npm-licenses-unknown", value: String(unknownNpm.length) },
                { name: "infinite-atelier:restrictive-direct-licenses", value: String(restrictive.length) },
                ...notes.map((n) => ({ name: "infinite-atelier:note", value: n })),
            ],
        },
        components: [...go, ...npm],
    };

    const summary = {
        generatedBy: "scripts/gen-sbom.mjs",
        goComponents: go.length,
        goTiers: {
            linked: go.filter((c) => tierOf(c) === "linked").map((c) => `${c.name}@${c.version}`),
            test: go.filter((c) => tierOf(c) === "test").map((c) => `${c.name}@${c.version}`),
            graph: go.filter((c) => tierOf(c) === "graph").length,
        },
        npmComponents: npm.length,
        npmTiers: {
            production: npm.filter((c) => tierOf(c) === "production").length,
            dev: npm.filter((c) => tierOf(c) === "dev").length,
        },
        directDependencies: direct.length,
        licensesUnknown: {
            go: unknownGo.map((c) => `${c.name}@${c.version}`),
            goLinked: unknownLinkedGo.map((c) => `${c.name}@${c.version}`),
            npm: unknownNpm.map((c) => `${c.name}@${c.version}`),
        },
        restrictiveLicensesAmongDirectDependencies: restrictive,
        directDependencyLicenses: direct,
        notes,
    };
    return { bom, summary };
}

function main() {
    const check = process.argv.includes("--check");
    const { bom, summary } = build();
    const bomPath = join(sbomRoot, "cyclonedx.json");
    const summaryPath = join(sbomRoot, "licences.json");
    const bomText = JSON.stringify(bom, null, 2) + "\n";
    const summaryText = JSON.stringify(summary, null, 2) + "\n";
    let drifted = 0;
    if (check) {
        for (const [path, rendered] of [[bomPath, bomText], [summaryPath, summaryText]]) {
            const existing = existsSync(path) ? readFileSync(path, "utf8") : "";
            if (existing !== rendered) {
                console.error(`drift: ${path.slice(repoRoot.length + 1)} is not what the generator produces`);
                drifted++;
            }
        }
        return drifted;
    }
    mkdirSync(dirname(bomPath), { recursive: true });
    writeFileSync(bomPath, bomText);
    writeFileSync(summaryPath, summaryText);
    console.log(`wrote sbom/cyclonedx.json (${bom.components.length} components: ${summary.goTiers.linked.length} linked Go, ${summary.npmTiers.production} production npm) and sbom/licences.json (${summary.directDependencies} direct dependencies)`);
    if (summary.licensesUnknown.goLinked.length > 0) {
        console.error(`FAIL: ${summary.licensesUnknown.goLinked.length} module(s) in the LINKED closure have an unidentified licence:`);
        for (const m of summary.licensesUnknown.goLinked) console.error(`  ${m}`);
        return 1;
    }
    if (summary.restrictiveLicensesAmongDirectDependencies.length > 0) {
        console.error(`FAIL: ${summary.restrictiveLicensesAmongDirectDependencies.length} direct dependency licence(s) are restrictive under AGENTS section 6:`);
        for (const r of summary.restrictiveLicensesAmongDirectDependencies) {
            console.error(`  ${r.ecosystem} ${r.name}@${r.version} — ${r.license}`);
        }
        return 1;
    }
    return 0;
}

process.exit(main() === 0 ? 0 : 1);
