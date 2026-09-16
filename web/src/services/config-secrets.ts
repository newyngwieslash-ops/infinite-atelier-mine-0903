import type { AiConfig, ModelChannel } from "@/stores/use-config-store";

/**
 * Secret stripping for ordinary exports, imports, and backups.
 *
 * PRD fixed decision 13 and SECURITY.md §4.4 require that ordinary backups and
 * config exports never contain API keys. This module is intentionally free of
 * runtime dependencies so it can be unit-tested without a browser or a
 * bundler.
 *
 * The secret is removed, not masked: a mask could be mistaken for a usable
 * value. Importing an encrypted sensitive backup is a separate, explicitly
 * enabled feature and must not reuse the ordinary export path.
 */
export function stripSecretsFromConfig(config: AiConfig): AiConfig {
    return {
        ...config,
        apiKey: "",
        channels: (config.channels || []).map((channel) => stripChannelSecrets(channel)),
    };
}

function stripChannelSecrets(channel: ModelChannel): ModelChannel {
    return { ...channel, apiKey: "" };
}

/**
 * Reports whether a config still carries raw secrets. Callers use this to warn
 * that a legacy config was sanitized during export or import.
 */
export function configContainsSecrets(config: AiConfig): boolean {
    if ((config.apiKey || "").trim()) return true;
    return (config.channels || []).some((channel) => (channel.apiKey || "").trim() !== "");
}
