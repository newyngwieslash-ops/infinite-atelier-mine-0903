import type { health } from "@/wailsjs/go/models";

type WailsHealthBinding = {
    Get?: unknown;
};

type WailsGo = {
    desktop?: {
        HealthBinding?: WailsHealthBinding;
    };
};

type WailsRuntime = {
    EventsOnMultiple?: unknown;
};

type DesktopWindow = Window & {
    go?: WailsGo;
    runtime?: WailsRuntime;
};

type HealthChangedEvent = {
    version: 1;
    type: "health.changed";
};

type HealthBindingModule = typeof import("@/wailsjs/go/desktop/HealthBinding");
type RuntimeModule = typeof import("@/wailsjs/runtime/runtime");

let healthBindingModule: Promise<HealthBindingModule> | undefined;
let runtimeModule: Promise<RuntimeModule> | undefined;

function getDesktopWindow(): DesktopWindow | null {
    return typeof window === "undefined" ? null : window;
}

export function isDesktopHealthBindingAvailable(): boolean {
    const desktopWindow = getDesktopWindow();
    return typeof desktopWindow?.go?.desktop?.HealthBinding?.Get === "function";
}

export function isDesktopEventRuntimeAvailable(): boolean {
    const desktopWindow = getDesktopWindow();
    return typeof desktopWindow?.runtime?.EventsOnMultiple === "function";
}

function isHealthChangedEvent(event: unknown): event is HealthChangedEvent {
    if (typeof event !== "object" || event === null) return false;

    const candidate = event as Record<string, unknown>;
    return candidate.version === 1 && candidate.type === "health.changed";
}

function loadHealthBinding(): Promise<HealthBindingModule> {
    healthBindingModule ??= import("@/wailsjs/go/desktop/HealthBinding").catch((error: unknown) => {
        healthBindingModule = undefined;
        throw error;
    });
    return healthBindingModule;
}

function loadRuntime(): Promise<RuntimeModule> {
    runtimeModule ??= import("@/wailsjs/runtime/runtime").catch((error: unknown) => {
        runtimeModule = undefined;
        throw error;
    });
    return runtimeModule;
}

export async function getDesktopHealth(): Promise<health.Snapshot | null> {
    if (!isDesktopHealthBindingAvailable()) return null;

    const { Get } = await loadHealthBinding();
    if (!isDesktopHealthBindingAvailable()) return null;
    return Get();
}

export function onHealthChanged(callback: () => void): () => void {
    if (!isDesktopEventRuntimeAvailable()) return () => undefined;

    let cancelled = false;
    let cleanup: () => void = () => undefined;

    void loadRuntime()
        .then(({ EventsOn }) => {
            if (cancelled || !isDesktopEventRuntimeAvailable()) return;

            const unsubscribe = EventsOn("core:event", (event: unknown) => {
                if (isHealthChangedEvent(event)) callback();
            });
            cleanup = typeof unsubscribe === "function" ? unsubscribe : () => undefined;
        })
        .catch(() => undefined);

    return () => {
        cancelled = true;
        cleanup();
    };
}
