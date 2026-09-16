import { saveProviderConfig, type ProviderConfigInput } from "@/services/desktop/providers";
import { toProviderId } from "@/services/desktop/provider-id";
import type { AiConfig, ModelChannel } from "@/stores/use-config-store";

/**
 * Keeps the Go-side provider registry in step with the channel list the user
 * already edits in the settings drawer.
 *
 * The drawer is the single place users configure a provider, so it is also the
 * place that must create the corresponding `provider_configs` row; otherwise a
 * stored secret would have no provider to attach to and the secure text path
 * would always report "no provider configured".
 *
 * Only non-secret fields are sent. The API key is written separately through
 * the secrets binding.
 */
export type { ProviderConfigInput };

/** Builds the Go config input for one channel. */
export function toProviderConfigInput(channel: ModelChannel): ProviderConfigInput {
    const baseUrl = normalizeBaseUrl(channel.baseUrl);
    return {
        id: toProviderId(channel.id),
        kind: "openai_compatible",
        displayName: channel.name?.trim() || channel.id,
        baseUrl,
        localApprove: isLocalAddress(baseUrl),
        enabled: true,
    };
}

function normalizeBaseUrl(baseUrl: string): string {
    const trimmed = (baseUrl || "").trim();
    // The Go validator rejects trailing slashes and requires a scheme.
    if (!trimmed) return trimmed;
    return trimmed.replace(/\/+$/, "");
}

/**
 * True when the URL targets a local/private host. Such a provider needs the
 * explicit local approval flag, which is exactly what the user is doing by
 * configuring it here.
 *
 * A plain `http:` scheme is NOT treated as local: the Go policy only accepts
 * an approved-local provider whose every resolved address is private, so
 * marking a public cleartext URL as local would produce a confusing
 * "blocked by policy" error instead of an honest HTTPS requirement.
 */
export function isLocalAddress(baseUrl: string): boolean {
    try {
        const url = new URL(baseUrl);
        const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, "");
        if (host === "localhost" || host.endsWith(".localhost")) return true;
        if (host === "::1" || host === "0:0:0:0:0:0:0:1") return true;
        if (host.startsWith("127.")) return true;
        if (host.startsWith("10.") || host.startsWith("192.168.")) return true;
        if (/^172\.(1[6-9]|2\d|3[01])\./.test(host)) return true;
        if (host.startsWith("169.254.")) return true;
        if (/^f[cd][0-9a-f]{2}:/.test(host)) return true;
        if (/^fe[89ab][0-9a-f]:/.test(host)) return true;
        return false;
    } catch {
        return false;
    }
}

/**
 * Synchronizes every enabled channel that has a valid base URL into the Go
 * provider registry. Returns the provider IDs that are now registered.
 *
 * A channel without a base URL cannot produce a reachable provider, so it is
 * skipped rather than failing the whole sync.
 */
export async function syncProviderConfigs(config: AiConfig): Promise<string[]> {
    const registered: string[] = [];
    for (const channel of config.channels || []) {
        const input = toProviderConfigInput(channel);
        if (!input.baseUrl) continue;
        try {
            // Register (or refresh) the non-secret metadata. Re-saving an
            // existing ID bumps its revision on the Go side rather than
            // creating a duplicate.
            await saveProviderConfig(input);
            registered.push(input.id);
        } catch {
            // A single bad channel must not break the rest.
            continue;
        }
    }
    return registered;
}
