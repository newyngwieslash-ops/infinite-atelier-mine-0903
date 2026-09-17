import {
    ImportProjects,
    ListImports,
    PrecheckImport,
} from "@/wailsjs/go/desktop/ProjectsBinding";
import {
    AbortLegacyFile,
    AppendLegacyFile,
    BeginLegacyFile,
    FinishLegacyFile,
} from "@/wailsjs/go/desktop/LegacyUploadBinding";
import { extractLegacySnapshot, readLegacyBlob, type LegacySnapshot } from "@/services/desktop/legacy-extract";

/**
 * The migration client: one import, driven from here through the chunked upload
 * channel and the import binding.
 *
 * ADR-0006 fixes the order this file must respect:
 *
 *   1. extract the browser state into a snapshot (legacy-extract.ts);
 *   2. hand every referenced blob to Go, in bounded chunks;
 *   3. ask Go to import the snapshot.
 *
 * The upload has to complete before the import, because the Go side resolves a
 * legacy key to its committed bytes and reports an absent one as missing media.
 * Uploading during the import would make the result depend on timing.
 */

/** One project's scope, as the precheck reports it. */
export type PrecheckProject = {
    legacyId: string;
    title: string;
    nodes: number;
    edges: number;
    /** True when a past import already completed for this project. */
    alreadyImported: boolean;
};

/** The precheck result, as the binding reports it. */
export type PrecheckResult = {
    manifestVersion: number;
    projects: PrecheckProject[];
    totalNodes: number;
    totalEdges: number;
    missingMedia: string[];
    warnings: MigrationWarning[];
    mediaFiles: number;
};

/** One reported migration case. */
export type MigrationWarning = {
    code: string;
    legacyId?: string;
    detail?: string;
    occurrences: number;
};

/** One project the import touched. */
export type ImportedProject = {
    legacyId: string;
    newId: string;
    title: string;
};

/** The import result, as the binding reports it. */
export type ImportResult = {
    importId: string;
    mode: string;
    imported: ImportedProject[];
    skipped: ImportedProject[];
    warnings: MigrationWarning[];
    counts: {
        importId: string;
        projects: number;
        nodes: number;
        edges: number;
        media: number;
        assets: number;
        history: number;
    };
};

/** One stored import run. */
export type ImportRecord = {
    id: string;
    fingerprint: string;
    sourceCase: string;
    mode: string;
    status: string;
    reportJson: string;
    warnings: MigrationWarning[] | null;
    legacyRoot: string;
    startedAt: string;
    finishedAt: string;
    createdAt: string;
};

/** Progress reported while a migration runs. */
export type MigrationProgress = {
    phase: "extract" | "upload" | "import";
    /** Completed units within the phase. */
    done: number;
    /** Total units in the phase, or 0 when unknown. */
    total: number;
    /** The key currently being handled, for a progress line. */
    detail?: string;
};

/**
 * Upload chunk size, in decoded bytes.
 *
 * It must not exceed the Go side's own ceiling (4 MiB): a larger chunk is
 * refused rather than split, which is the boundary working as designed.
 */
export const UPLOAD_CHUNK_BYTES = 4 * 1024 * 1024;

/**
 * precheck extracts the browser state and asks Go what it would do.
 *
 * Nothing is written: the precheck exists so the user sees the scope before
 * committing (AC-LEGACY-001's "预检" step), and so a project that was already
 * imported is reported before the user clicks.
 */
export async function precheckMigration(): Promise<{ snapshot: LegacySnapshot; result: PrecheckResult }> {
    const snapshot = await extractLegacySnapshot();
    const raw = await PrecheckImport(JSON.stringify(snapshot));
    return { snapshot: snapshot, result: JSON.parse(raw) as PrecheckResult };
}

/**
 * uploadSnapshotMedia hands every referenced blob to Go.
 *
 * A blob that cannot be read is skipped: the Go side records it as missing media
 * rather than failing the project, which is the behaviour AC-LEGACY-001 asks for
 * when a browser profile has lost a file.
 */
export async function uploadSnapshotMedia(snapshot: LegacySnapshot, onProgress?: (progress: MigrationProgress) => void): Promise<{ uploaded: number; skipped: number }> {
    let uploaded = 0;
    let skipped = 0;
    const total = snapshot.media.length;
    for (let index = 0; index < snapshot.media.length; index++) {
        const entry = snapshot.media[index];
        onProgress?.({ phase: "upload", done: index, total, detail: entry.legacyKey });
        const blob = await readLegacyBlob(entry.legacyKey);
        if (!blob) {
            skipped++;
            continue;
        }
        const uploadId = await BeginLegacyFile(entry.legacyKey, entry.mimeType || blob.type, blob.size);
        try {
            for (let offset = 0; offset < blob.size; offset += UPLOAD_CHUNK_BYTES) {
                const slice = blob.slice(offset, Math.min(offset + UPLOAD_CHUNK_BYTES, blob.size));
                const encoded = await blobToBase64(slice);
                await AppendLegacyFile(uploadId, encoded);
            }
            const accepted = await FinishLegacyFile(uploadId, "");
            if (accepted) {
                uploaded++;
            } else {
                skipped++;
            }
        } catch (error) {
            // The transfer is abandoned so its temporary file is removed; the key
            // is then reported as missing media rather than silently dropped.
            try {
                await AbortLegacyFile(uploadId);
            } catch {
                // Nothing further to do: the Go side cleans up an abandoned
                // transfer at the next start.
            }
            skipped++;
        }
    }
    onProgress?.({ phase: "upload", done: total, total });
    return { uploaded, skipped };
}

/**
 * runMigration performs one import.
 *
 * `mode` is "initial" or "copy": the first writes new rows and skips projects a
 * past import already completed, and the second deliberately imports again with
 * fresh identifiers. There is no overwrite mode (ADR-0006 §4).
 */
export async function runMigration(
    snapshot: LegacySnapshot,
    options: { mode?: "initial" | "copy"; sourceCase?: string; onProgress?: (progress: MigrationProgress) => void } = {},
): Promise<ImportResult> {
    options.onProgress?.({ phase: "upload", done: 0, total: snapshot.media.length });
    await uploadSnapshotMedia(snapshot, options.onProgress);
    options.onProgress?.({ phase: "import", done: 0, total: 1 });
    const raw = await ImportProjects({
        snapshotJson: JSON.stringify(snapshot),
        mode: options.mode ?? "initial",
        sourceCase: options.sourceCase ?? "browser",
        legacyRoot: "localForage",
    });
    options.onProgress?.({ phase: "import", done: 1, total: 1 });
    return JSON.parse(raw) as ImportResult;
}

/** listImportRecords returns recent runs newest first. */
export async function listImportRecords(limit = 20): Promise<ImportRecord[]> {
    const raw = await ListImports(limit);
    return JSON.parse(raw) as ImportRecord[];
}

/**
 * blobToBase64 encodes a slice for the binding.
 *
 * A Wails binding carries text, so binary travels as base64. FileReader is used
 * rather than a manual loop because it handles the whole slice in one pass and
 * reports a failure instead of producing a truncated string.
 */
function blobToBase64(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(reader.error ?? new Error("the file could not be read"));
        reader.onload = () => {
            const result = reader.result;
            if (typeof result !== "string") {
                reject(new Error("the file could not be read"));
                return;
            }
            // FileReader's data URL form is `data:<type>;base64,<payload>`; the
            // binding wants the payload alone.
            const separator = result.indexOf(",");
            resolve(separator >= 0 ? result.slice(separator + 1) : result);
        };
        reader.readAsDataURL(blob);
    });
}
