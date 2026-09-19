import assert from "node:assert/strict";
import test from "node:test";

import en from "../../i18n/locales/en-US";
import zh from "../../i18n/locales/zh-CN";

/**
 * The two locales must describe the same key tree.
 *
 * i18next falls back silently: a key present in one locale and missing in the
 * other renders the raw key path in that language and no test notices. That makes
 * an unmatched edit invisible in exactly the review where it matters, since the
 * person editing usually reads one language and not the other.
 *
 * This is also a check on the FILE rather than on the translation. An insertion
 * into a nested object is easy to place one level too deep, and the resulting
 * TypeScript is still valid: the keys simply end up somewhere no component looks
 * for them, and the fallback hides it. Comparing the flattened key sets catches
 * that even when both languages were edited the same wrong way.
 */

/** flatten turns a nested translation object into a path-to-value map. */
function flatten(value: unknown, prefix = ""): Map<string, string> {
    const out = new Map<string, string>();
    if (typeof value === "string") {
        out.set(prefix, value);
        return out;
    }
    if (value === null || typeof value !== "object") {
        return out;
    }
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
        const path = prefix === "" ? key : `${prefix}.${key}`;
        for (const [nested, leaf] of flatten(child, path)) {
            out.set(nested, leaf);
        }
    }
    return out;
}

const englishKeys = flatten(en);
const chineseKeys = flatten(zh);

test("both locales define the same keys", () => {
    const missingInChinese = [...englishKeys.keys()].filter((key) => !chineseKeys.has(key)).sort();
    const missingInEnglish = [...chineseKeys.keys()].filter((key) => !englishKeys.has(key)).sort();
    assert.deepEqual(
        missingInChinese,
        [],
        `these keys are in en-US and not in zh-CN: ${missingInChinese.join(", ")}`,
    );
    assert.deepEqual(
        missingInEnglish,
        [],
        `these keys are in zh-CN and not in en-US: ${missingInEnglish.join(", ")}`,
    );
    assert.ok(englishKeys.size > 0, "flattening found no keys, so the check would pass on anything");
});

test("interpolation placeholders match between locales", () => {
    // A placeholder that exists in one language and not the other is a message
    // that renders "{{count}}" literally to the user who reads the other one.
    const placeholderPattern = /\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g;
    const mismatches: string[] = [];
    for (const [key, english] of englishKeys) {
        const chinese = chineseKeys.get(key);
        if (chinese === undefined) continue;
        const englishNames = [...english.matchAll(placeholderPattern)].map((match) => match[1]).sort();
        const chineseNames = [...chinese.matchAll(placeholderPattern)].map((match) => match[1]).sort();
        if (englishNames.join(",") !== chineseNames.join(",")) {
            mismatches.push(`${key}: en [${englishNames.join(", ")}] vs zh [${chineseNames.join(", ")}]`);
        }
    }
    assert.deepEqual(mismatches, [], `placeholders differ:\n${mismatches.join("\n")}`);
});

test("no translation is empty or left as its own key", () => {
    const problems: string[] = [];
    for (const [language, entries] of [
        ["en-US", englishKeys],
        ["zh-CN", chineseKeys],
    ] as const) {
        for (const [key, value] of entries) {
            if (value.trim() === "") {
                problems.push(`${language}: ${key} is empty`);
                continue;
            }
            // A value equal to its own key path is the shape a hurried
            // placeholder takes, and it renders as a dotted identifier to the
            // user rather than as a sentence.
            if (value === key) {
                problems.push(`${language}: ${key} is its own key`);
            }
        }
    }
    assert.deepEqual(problems, [], `these entries are unusable:\n${problems.join("\n")}`);
});

test("the WP-06 studio keys are present in both locales", () => {
    // The import flow, the chapter panel and the story graph read these by
    // literal path, so a rename here without a rename there is a runtime
    // fallback rather than a compile error. Naming them makes that explicit.
    const required = [
        "studio.import.open",
        "studio.import.duplicateTitle",
        "studio.import.source.heading",
        "studio.chapters.confirm",
        "studio.chapters.next",
        "studio.storyGraph.entitiesTitle",
        "studio.storyGraph.evidenceRange",
        "studio.storyGraph.graphLabel",
        "studio.storyGraph.statusAny",
    ];
    for (const key of required) {
        assert.ok(englishKeys.has(key), `en-US is missing ${key}`);
        assert.ok(chineseKeys.has(key), `zh-CN is missing ${key}`);
    }
});
