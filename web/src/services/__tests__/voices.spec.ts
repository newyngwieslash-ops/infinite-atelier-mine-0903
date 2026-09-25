import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The voices client's absence and presence contracts.
 *
 * `voices.ts` keeps the split every desktop client in this repository keeps: a QUERY answers with an
 * empty result in a browser session, and a COMMAND throws. The split is behaviour rather than
 * formatting — the casting table renders an empty list while a button a user pressed says why nothing
 * happened — so it is asserted here rather than left to the e2e suite, which runs with no core.
 *
 * The tests are about the RESULT and the THROW rather than about which method was called: a client
 * that reached the binding would fail the absence half in a browser session, which is the point.
 */
import {
    assignCharacterVoice,
    clearCharacterVoice,
    effectVocabulary,
    isVoiceSurfaceAvailable,
    listCharacterVoices,
    resolveCharacterVoice,
    suggestShotEffects,
} from "../desktop/voices";
import { withDesktopCore, type FakeBindings } from "./desktop-core";

test("a window with no desktop core reports the casting surface unavailable", () => {
    assert.equal(isVoiceSurfaceAvailable(), false);
});

test("the casting reads answer empty rather than throwing when the core is absent", async () => {
    // The two READS. The section's first act is to list, and an exception here would render an error
    // about a machine the interface never asked.
    assert.deepEqual(await listCharacterVoices("project-1"), []);
    assert.deepEqual(await suggestShotEffects([{ shotId: "shot-1", ordinal: 1, audioIntent: "雨声" }]), []);
    assert.deepEqual(await effectVocabulary(), []);
});

test("a resolve with no core answers from the caller's own configuration", async () => {
    // This one is NOT an empty answer, and that is deliberate: a build without the drama store can
    // still render speech with the voice the user chose in settings, which is what the audio section
    // did before casting existed. Answering "unset" here would remove a working capability from a
    // degraded build rather than add one.
    const fromSettings = await resolveCharacterVoice({
        projectId: "project-1",
        characterEntityId: "char-1",
        projectVoice: "alloy",
        projectModel: "tts-1",
        projectProvider: "chan-1",
    });
    assert.equal(fromSettings.voice, "alloy");
    assert.equal(fromSettings.source, "project");
    assert.equal(fromSettings.isSet, true);
    // And with nothing configured it says so, rather than inventing a voice.
    const unset = await resolveCharacterVoice({ projectId: "project-1", characterEntityId: "char-1" });
    assert.equal(unset.isSet, false);
    assert.equal(unset.source, "unset");
    assert.equal(unset.voice, "");
});

test("the casting commands throw rather than reporting a success that did nothing", async () => {
    // A command is a user's act. Reporting success for an assignment nothing stored is the failure
    // this asserts against, and it is why the commands do not follow the reads' empty answer.
    await assert.rejects(() =>
        assignCharacterVoice({ projectId: "p", characterEntityId: "c", voice: "alloy" }),
    );
    await assert.rejects(() => clearCharacterVoice({ projectId: "p", characterEntityId: "c" }));
});

test("the casting surface reads through the binding when a core is present", async () => {
    const calls: string[] = [];
    const bindings: FakeBindings = {
        MediaBinding: {
            ListCharacterVoices: ((projectId: string) => {
                calls.push(`list:${projectId}`);
                return [];
            }) as never,
            ResolveCharacterVoice: ((request: { characterEntityId: string }) => {
                calls.push(`resolve:${request.characterEntityId}`);
                return { voice: "nova", source: "character", isSet: true };
            }) as never,
            SuggestShotEffects: ((shots: unknown[]) => {
                calls.push(`suggest:${shots.length}`);
                return [];
            }) as never,
        },
    };
    await withDesktopCore(bindings, async () => {
        assert.equal(isVoiceSurfaceAvailable(), true);
        await listCharacterVoices("project-9");
        await resolveCharacterVoice({ projectId: "project-9", characterEntityId: "char-9" });
        await suggestShotEffects([
            { shotId: "a", ordinal: 1, audioIntent: "雨" },
            { shotId: "b", ordinal: 2, audioIntent: "脚步" },
        ]);
    });
    // The calls went through, with the arguments the caller gave: a client that swallowed the request
    // and answered a constant would pass the assertions above without these.
    assert.deepEqual(calls, ["list:project-9", "resolve:char-9", "suggest:2"]);
});

test("the availability probe answers false for a build whose binding lacks the methods", async () => {
    // A build whose media binding predates WP-27: the binding object exists, so a probe that only
    // checked for it would offer a casting table that cannot read anything.
    await withDesktopCore({ MediaBinding: { MediaCapability: (() => ({})) as never } }, async () => {
        assert.equal(isVoiceSurfaceAvailable(), false);
    });
});
