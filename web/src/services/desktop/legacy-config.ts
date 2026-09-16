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
