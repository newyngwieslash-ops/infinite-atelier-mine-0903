import type { desktop } from "@/wailsjs/go/models";

/**
 * app-settings.ts is RP-09.2's frontend service for the application settings
 * surface: the auto-backup configuration read at startup by the scheduler
 * and written by the settings panel under a revision guard.
 */

let bindingModule: Promise<typeof import("@/wailsjs/go/desktop/SettingsBinding")> | undefined;

function settingsBindingAvailable(): boolean {
    const bindings = (window as unknown as {
        go?: { desktop?: { SettingsBinding?: Record<string, unknown> } };
    }).go?.desktop ?? null;
    return typeof bindings?.SettingsBinding?.GetAutoBackup === "function";
}

async function loadBinding() {
    bindingModule ??= import("@/wailsjs/go/desktop/SettingsBinding").catch((error: unknown) => {
        bindingModule = undefined;
        throw error;
    });
    return bindingModule;
}

function unavailable(): Error {
    return new Error("the settings surface is unavailable in this build");
}

/** getAutoBackup reads the persisted settings or the documented defaults. */
export async function getAutoBackup(): Promise<desktop.AutoBackupSettings> {
    if (!settingsBindingAvailable()) throw unavailable();
    const { GetAutoBackup } = await loadBinding();
    return GetAutoBackup();
}

/** setAutoBackup validates and persists the settings under a revision guard. */
export async function setAutoBackup(settings: desktop.AutoBackupSettings, expectedRevision: number): Promise<void> {
    if (!settingsBindingAvailable()) throw unavailable();
    const { SetAutoBackup } = await loadBinding();
    return SetAutoBackup(settings, expectedRevision);
}
