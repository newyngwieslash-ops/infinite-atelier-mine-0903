/**
 * Provider-ID normalization shared by the secure secret UI and the desktop
 * client. Pure and dependency-free so it is unit-testable and so the rule has
 * exactly one definition.
 *
 * The Go side accepts `[a-z0-9-]{1,64}` for provider IDs; anything else would
 * be rejected when creating the credential-manager target. Normalizing here
 * keeps the UI honest instead of letting a save fail mysteriously.
 */
export function toProviderId(channelId: string): string {
    const normalized = (channelId || "")
        .toLowerCase()
        .replace(/[^a-z0-9-]/g, "-")
        .replace(/-+/g, "-")
        .replace(/^-|-$/g, "");
    return normalized.slice(0, 64) || "provider";
}
