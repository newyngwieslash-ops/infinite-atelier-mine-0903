import assert from "node:assert/strict";
import { test } from "node:test";

import { configContainsSecrets, stripSecretsFromConfig } from "../config-secrets";
import { toProviderId } from "../desktop/provider-id";
import { toSecureMessages } from "../desktop/messages";
import { channelIdForModel, decodeModelSelection } from "../desktop/model-selection";
import { isLocalAddress, toProviderConfigInput } from "../desktop/provider-sync";
import { hasLegacyPlaintextKeys, legacyKeyLocations } from "../desktop/legacy-config";

type Channel = { id: string; name: string; baseUrl: string; apiKey: string; apiFormat: "openai" | "gemini"; models: Array<{ name: string; capability: string }> };
type Config = {
    channelMode: "remote" | "local";
    baseUrl: string;
    apiKey: string;
    apiFormat: "openai" | "gemini";
    channels: Channel[];
    model: string;
    [key: string]: unknown;
};

function makeConfig(overrides: Partial<Config> = {}): Config {
    return {
        channelMode: "local",
        baseUrl: "https://api.example.com",
        apiKey: "sk-root-secret-0001",
        apiFormat: "openai",
        channels: [
            { id: "default", name: "Default", baseUrl: "https://api.example.com", apiKey: "sk-channel-secret-0002", apiFormat: "openai", models: [{ name: "gpt-x", capability: "text" }] },
            { id: "second", name: "Second", baseUrl: "https://relay.example.com", apiKey: "", apiFormat: "openai", models: [] },
        ],
        model: "default::gpt-x",
        ...overrides,
    };
}

test("stripSecretsFromConfig removes root and channel keys", () => {
    const stripped = stripSecretsFromConfig(makeConfig() as never) as unknown as Config;
    assert.equal(stripped.apiKey, "");
    for (const channel of stripped.channels) {
        assert.equal(channel.apiKey, "");
    }
});

test("stripSecretsFromConfig output contains no secret substring", () => {
    const stripped = stripSecretsFromConfig(makeConfig() as never);
    const serialized = JSON.stringify(stripped);
    assert.ok(!serialized.includes("sk-root-secret-0001"), "root key leaked into export");
    assert.ok(!serialized.includes("sk-channel-secret-0002"), "channel key leaked into export");
});

test("stripSecretsFromConfig preserves non-secret fields", () => {
    const original = makeConfig();
    const stripped = stripSecretsFromConfig(original as never) as unknown as Config;
    assert.equal(stripped.baseUrl, original.baseUrl);
    assert.equal(stripped.model, original.model);
    assert.equal(stripped.channels.length, original.channels.length);
    assert.equal(stripped.channels[0].name, "Default");
    assert.equal(stripped.channels[0].baseUrl, "https://api.example.com");
    assert.deepEqual(stripped.channels[0].models, original.channels[0].models);
});

test("stripSecretsFromConfig does not mutate the input config", () => {
    const original = makeConfig();
    stripSecretsFromConfig(original as never);
    assert.equal(original.apiKey, "sk-root-secret-0001");
    assert.equal(original.channels[0].apiKey, "sk-channel-secret-0002");
});

test("configContainsSecrets detects root and channel keys", () => {
    assert.equal(configContainsSecrets(makeConfig() as never), true);
    assert.equal(configContainsSecrets(makeConfig({ apiKey: "" }) as never), true);
    assert.equal(configContainsSecrets(makeConfig({ apiKey: "", channels: [] }) as never), false);
});

test("configContainsSecrets ignores whitespace-only keys", () => {
    assert.equal(configContainsSecrets(makeConfig({ apiKey: "   ", channels: [{ id: "a", name: "a", baseUrl: "https://a.example.com", apiKey: " ", apiFormat: "openai", models: [] }] }) as never), false);
});

test("toProviderId normalizes channel IDs to the Go-side shape", () => {
    assert.equal(toProviderId("default"), "default");
    assert.equal(toProviderId("ABC_def"), "abc-def");
    assert.equal(toProviderId("a b c"), "a-b-c");
    assert.equal(toProviderId("--leading--"), "leading");
    assert.equal(toProviderId(""), "provider");
    assert.ok(toProviderId("x".repeat(100)).length <= 64);
});

test("toProviderId rejects characters that could forge a credential target", () => {
    const forged = toProviderId("evil:InfiniteAtelier:provider:other");
    assert.ok(!forged.includes(":"), `colon survived normalization: ${forged}`);
    assert.match(forged, /^[a-z0-9-]+$/);
});

test("toSecureMessages flattens multimodal parts without leaking image data", () => {
    const messages = toSecureMessages([
        { role: "system", content: "be brief" },
        { role: "user", content: "hello" },
        {
            role: "user",
            content: [
                { type: "text", text: "describe this" },
                { type: "image_url", image_url: { url: "data:image/png;base64,AAAABBBBCCCC" } },
            ],
        },
    ]);
    assert.equal(messages[0].content, "be brief");
    assert.equal(messages[1].content, "hello");
    assert.ok(messages[2].content.includes("describe this"));
    assert.ok(!messages[2].content.includes("AAAABBBBCCCC"), "raw image data must not be forwarded");
});

test("toSecureMessages keeps plain string content untouched", () => {
    const messages = toSecureMessages([{ role: "user", content: "plain text" }]);
    assert.equal(messages[0].content, "plain text");
});

test("decodeModelSelection strips the channel prefix before the provider call", () => {
    assert.deepEqual(decodeModelSelection("default::gpt-5.5"), { channelId: "default", model: "gpt-5.5" });
    assert.deepEqual(decodeModelSelection("relay-a::custom/model-1"), { channelId: "relay-a", model: "custom/model-1" });
    // A bare model name has no channel and must pass through unchanged.
    assert.deepEqual(decodeModelSelection("gpt-4o-mini"), { channelId: "", model: "gpt-4o-mini" });
    assert.deepEqual(decodeModelSelection(""), { channelId: "", model: "" });
});

test("channelIdForModel prefers the encoded channel and falls back to a model match", () => {
    const config = {
        channels: [
            { id: "chan-a", models: [{ name: "model-a" }] },
            { id: "chan-b", models: [{ name: "model-b" }] },
        ],
    };
    assert.equal(channelIdForModel(config, "chan-b::model-b"), "chan-b");
    assert.equal(channelIdForModel(config, "model-a"), "chan-a");
    assert.equal(channelIdForModel(config, "unknown-model"), "");
    assert.equal(channelIdForModel({ channels: [] }, "x::y"), "x");
});

test("isLocalAddress only classifies private/loopback hosts as local", () => {
    const local = ["http://127.0.0.1:11434/v1", "https://localhost/v1", "http://192.168.1.10:8000/v1", "http://10.1.2.3/v1", "http://172.16.0.5/v1", "http://[::1]:8080/v1"];
    for (const url of local) {
        assert.equal(isLocalAddress(url), true, `${url} should be local`);
    }
    const remote = ["https://api.openai.com", "http://public.example.com/v1", "https://relay.example.com/v1", "not a url"];
    for (const url of remote) {
        assert.equal(isLocalAddress(url), false, `${url} must not be treated as local`);
    }
});

test("toProviderConfigInput produces the Go-side non-secret shape", () => {
    const input = toProviderConfigInput({
        id: "Relay A",
        name: "Relay A",
        baseUrl: "https://relay.example.com/v1/",
        apiKey: "sk-should-not-be-sent-0003",
        apiFormat: "openai",
        models: [],
    } as never);
    assert.equal(input.id, "relay-a");
    assert.equal(input.kind, "openai_compatible");
    assert.equal(input.baseUrl, "https://relay.example.com/v1");
    assert.equal(input.enabled, true);
    // The input type has no key field, and the serialized value must not carry it.
    assert.ok(!JSON.stringify(input).includes("sk-"), "provider config input must not contain a key");
});

test("legacyKeyLocations reports plaintext keys without exposing them", () => {
    const locations = legacyKeyLocations(makeConfig() as never);
    assert.equal(locations.rootKey, true);
    assert.equal(locations.channelCount, 1);
    assert.equal(hasLegacyPlaintextKeys(makeConfig() as never), true);
    assert.equal(hasLegacyPlaintextKeys(makeConfig({ apiKey: "", channels: [] }) as never), false);
});
