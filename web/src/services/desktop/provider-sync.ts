import { saveProviderConfig, type ProviderConfigInput } from "@/services/desktop/providers";
import type { AiConfig, ApiCallFormat, ModelChannel } from "@/stores/use-config-store";
import { toProviderId } from "@/services/desktop/provider-id";

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

/**
 * kindForApiFormat maps the drawer's protocol choice onto the provider kind
 * the Go registry routes by (RP-02.1).
 *
 * The previous wiring hardcoded `openai_compatible` for every channel, so a
 * Gemini channel's text requests were sent down the OpenAI chat path — the
 * defect RP-02.3's adapter exists for. An UNKNOWN format is REFUSED rather
 * than defaulted: a channel whose protocol the app does not speak must fail
 * at save time, loudly, instead of becoming an OpenAI-shaped provider that
 * silently mangles every call.
 */
export function kindForApiFormat(apiFormat: ApiCallFormat): ProviderConfigInput["kind"] {
    switch (apiFormat) {
        case "openai":
            return "openai_compatible";
        case "gemini":
            return "gemini_compatible";
        default:
            throw new Error(`unknown api format: ${String(apiFormat)}`);
    }
}

/** Builds the Go config input for one channel. */
export function toProviderConfigInput(channel: ModelChannel): ProviderConfigInput {
    const baseUrl = normalizeBaseUrl(channel.baseUrl);
    return {
        id: toProviderId(channel.id),
        kind: kindForApiFormat(channel.apiFormat),
        displayName: channel.name?.trim() || channel.id,
        baseUrl,
        localApprove: isLocalAddress(baseUrl),
        enabled: true,
        // Absent reads as 0, which the Go side reads as unlimited. The Math.max is what keeps a
        // channel saved before WP-14 — or a hand-edited store — from sending a negative or a NaN.
        maxConcurrency: Math.max(0, Math.trunc(channel.maxConcurrency || 0)),
        // RP-02.1: the per-minute rate limit rides along, and the SET flag is
        // always true from the drawer's save — the field is rendered whenever
        // a channel is edited, so the stated value (including an explicit 0)
        // is stored. An update that did not render the field would omit the
        // flag and keep the stored limit instead.
        rateLimitPerMinute: Math.max(0, Math.trunc(channel.rateLimitPerMinute ?? 0)),
        rateLimitPerMinuteSet: true,
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
 * One channel's sync outcome (RP-02.2): `registered` names a provider the Go
 * side CONFIRMED, `failed` names one whose save threw with its safe message,
 * and `skipped` names one with nothing to register (no base URL). The caller
 * can now surface a partial failure instead of swallowing it — the old
 * boolean-less list made "saved" and "silently dropped" indistinguishable.
 */
export type ProviderSyncOutcome = {
    registered: string[];
    failed: Array<{ id: string; reason: string }>;
    skipped: string[];
};

/**
 * Synchronizes every channel that has a valid base URL into the Go provider
 * registry, reporting EACH channel's outcome.
 *
 * A single bad channel must not break the rest, so failures are collected
 * rather than thrown — but they are REPORTED, not discarded: a save the user
 * believes succeeded while the backend refused it is the UI/backend divergence
 * RP-02.2 exists to remove. The skipped list keeps the no-base-URL case honest
 * too.
 */
export async function syncProviderConfigs(config: AiConfig): Promise<string[]> {
    const outcome = await syncProviderConfigsWithOutcome(config);
    return outcome.registered;
}

/**
 * The reporting variant `syncProviderConfigs` returns. Failures carry the
 * safe error message the binding threw — never a key, never a header.
 */
export async function syncProviderConfigsWithOutcome(
    config: AiConfig,
    saver: typeof saveProviderConfig = saveProviderConfig,
): Promise<ProviderSyncOutcome> {
    const outcome: ProviderSyncOutcome = { registered: [], failed: [], skipped: [] };
    for (const channel of config.channels || []) {
        const input = toProviderConfigInput(channel);
        if (!input.baseUrl) {
            outcome.skipped.push(input.id);
            continue;
        }
        try {
            // Register (or refresh) the non-secret metadata. Re-saving an
            // existing ID bumps its revision on the Go side rather than
            // creating a duplicate.
            await saver(input);
            outcome.registered.push(input.id);
        } catch (failure) {
            outcome.failed.push({
                id: input.id,
                reason: failure instanceof Error ? failure.message : String(failure),
            });
        }
    }
    return outcome;
}
