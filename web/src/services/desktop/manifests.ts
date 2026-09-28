import type { desktop } from "@/wailsjs/go/models";

/**
 * manifests.ts is RP-05.3's frontend service for the versioned manifest
 * surface: preview (validate without storing), list versions, save a
 * version, activate a version. All four are narrow commands over the Go
 * manifest service — the document never executes, and every refusal comes
 * from the domain's validation.
 */

let bindingModule: Promise<typeof import("@/wailsjs/go/desktop/ProvidersBinding")> | undefined;

function desktopBindings(): Record<string, unknown> | null {
    return (window as unknown as { go?: { desktop?: Record<string, unknown> } }).go?.desktop ?? null;
}

function isManifestSurfaceAvailable(): boolean {
    const bindings = desktopBindings() as {
        ProvidersBinding?: Record<string, unknown>;
    } | null;
    const providers = bindings?.ProvidersBinding;
    return (
        typeof providers?.PreviewManifest === "function" &&
        typeof providers?.SaveManifest === "function"
    );
}

export function manifestSurfaceError(): string {
    return "the manifest management surface is unavailable in this build";
}

async function loadBinding() {
    bindingModule ??= import("@/wailsjs/go/desktop/ProvidersBinding").catch((error: unknown) => {
        bindingModule = undefined;
        throw error;
    });
    return bindingModule;
}

export type ManifestPreview = desktop.ManifestPreviewDTO;

/** previewManifest validates a document WITHOUT storing it. */
export async function previewManifest(document: string): Promise<ManifestPreview> {
    if (!isManifestSurfaceAvailable()) throw new Error(manifestSurfaceError());
    const { PreviewManifest } = await loadBinding();
    return PreviewManifest(document);
}

/** listManifestVersions returns a config's stored versions, newest first. */
export async function listManifestVersions(providerConfigId: string): Promise<desktop.ManifestVersionDTO[]> {
    if (!isManifestSurfaceAvailable()) throw new Error(manifestSurfaceError());
    const { ListManifestVersions } = await loadBinding();
    return ListManifestVersions(providerConfigId);
}

/** saveManifest stores one version. It does NOT activate it. */
export async function saveManifest(request: desktop.SaveManifestRequest): Promise<desktop.ManifestVersionDTO> {
    if (!isManifestSurfaceAvailable()) throw new Error(manifestSurfaceError());
    const { SaveManifest } = await loadBinding();
    return SaveManifest(request);
}

/** activateManifest switches the active version under a revision check. */
export async function activateManifest(request: desktop.ActivateManifestRequest): Promise<void> {
    if (!isManifestSurfaceAvailable()) throw new Error(manifestSurfaceError());
    const { ActivateManifest } = await loadBinding();
    return ActivateManifest(request);
}
