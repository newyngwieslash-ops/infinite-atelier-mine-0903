import localforage from "localforage";

import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import type { Asset } from "@/stores/use-asset-store";
import type { GenerationHistoryRecord } from "@/stores/canvas/use-generation-history-store";

/**
 * The legacy snapshot the Go importer consumes (ADR-0006 §1).
 *
 * This module is the *extractor*: it reads the browser stores and produces the
 * envelope. It deliberately performs no transformation — no renaming, no
 * dropping, no normalising — because ADR-0006 puts every conversion in Go. That
 * split is what makes the import reproducible: the same browser state always
 * produces the same envelope, and the shape the legacy store had stays auditable
 * in the archive.
 */

export const SUPPORTED_MANIFEST_VERSION = 1;

/** A blob the importer should expect, with the key it is stored under. */
export type LegacyMediaEntry = {
    legacyKey: string;
    mimeType: string;
    bytes: number;
};

export type LegacySnapshot = {
    app: "infinite-canvas";
    manifestVersion: number;
    exportedAt: string;
    projects: CanvasProject[];
    assets: Asset[];
    generationHistory: GenerationHistoryRecord[];
    promptLibrary: { builtInCovers: Record<string, string> };
    uiPreferences: Record<string, string>;
    media: LegacyMediaEntry[];
    unsupported: Record<string, unknown>;
};

/** Storage layout the legacy application used. */
const LEGACY_DATABASE = "infinite-canvas";
const LEGACY_APP_STATE = "app_state";
const LEGACY_IMAGE_STORE = "image_files";
const LEGACY_MEDIA_STORE = "media_files";

const CANVAS_STORE_KEY = "infinite-canvas:canvas_store";
const ASSET_STORE_KEY = "infinite-canvas:asset_store";
const HISTORY_STORE_KEY = "infinite-canvas:generation_history";
const PROMPT_LIBRARY_KEY = "infinite-canvas:prompt_library_store";

/**
 * readRawStore reads one persisted Zustand store out of localForage.
 *
 * The value is the persist middleware's envelope (`{state, version}`), so the
 * `state` field is returned rather than the whole record. A missing or malformed
 * row yields the fallback: a browser profile that never used one of these
 * features is a normal case, not an error.
 */
async function readRawStore<T>(key: string, field: string, fallback: T): Promise<T> {
    const store = localforage.createInstance({ name: LEGACY_DATABASE, storeName: LEGACY_APP_STATE });
    try {
        const value = await store.getItem<string>(key);
        if (!value) return fallback;
        const parsed = JSON.parse(value) as { state?: Record<string, unknown> };
        const state = parsed?.state;
        if (!state || typeof state !== "object") return fallback;
        const extracted = (state as Record<string, unknown>)[field];
        return (extracted as T) ?? fallback;
    } catch {
        // A corrupt row is reported as "nothing to migrate" for that store. The
        // import report names what was found, so a user can tell the difference
        // between an empty feature and an unreadable one.
        return fallback;
    }
}

/**
 * listStoredKeys lists the keys in one blob store.
 *
 * localForage's iterate() strips the instance's own prefix, so the keys are the
 * `${prefix}:${id}` values the application assigned.
 */
async function listStoredKeys(storeName: string): Promise<string[]> {
    const store = localforage.createInstance({ name: LEGACY_DATABASE, storeName });
    const keys: string[] = [];
    await store.iterate((_value, key) => {
        keys.push(key);
    });
    return keys;
}

/** describeSize reports one blob's length without holding it in memory. */
async function describeBlob(storeName: string, key: string): Promise<LegacyMediaEntry | null> {
    const store = localforage.createInstance({ name: LEGACY_DATABASE, storeName });
    const blob = await store.getItem<Blob>(key);
    if (!blob) return null;
    return { legacyKey: key, mimeType: blob.type || "application/octet-stream", bytes: blob.size };
}

/**
 * mediaKeyPattern matches a legacy storage key.
 *
 * The legacy application builds a key as `${prefix}:${nanoid()}` with the
 * prefixes below, and the distinction matters: a text node's `content` is its
 * prose, while an image node's `content` is a key. Requiring the known prefix
 * shape is what separates the two, so a sentence is never mistaken for a file
 * the migration should upload.
 */
const mediaKeyPattern = /^(image|video|audio|director|file):[A-Za-z0-9_-]+$/;

/**
 * collectMediaKeys finds every storage key the given projects reference.
 *
 * The authority is the project document itself, not the blob store's contents:
 * an orphaned blob is not part of any project, and the legacy garbage collector
 * already treats it as removable. The same key can appear in a node, an image
 * list, a chat reference and an asset, so the result is deduplicated.
 */
export function collectMediaKeys(projects: CanvasProject[], assets: Asset[]): Set<string> {
    const keys = new Set<string>();
    const consider = (value: unknown) => {
        if (typeof value !== "string") return;
        const trimmed = value.trim();
        if (trimmed === "" || !mediaKeyPattern.test(trimmed)) return;
        keys.add(trimmed);
    };
    for (const project of projects) {
        for (const node of project.nodes ?? []) {
            const metadata = (node.metadata ?? {}) as Record<string, unknown>;
            consider(metadata.content);
            consider(metadata.storageKey);
            consider(metadata.coverUrl);
            const references = metadata.references;
            if (Array.isArray(references)) references.forEach(consider);
            const images = metadata.images;
            if (Array.isArray(images)) {
                for (const image of images) {
                    if (image && typeof image === "object") {
                        consider((image as Record<string, unknown>).storageKey);
                        consider((image as Record<string, unknown>).content);
                    }
                }
            }
        }
        for (const session of project.chatSessions ?? []) {
            for (const message of session.messages ?? []) {
                const references = message.references;
                if (Array.isArray(references)) {
                    for (const reference of references) {
                        if (reference && typeof reference === "object") {
                            consider((reference as Record<string, unknown>).storageKey);
                        }
                    }
                }
            }
        }
    }
    for (const asset of assets) {
        const data = (asset.data ?? {}) as unknown as Record<string, unknown>;
        consider(data.storageKey);
        consider(data.url);
        consider(data.dataUrl);
        consider(asset.coverUrl);
    }
    return keys;
}

/**
 * readUIPreferences reads the raw localStorage values the legacy app set.
 *
 * These are display preferences (a panel width, the locale, the palette), so
 * they are reported rather than imported: WP-04 does not move UI state into the
 * domain, and ROADMAP item 10 keeps it in the frontend.
 */
export function readUIPreferences(): Record<string, string> {
    if (typeof window === "undefined" || !window.localStorage) return {};
    const preferences: Record<string, string> = {};
    for (const key of ["canvas-side-panel-width", "canvas-side-panel-open", "canvas-image-quick-tools-v7", "infinite-canvas:locale", "infinite-canvas:home_palette"]) {
        try {
            const value = window.localStorage.getItem(key);
            if (value !== null) preferences[key] = value;
        } catch {
            // Storage can be disabled or full; a missing preference is not fatal.
        }
    }
    return preferences;
}

/**
 * extractLegacySnapshot reads everything the importer needs.
 *
 * The order matters for the envelope's own consistency: the projects are read
 * first, because the media list is derived from what they reference.
 */
export async function extractLegacySnapshot(): Promise<LegacySnapshot> {
    const projects = await readRawStore<CanvasProject[]>(CANVAS_STORE_KEY, "projects", []);
    const assets = await readRawStore<Asset[]>(ASSET_STORE_KEY, "assets", []);
    const generationHistory = await readRawStore<GenerationHistoryRecord[]>(HISTORY_STORE_KEY, "records", []);
    const promptLibrary = await readRawStore<{ builtInCovers: Record<string, string> }>(PROMPT_LIBRARY_KEY, "builtInCovers", { builtInCovers: {} });

    const wanted = collectMediaKeys(projects, assets);
    const media: LegacyMediaEntry[] = [];
    const unsupported: Record<string, unknown> = {};

    // Each key is looked up in the store its prefix belongs to: an `image:` key
    // lives in image_files and everything else in media_files, which is the
    // routing the legacy application itself used.
    for (const key of wanted) {
        const storeName = key.startsWith("image:") ? LEGACY_IMAGE_STORE : LEGACY_MEDIA_STORE;
        const entry = await describeBlob(storeName, key);
        if (entry) {
            media.push(entry);
            continue;
        }
        // A referenced key with no blob is listed so the importer can report it
        // rather than discovering a node with no content.
        media.push({ legacyKey: key, mimeType: "", bytes: 0 });
    }

    // MONOFORM keeps its own scene data in a separate application's storage,
    // keyed by canvas node id. WP-04 records that it exists rather than
    // importing a schema this repository does not own.
    const directorNodeIds = projects.flatMap((project) => (project.nodes ?? []).filter((node) => node.type === "director").map((node) => node.id));
    if (directorNodeIds.length > 0) {
        unsupported.monoform = {
            nodeIds: directorNodeIds,
            note: "MONOFORM scene data lives in the embedded application's own storage and was not imported.",
        };
    }

    return {
        app: "infinite-canvas",
        manifestVersion: SUPPORTED_MANIFEST_VERSION,
        exportedAt: new Date().toISOString(),
        projects,
        assets,
        generationHistory,
        promptLibrary: { builtInCovers: promptLibrary?.builtInCovers ?? {} },
        uiPreferences: readUIPreferences(),
        media,
        unsupported,
    };
}

/**
 * readLegacyBlob reads one blob for the chunked upload.
 *
 * The bytes are read here, in the browser, because the Go core cannot reach
 * IndexedDB; they cross the binding in bounded chunks (ADR-0006 §2).
 */
export async function readLegacyBlob(legacyKey: string): Promise<Blob | null> {
    const storeName = legacyKey.startsWith("image:") ? LEGACY_IMAGE_STORE : LEGACY_MEDIA_STORE;
    const store = localforage.createInstance({ name: LEGACY_DATABASE, storeName });
    const blob = await store.getItem<Blob>(legacyKey);
    return blob ?? null;
}

/** legacyStoreKeys reports the blob keys a store holds, for diagnostics. */
export async function legacyStoreKeys(storeName: "image" | "media"): Promise<string[]> {
    return listStoredKeys(storeName === "image" ? LEGACY_IMAGE_STORE : LEGACY_MEDIA_STORE);
}
