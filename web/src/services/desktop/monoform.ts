import type { desktop } from "@/wailsjs/go/models";

/**
 * The previs snapshot client: the one place a thumbnail from the embedded studio reaches the core.
 *
 * ## Why this file exists
 *
 * FR-060's acceptance says 「保存后可在 Shot 中看到摄像机参数和预览图」. The camera half worked; the
 * PREVIEW half had no path at all — `shot_updated` carried a `thumbnail?: Blob` that
 * `monoform-bridge.ts` validated and `director-panel.tsx` forwarded, and the handler in
 * `director-view.tsx` narrowed it away with a parameter type that named the shot and the camera and
 * nothing else. No component mentioned a thumbnail, because no binding could store one.
 *
 * ## Why the transfer is chunked and base64
 *
 * The same reason the document upload is: a Wails binding carries text, so binary crosses as base64,
 * and a whole image in one message would be materialised on the webview's main thread — work
 * proportional to the image rather than to a chunk. The constants come from the CORE
 * (`ChunkBytes` in the begin reply) rather than being chosen here, so a client cannot ask for the
 * whole snapshot in one message even by accident.
 *
 * ## Why the failure is returned rather than thrown away
 *
 * A snapshot that stored but did not attach is one the user will not find in the shot, so the caller
 * has to know. The functions below therefore PROPAGATE the failure, and the caller decides what to
 * tell the user — the division the store's own clients keep: a query returns empty, a command throws.
 */

/** The methods this client needs, so availability is checked against reality rather than assumed. */
const REQUIRED_MONOFORM_METHODS: (keyof MonoformBindingShape)[] = [
    "BeginSnapshotUpload",
    "AppendSnapshotChunk",
    "FinishSnapshotUpload",
];

/** isMonoformSnapshotAvailable reports whether the core can accept a snapshot. */
export function isMonoformSnapshotAvailable(): boolean {
    const monoform = getMonoformBinding();
    if (!monoform) return false;
    return REQUIRED_MONOFORM_METHODS.every((method) => typeof monoform[method] === "function");
}

/**
 * getMonoformBinding reads the generated binding off the desktop window.
 *
 * It is read lazily rather than at module load, because the module is imported in the browser build
 * too and a top-level read of `window.go` would throw there.
 */
type MonoformBindingShape = {
    BeginSnapshotUpload: (request: unknown) => Promise<{ uploadId: string; chunkBytes: number }>;
    AppendSnapshotChunk: (request: unknown) => Promise<void>;
    FinishSnapshotUpload: (request: unknown) => Promise<{ fileHash: string; storageKey: string; bytes: number }>;
    AbortSnapshotUpload: (uploadId: string) => Promise<void>;
};

function getMonoformBinding(): MonoformBindingShape | null {
    if (typeof window === "undefined") return null;
    const go = (window as { go?: { desktop?: { MonoformBinding?: MonoformBindingShape } } }).go;
    return go?.desktop?.MonoformBinding ?? null;
}

function unavailableError(): Error {
    return new Error("The desktop core is not available, so a previs snapshot cannot be stored.");
}

/**
 * blobToBase64 encodes one slice for the binding.
 *
 * FileReader is used rather than a manual loop because it is the platform's own base64 path and it
 * does the work off the main thread for a large slice. The data-URL prefix is stripped, because the
 * binding decodes standard base64 and a `data:` header would be a malformed chunk.
 */
function blobToBase64(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(new Error("The snapshot could not be read."));
        reader.onload = () => {
            const result = typeof reader.result === "string" ? reader.result : "";
            const comma = result.indexOf(",");
            if (comma < 0) {
                reject(new Error("The snapshot could not be encoded."));
                return;
            }
            resolve(result.slice(comma + 1));
        };
        reader.readAsDataURL(blob);
    });
}

/** SnapshotRef is where a stored snapshot lives, which is what the shot's overrides record. */
export type SnapshotRef = {
    fileHash: string;
    storageKey: string;
    bytes: number;
};

/**
 * storePrevisSnapshot sends a previs image to the core and links it to an asset version.
 *
 * # The two failure points, and why neither is swallowed
 *
 * The transfer can fail (a refused type, an oversized image, a store error) and the LINK can fail
 * separately. The core reports them as one call's outcome because a snapshot that is not linked is
 * not attached to anything — so this returns the FIRST failure it meets and the caller reports it.
 * What it does NOT do is continue as though the snapshot had been saved: a silent failure here is
 * exactly the shape of the defect this file exists to fix.
 */
export async function storePrevisSnapshot(versionId: string, image: Blob): Promise<SnapshotRef> {
    const binding = getMonoformBinding();
    if (!binding) throw unavailableError();
    // The type the STUDIO produced, which the core checks against its own closed set. An empty type
    // is sent as PNG because that is what a canvas produces by default, and the core refusing it
    // would be a refusal about a field the studio did not fill in rather than about the image.
    const mimeType = image.type && image.type !== "" ? image.type : "image/png";
    const begun = await binding.BeginSnapshotUpload({ versionId, mimeType, totalBytes: image.size });
    if (!begun || !begun.uploadId) {
        throw new Error("The core did not open a snapshot upload.");
    }
    if (!Number.isFinite(begun.chunkBytes) || begun.chunkBytes <= 0) {
        throw new Error("The core reported no chunk size for the snapshot upload.");
    }
    try {
        for (let offset = 0; offset < image.size; offset += begun.chunkBytes) {
            const slice = image.slice(offset, Math.min(offset + begun.chunkBytes, image.size));
            const encoded = await blobToBase64(slice);
            await binding.AppendSnapshotChunk({ uploadId: begun.uploadId, chunk: encoded });
        }
        const finished = await binding.FinishSnapshotUpload({
            uploadId: begun.uploadId,
            displayName: "previs snapshot",
        });
        return { fileHash: finished.fileHash, storageKey: finished.storageKey, bytes: finished.bytes };
    } catch (failure) {
        // The transfer is abandoned on any failure so the core does not hold a half-arrived image.
        // The abort's own outcome is deliberately NOT propagated: the caller needs the ORIGINAL
        // failure, and reporting "the abort also failed" would replace the actionable message with a
        // cleanup detail.
        try {
            await binding.AbortSnapshotUpload(begun.uploadId);
        } catch {
            // Nothing to do: the core drops a stale transfer on its own, and the caller is about to
            // be told about the failure that matters.
        }
        throw failure;
    }
}

/**
 * readSnapshotDataURL reads a stored snapshot back for display.
 *
 * # Why this goes through the JOB result reader
 *
 * `ReadResultFile` is the surface that reads a content-addressed object by its storage key, and it
 * already returns a DATA URL — so nothing is re-encoded here, and a second decoder would be a second
 * place the encoding lives. The method is named for job results because that is what first needed it;
 * the key it takes is any stored object's, which is why a snapshot can use it without a new binding.
 *
 * # Why a failure returns null rather than throwing
 *
 * A snapshot that cannot be read is a cosmetic loss: the shot still has its camera parameters, and the
 * panel renders them without the picture. Throwing would turn a missing preview into a failed save,
 * which is the wrong trade for a user who got what they asked for.
 */
export async function readSnapshotDataURL(storageKey: string): Promise<string | null> {
    if (typeof window === "undefined") return null;
    const go = (window as {
        go?: { desktop?: { JobsBinding?: { ReadResultFile?: (key: string) => Promise<desktop.JobResultFileContent> } } };
    }).go;
    const read = go?.desktop?.JobsBinding?.ReadResultFile;
    if (typeof read !== "function") return null;
    if (!/^[0-9a-f]{64}$/.test(storageKey)) {
        // The reader validates the key's shape before touching the store, and so does this: a key that
        // is not a content address names nothing, and asking would be a call that cannot succeed.
        return null;
    }
    try {
        const content = await read(storageKey);
        return content?.dataUrl ?? null;
    } catch {
        return null;
    }
}
