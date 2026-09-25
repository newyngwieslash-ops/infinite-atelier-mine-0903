import type { desktop } from "@/wailsjs/go/models";

/**
 * music.ts is the client for FR-080's 背景音乐导入: a user's own track, carried into the library.
 *
 * # Why the transfer is chunked
 *
 * A Wails binding carries text, so binary crosses as base64. Sending a 40 MB track as one message would
 * materialise it twice — once in the browser and once in the Go decoder — on the webview's main thread,
 * which is the same problem the document import and the previs snapshot both solve by beginning an
 * upload, appending bounded chunks and finishing with the size verified. This module follows that shape
 * rather than inventing a third one.
 *
 * # The division of failure
 *
 * Every function THROWS, because every one of them is a step in a command a user started. A half-finished
 * import that reported success would leave a track the core had not stored, and the user would hear
 * nothing while the library showed a row. The caller reports the first failure it meets; this module
 * never continues past one.
 */

/** The methods this client needs, so availability is checked against reality rather than assumed. */
const REQUIRED_MUSIC_METHODS = ["BeginMusicImport", "AppendMusicChunk", "FinishMusicImport"] as const;

type MusicBindingShape = {
    BeginMusicImport: (request: unknown) => Promise<{ uploadId: string; chunkBytes: number }>;
    AppendMusicChunk: (request: unknown) => Promise<void>;
    FinishMusicImport: (request: unknown) => Promise<desktop.FinishMusicImportResult>;
    AbortMusicImport: (uploadId: string) => Promise<void>;
};

/**
 * getMusicBinding reads the generated binding off the desktop window.
 *
 * It is read lazily rather than at module load, because this module is imported in the browser build too
 * and a top-level read of `window.go` would throw there.
 */
function getMusicBinding(): MusicBindingShape | null {
    if (typeof window === "undefined") return null;
    const go = (window as { go?: { desktop?: { MusicImportBinding?: MusicBindingShape } } }).go;
    return go?.desktop?.MusicImportBinding ?? null;
}

function unavailableError(): Error {
    return new Error("The desktop core is not available, so a music file cannot be imported.");
}

/** isMusicImportAvailable reports whether this build can accept a music file at all. */
export function isMusicImportAvailable(): boolean {
    const binding = getMusicBinding();
    if (!binding) return false;
    return REQUIRED_MUSIC_METHODS.every((method) => typeof binding[method] === "function");
}

/**
 * blobToBase64 encodes one slice for the binding.
 *
 * FileReader is used rather than a manual loop because it is the platform's own base64 path and does the
 * work off the main thread for a large slice. The data-URL prefix is stripped, because the binding
 * decodes standard base64 and a `data:` header would be a malformed chunk.
 */
function blobToBase64(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(new Error("The music file could not be read."));
        reader.onload = () => {
            const result = typeof reader.result === "string" ? reader.result : "";
            const comma = result.indexOf(",");
            if (comma < 0) {
                reject(new Error("The music file could not be encoded."));
                return;
            }
            resolve(result.slice(comma + 1));
        };
        reader.readAsDataURL(blob);
    });
}

/** ImportedBed is what a completed import produced. */
export type ImportedBed = {
    assetId: string;
    versionId: string;
    fileHash: string;
    bytes: number;
    mimeType: string;
};

/**
 * importBackgroundMusic carries a user's file into the library as an approved bed.
 *
 * `shotId` is REQUIRED and is what makes the import audible: the mix joins its audio on the shot, so a
 * usage recorded against anything else is a row no read finds. The caller passes the episode's first
 * shot — the choice names which mix carries the bed, not when it begins, because a bed starts at zero
 * wherever it was attached.
 *
 * The type the FILE reports is deliberately NOT sent: the core checks the type its own sniffer decides,
 * so a file renamed to `.mp3` is stored as what it actually is. Sending the browser's claim would be a
 * second answer to a question the store already answers.
 */
export async function importBackgroundMusic(shotId: string, projectId: string, file: File): Promise<ImportedBed> {
    const binding = getMusicBinding();
    if (!binding) throw unavailableError();
    if (!shotId || !shotId.trim()) {
        // Refused here as well as in the core, because the failure it prevents — a bed attached to
        // nothing — is silent: the import would succeed and the music would never play.
        throw new Error("A music file must be attached to a shot of the episode it belongs to.");
    }
    if (!file || file.size === 0) {
        throw new Error("That file is empty.");
    }
    const begun = await binding.BeginMusicImport({
        projectId,
        displayName: file.name || "background-music",
        totalBytes: file.size,
        shotId,
    });
    if (!begun || !begun.uploadId) {
        throw new Error("The core did not open a music upload.");
    }
    if (!Number.isFinite(begun.chunkBytes) || begun.chunkBytes <= 0) {
        throw new Error("The core reported no chunk size for the music upload.");
    }
    try {
        for (let offset = 0; offset < file.size; offset += begun.chunkBytes) {
            const slice = file.slice(offset, Math.min(offset + begun.chunkBytes, file.size));
            const encoded = await blobToBase64(slice);
            await binding.AppendMusicChunk({ uploadId: begun.uploadId, data: encoded });
        }
        const finished = await binding.FinishMusicImport({
            uploadId: begun.uploadId,
            name: file.name || "background-music",
        });
        if (!finished || !finished.versionId) {
            throw new Error("The core stored no music version.");
        }
        return {
            assetId: finished.assetId,
            versionId: finished.versionId,
            fileHash: finished.fileHash,
            bytes: finished.bytes,
            mimeType: finished.mimeType,
        };
    } catch (failure) {
        // The transfer is abandoned on ANY failure, and the abort is best-effort: a core that has already
        // forgotten the upload is the state the abort was asking for. Swallowing the abort's own failure
        // is deliberate — the caller needs to hear why the IMPORT failed, not that the cleanup also did.
        try {
            await binding.AbortMusicImport(begun.uploadId);
        } catch {
            // Nothing to add: seen above.
        }
        throw failure;
    }
}
