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

/**
 * persistedConfigShape is what the store writes to browser storage.
 *
 * # Why it lives HERE rather than in the store
 *
 * AC-E2E-006 requires that DevTools cannot read a key out of the store. The store persists through
 * zustand's `persist`, which writes whatever `partialize` returns into localStorage — the first
 * place a person looks. So the clause is decided by the SHAPE that reaches storage.
 *
 * That shape's rule is stated in this module because this module is DEPENDENCY-FREE by design (see
 * the file header): the store itself imports `i18n`, which reads `localStorage` at module load, so a
 * test that imported the store could not run in the node test runner at all. A rule that can only be
 * checked in a browser is a rule that stops being checked, which is exactly how this gap arose.
 *
 * The store imports THIS function for its `partialize`, so the tested rule and the persisted shape
 * are the same code.
 *
 * The removal is `stripSecretsFromConfig`'s — blanked, not masked, for the same reason: a mask can
 * be mistaken for a usable value. The difference between the two is WHEN they run: stripping is for
 * a document leaving the application (an export, a backup), and this is for the browser writing
 * state. Both are needed, and a key must not survive either.
 *
 * The IN-MEMORY config keeps its key: a browser-mode build needs it to make a request through the
 * legacy path, and clearing it there would break that path rather than protect anything.
 */
export function persistedConfigShape(config: AiConfig): AiConfig {
    return stripSecretsFromConfig(config);
}
