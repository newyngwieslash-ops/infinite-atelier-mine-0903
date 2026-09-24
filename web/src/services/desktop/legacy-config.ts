import type { AiConfig } from "@/stores/use-config-store";

/**
 * Detects legacy plaintext keys still present in a persisted frontend config.
 *
 * WP-02 does not silently delete user data: an existing install may hold a raw
 * key under `infinite-canvas:ai_config_store`. This module reports that state
 * so the UI can warn the user to migrate the key into the OS credential store,
 * without the key ever being read back into a component.
 */
export function legacyKeyLocations(config: AiConfig): { rootKey: boolean; channelCount: number } {
    const rootKey = (config.apiKey || "").trim() !== "";
    const channelCount = (config.channels || []).filter((channel) => (channel.apiKey || "").trim() !== "").length;
    return { rootKey, channelCount };
}

/** True when the persisted config still carries any legacy plaintext key. */
export function hasLegacyPlaintextKeys(config: AiConfig): boolean {
    const locations = legacyKeyLocations(config);
    return locations.rootKey || locations.channelCount > 0;
}

/**
 * clearLegacyPlaintextKeys removes the plaintext keys a persisted config still carries.
 *
 * # Why a REMOVAL is now the right thing, when this module's header says it is not
 *
 * The header is about the automatic path: silently deleting a key whose value has not been migrated
 * would lose a user's credential with nothing to replace it. That reasoning still holds and this
 * function is not called automatically.
 *
 * What changed is that the USER-DRIVEN path this module's header names — "re-enter through the
 * secure field, then clear" — had no second half. `LegacySecretNotice` told a user their key was
 * still in browser storage and offered nothing to do about it, so the state it reported was
 * permanent. FR-140's requirement that the frontend never be able to read a raw key cannot be
 * satisfied while a plaintext copy is readable in DevTools, and the migration path was documented
 * as the answer. This is that path's last step, called by the user from the notice.
 *
 * # What it does NOT do
 *
 * It does not read the key. The value is discarded rather than copied, because it has already been
 * re-entered through the secure field (that is the precondition the caller enforces by only offering
 * the control in secure mode) and reading it here would put it back in the component that the whole
 * design keeps it out of.
 *
 * It does not touch anything but the key fields: the provider, its endpoint and its models stay, so
 * clearing a leaked credential does not cost a user their configuration.
 */
export function clearLegacyPlaintextKeys(config: AiConfig): AiConfig {
    return {
        ...config,
        apiKey: "",
        channels: (config.channels || []).map((channel) => ({ ...channel, apiKey: "" })),
    };
}
