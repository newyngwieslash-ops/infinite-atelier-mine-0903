import assert from "node:assert/strict";
import { test } from "node:test";
import { toProviderConfigInput } from "../desktop/provider-sync";
import type { ModelChannel } from "@/stores/use-config-store";

/**
 * RP-02.1's regression: the settings drawer's channels carry BOTH the
 * protocol kind the Go registry must route by and the per-minute rate limit
 * T07 persists — and a save must not silently clear a limit the user
 * configured earlier.
 */

function channel(overrides: Partial<ModelChannel> = {}): ModelChannel {
    return {
        id: "ch-1",
        name: "Test Channel",
        baseUrl: "https://api.example.com",
        apiKey: "",
        apiFormat: "openai",
        models: [],
        ...overrides,
    };
}

test("an openai channel maps to the openai_compatible kind", () => {
    const input = toProviderConfigInput(channel({ apiFormat: "openai" }));
    assert.equal(input.kind, "openai_compatible");
});

test("a gemini channel maps to the gemini_compatible kind, not openai", () => {
    const input = toProviderConfigInput(channel({ apiFormat: "gemini" }));
    assert.equal(input.kind, "gemini_compatible");
});

test("an unknown api format is refused rather than defaulted to openai", () => {
    assert.throws(() => toProviderConfigInput(channel({ apiFormat: "mystery" as never })), /unknown api format/i);
});

test("rateLimitPerMinute rides through the sync input", () => {
    const input = toProviderConfigInput(channel({ rateLimitPerMinute: 60 }));
    assert.equal(input.rateLimitPerMinute, 60);
});

test("an absent rate limit reads as zero (unlimited), not as a stale number", () => {
    const input = toProviderConfigInput(channel());
    assert.equal(input.rateLimitPerMinute, 0);
});

test("a negative or fractional rate limit is normalised to a whole non-negative number", () => {
    assert.equal(toProviderConfigInput(channel({ rateLimitPerMinute: -5 })).rateLimitPerMinute, 0);
    assert.equal(toProviderConfigInput(channel({ rateLimitPerMinute: 30.7 })).rateLimitPerMinute, 30);
});

test("maxConcurrency survives unchanged alongside the new field", () => {
    const input = toProviderConfigInput(channel({ maxConcurrency: 3, rateLimitPerMinute: 10 }));
    assert.equal(input.maxConcurrency, 3);
    assert.equal(input.rateLimitPerMinute, 10);
});

import { syncProviderConfigsWithOutcome } from "../desktop/provider-sync";
import type { ProviderConfigInput } from "../desktop/providers";

test("sync reports per-channel outcomes: registered, failed, skipped", async () => {
    const channels = [
        { ...channel(), id: "ok-1", baseUrl: "https://a.example.com" },
        { ...channel(), id: "bad-1", baseUrl: "https://b.example.com" },
        { ...channel(), id: "skip-1", baseUrl: "" },
    ];
    const calls: string[] = [];
    const saver = async (input: ProviderConfigInput) => {
        calls.push(input.id);
        if (input.id === "bad-1") throw new Error("the provider could not be saved");
        return null;
    };
    const outcome = await syncProviderConfigsWithOutcome({ channels } as never, saver);
    assert.deepEqual(outcome.registered, ["ok-1"]);
    assert.equal(outcome.failed.length, 1);
    assert.equal(outcome.failed[0].id, "bad-1");
    assert.match(outcome.failed[0].reason, /could not be saved/);
    assert.deepEqual(outcome.skipped, ["skip-1"]);
});
