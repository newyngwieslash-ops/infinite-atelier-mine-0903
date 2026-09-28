import type { desktop, provider, providers, secrets } from "@/wailsjs/go/models";

type WailsBinding = Record<string, unknown>;

type WailsGo = {
    desktop?: {
        HealthBinding?: WailsBinding;
        SecretsBinding?: WailsBinding;
        ProvidersBinding?: WailsBinding;
    };
};

type WailsRuntime = {
    EventsOn?: unknown;
    EventsOnMultiple?: unknown;
};

type DesktopWindow = Window & { go?: WailsGo; runtime?: WailsRuntime };

function getDesktopWindow(): DesktopWindow | null {
    return typeof window === "undefined" ? null : window;
}

/**
 * Returns true when the Wails provider/secret bindings are available. The
 * bindings expose status, set/replace/delete, config metadata, and text
 * request/cancel only. There is deliberately no resolve/secret-read method.
 */
export function isDesktopProviderBindingsAvailable(): boolean {
    const desktop = getDesktopWindow()?.go?.desktop;
    return (
        typeof desktop?.SecretsBinding?.Status === "function" &&
        typeof desktop?.SecretsBinding?.Set === "function" &&
        typeof desktop?.SecretsBinding?.Delete === "function" &&
        typeof desktop?.ProvidersBinding?.ListConfigs === "function" &&
        typeof desktop?.ProvidersBinding?.SaveConfig === "function" &&
        typeof desktop?.ProvidersBinding?.GenerateText === "function" &&
        typeof desktop?.ProvidersBinding?.StreamText === "function" &&
        typeof desktop?.ProvidersBinding?.CancelStream === "function"
    );
}

/**
 * Secure mode is on whenever the desktop provider bindings exist. In secure
 * mode the legacy browser key handling (query-string keys, dynamic model
 * scripts, direct Provider calls with raw keys) must be disabled rather than
 * silently falling back.
 */
export function isSecureProviderMode(): boolean {
    return isDesktopProviderBindingsAvailable();
}

let secretsModule: Promise<typeof import("@/wailsjs/go/desktop/SecretsBinding")> | undefined;
let providersModule: Promise<typeof import("@/wailsjs/go/desktop/ProvidersBinding")> | undefined;

async function loadSecretsBinding() {
    secretsModule ??= import("@/wailsjs/go/desktop/SecretsBinding").catch((error: unknown) => {
        secretsModule = undefined;
        throw error;
    });
    return secretsModule;
}

async function loadProvidersBinding() {
    providersModule ??= import("@/wailsjs/go/desktop/ProvidersBinding").catch((error: unknown) => {
        providersModule = undefined;
        throw error;
    });
    return providersModule;
}

export async function getSecretStatus(providerId: string): Promise<secrets.Status | null> {
    if (!isDesktopProviderBindingsAvailable()) return null;
    const { Status } = await loadSecretsBinding();
    return Status(providerId);
}

export async function setSecret(providerId: string, value: string): Promise<void> {
    if (!isDesktopProviderBindingsAvailable()) throw new Error("desktop provider bindings are unavailable");
    const { Set } = await loadSecretsBinding();
    await Set({ providerId, value } as desktop.SetSecretRequest);
}

export async function deleteSecret(providerId: string): Promise<void> {
    if (!isDesktopProviderBindingsAvailable()) throw new Error("desktop provider bindings are unavailable");
    const { Delete } = await loadSecretsBinding();
    await Delete(providerId);
}

export async function listProviderConfigs(): Promise<providers.ConfigDTO[]> {
    if (!isDesktopProviderBindingsAvailable()) return [];
    const { ListConfigs } = await loadProvidersBinding();
    return ListConfigs();
}

/**
 * Non-secret provider configuration input, mirroring the Go
 * `desktop.ProviderConfigRequest` DTO. It deliberately has no key field: the
 * secret is written through the separate secrets binding.
 */
export type ProviderConfigInput = {
    id: string;
    /**
     * kind is the protocol family the Go registry routes by (RP-02.1). The
     * Gemini kind exists since the registry's own vocabulary does; a frontend
     * that only ever sent `openai_compatible` sent Gemini traffic down the
     * OpenAI path.
     */
    kind: "openai_compatible" | "gemini_compatible";
    displayName: string;
    baseUrl: string;
    localApprove: boolean;
    enabled: boolean;
    /**
     * maxConcurrency is the provider's job ceiling, where ZERO MEANS UNLIMITED (ADR-0018).
     */
    maxConcurrency: number;
    /**
     * rateLimitPerMinute is the provider's requests-per-rolling-minute ceiling
     * (T07), where ZERO MEANS UNLIMITED. RP-02.1: carried on the save so a
     * configured limit reaches the registry instead of being cleared.
     */
    rateLimitPerMinute: number;
    /**
     * rateLimitPerMinuteSet distinguishes a stated limit (true — including an
     * explicit 0) from an omitted one (false — keep the stored value).
     * RP-02.1.
     */
    rateLimitPerMinuteSet: boolean;
};

export async function saveProviderConfig(request: ProviderConfigInput): Promise<providers.ConfigDTO | null> {
    if (!isDesktopProviderBindingsAvailable()) return null;
    const { SaveConfig } = await loadProvidersBinding();
    return SaveConfig(request as unknown as desktop.ProviderConfigRequest);
}

export async function deleteProviderConfig(id: string): Promise<void> {
    if (!isDesktopProviderBindingsAvailable()) return;
    const { DeleteConfig } = await loadProvidersBinding();
    await DeleteConfig(id);
}

export async function checkProviderHealth(providerId: string): Promise<provider.HealthState | null> {
    if (!isDesktopProviderBindingsAvailable()) return null;
    const { CheckHealth } = await loadProvidersBinding();
    return CheckHealth(providerId);
}

export async function generateText(request: desktop.TextRequestDTO): Promise<providers.TextResult> {
    if (!isDesktopProviderBindingsAvailable()) throw new Error("desktop provider bindings are unavailable");
    const { GenerateText } = await loadProvidersBinding();
    return GenerateText(request);
}

export async function startTextStream(request: desktop.TextRequestDTO): Promise<string> {
    if (!isDesktopProviderBindingsAvailable()) throw new Error("desktop provider bindings are unavailable");
    const { StreamText } = await loadProvidersBinding();
    return StreamText(request);
}

export async function cancelTextStream(streamId: string): Promise<void> {
    if (!isDesktopProviderBindingsAvailable()) return;
    const { CancelStream } = await loadProvidersBinding();
    await CancelStream(streamId);
}

export type StreamEventPayload = {
    streamId: string;
    event: {
        delta?: string;
        done?: boolean;
        errorCode?: string;
        /** Correlation ID safe to show the user; carries no message content. */
        diagnostic?: string;
        retriable?: boolean;
    };
};

/**
 * Subscribes to provider text stream events. Returns an unsubscribe function.
 * The caller owns cleanup on unmount.
 */
export function onTextStreamEvent(callback: (payload: StreamEventPayload) => void): () => void {
    const runtime = getDesktopWindow()?.runtime as { EventsOn?: unknown } | undefined;
    if (typeof runtime?.EventsOn !== "function") return () => undefined;

    let cancelled = false;
    let cleanup: () => void = () => undefined;

    void import("@/wailsjs/runtime/runtime")
        .then(({ EventsOn }) => {
            if (cancelled) return;
            const unsubscribe = EventsOn("core:event", (event: unknown) => {
                if (typeof event !== "object" || event === null) return;
                const candidate = event as Record<string, unknown>;
                if (candidate.type !== "provider:text_stream") return;
                const payload = candidate.payload as StreamEventPayload | undefined;
                if (!payload || typeof payload.streamId !== "string") return;
                callback(payload);
            });
            cleanup = typeof unsubscribe === "function" ? unsubscribe : () => undefined;
        })
        .catch(() => undefined);

    return () => {
        cancelled = true;
        cleanup();
    };
}
