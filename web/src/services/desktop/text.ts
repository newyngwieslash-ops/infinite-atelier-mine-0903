import i18n from "@/i18n";

import type { providers as providerModels } from "@/wailsjs/go/models";
import { toSecureMessages, type SecureTextMessage } from "./messages";
import {
    cancelTextStream,
    isSecureProviderMode,
    listProviderConfigs,
    onTextStreamEvent,
    startTextStream,
    type StreamEventPayload,
} from "./providers";

export { toSecureMessages };
export type { SecureTextMessage };

export class SecureTextUnavailableError extends Error {
    constructor(message: string) {
        super(message);
        this.name = "SecureTextUnavailableError";
    }
}

/**
 * Resolves the provider that should serve a model selection.
 *
 * The selection carries its channel (`<channelId>::<model>`), so the matching
 * provider wins; otherwise the first enabled provider is used. When the Go
 * registry is still empty it is first synchronized from the channel list, so a
 * user who configured a channel before this feature existed still gets a
 * working secure path.
 */
async function selectProvider(modelSelection: string): Promise<providerModels.ConfigDTO | null> {
    let configs = await listProviderConfigs();
    if (configs.length === 0) {
        const { useConfigStore } = await import("@/stores/use-config-store");
        const { syncProviderConfigs } = await import("./provider-sync");
        await syncProviderConfigs(useConfigStore.getState().config).catch(() => undefined);
        configs = await listProviderConfigs();
    }
    if (configs.length === 0) return null;

    const { useConfigStore } = await import("@/stores/use-config-store");
    const { channelIdForModel } = await import("./model-selection");
    const { toProviderId } = await import("./provider-id");
    const wanted = channelIdForModel(useConfigStore.getState().config, modelSelection);
    if (wanted) {
        const providerId = toProviderId(wanted);
        const matched = configs.find((config) => config.id === providerId);
        if (matched) return matched;
    }
    return configs.find((config) => config.enabled) ?? configs[0] ?? null;
}

/**
 * Requests a streamed text completion through the Go Provider Gateway. The
 * caller's messages are forwarded verbatim; the API key never leaves Go.
 *
 * Failures are surfaced as errors. There is deliberately NO fallback to the
 * legacy browser path, raw query keys, or dynamic model scripts: a secure
 * failure must stay visible instead of silently degrading.
 */
export async function streamSecureText(
    messages: SecureTextMessage[],
    modelSelection: string,
    onDelta: (text: string) => void,
    signal?: AbortSignal,
): Promise<string> {
    if (!isSecureProviderMode()) {
        throw new SecureTextUnavailableError(i18n.t("secureText.unavailable"));
    }
    const { decodeModelSelection } = await import("./model-selection");
    // Providers expect the bare model name; the encoded channel prefix is
    // routing metadata only.
    const { model } = decodeModelSelection(modelSelection);
    const provider = await selectProvider(modelSelection);
    if (!provider) {
        throw new SecureTextUnavailableError(i18n.t("secureText.noProvider"));
    }

    const streamId = await startTextStream({
        providerId: provider.id,
        model,
        messages,
    } as unknown as Parameters<typeof startTextStream>[0]);

    return await new Promise<string>((resolve, reject) => {
        let content = "";
        let settled = false;
        let unsubscribe: () => void = () => undefined;

        const finish = (error?: Error, result?: string) => {
            if (settled) return;
            settled = true;
            unsubscribe();
            signal?.removeEventListener("abort", abortHandler);
            if (error) reject(error);
            else resolve(result ?? content);
        };

        const abortHandler = () => {
            void cancelTextStream(streamId).catch(() => undefined);
            const abortError = new DOMException("Aborted", "AbortError");
            finish(abortError);
        };

        unsubscribe = onTextStreamEvent((payload: StreamEventPayload) => {
            if (payload.streamId !== streamId) return;
            const event = payload.event ?? {};
            if (event.errorCode) {
                // Surface the safe category plus the diagnostic ID so a user
                // can quote it in a report; no provider text is shown.
                finish(
                    new SecureTextUnavailableError(
                        i18n.t("secureText.failed", { code: event.errorCode, diagnostic: event.diagnostic || i18n.t("secureText.diagnosticUnavailable") }),
                    ),
                );
                return;
            }
            if (typeof event.delta === "string" && event.delta) {
                if (event.done) {
                    content = event.delta;
                } else {
                    content += event.delta;
                }
                onDelta(content);
            }
            if (event.done) {
                finish(undefined, content);
            }
        });

        if (signal) {
            if (signal.aborted) {
                abortHandler();
                return;
            }
            signal.addEventListener("abort", abortHandler, { once: true });
        }
    });
}
