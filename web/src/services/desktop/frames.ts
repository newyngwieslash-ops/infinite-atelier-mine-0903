import { readResultFile } from "@/services/desktop/jobs";

/**
 * frames.ts loads a shot's approved frame so it can be sent as a video's first or last frame.
 *
 * # Where the bytes come from, and why no new binding was added
 *
 * A timeline row carries `mediaHash` — the content-addressed key of the approved panel image — and
 * `ReadResultFile` already opens an object by exactly that key, validates the sixty-four-hexadecimal
 * shape and returns a data URL. A new binding for "give me this shot's frame" would be a SECOND answer
 * to a question the file store already answers, and the second answer is the one that drifts.
 *
 * # The absence direction
 *
 * A frame that cannot be loaded returns `null` rather than throwing. The caller's next step is not to
 * fail a submission: a shot whose panel image cannot be read is a shot a user CANNOT send a first frame
 * for, and the section's job is to say so and send no `firstFrame` at all — never an empty string,
 * which a provider reads as a zero-byte image. That distinction is the defect WP-28's probe found
 * inside the Go adapter, and this module is the other end of the same rule.
 */

/** maxFrameBytes mirrors the read path's own cap, for the UI's message rather than for enforcement. */
const maxFrameBytes = 64 * 1024 * 1024;

/** ShotFrame is an approved panel image ready to travel as a video's first or last frame. */
export type ShotFrame = {
    /** dataUrl carries the bytes in the shape `SubmitVideoJobRequest` accepts. */
    dataUrl: string;
    /** mime is the stored type, which the request's matching `…MIME` field carries. */
    mime: string;
    /** storageKey is the `mediaHash` the frame was loaded by, for a caller that wants to compare. */
    storageKey: string;
    size: number;
};

/**
 * loadShotFrame loads one approved frame by its content-addressed key.
 *
 * Returns `null` when the key is not one the store holds, when the core is absent, or when the object
 * is larger than the read path accepts. Every one of those is "this frame cannot be sent", which is a
 * state the caller renders rather than an error it reports.
 */
export async function loadShotFrame(mediaHash: string): Promise<ShotFrame | null> {
    const key = mediaHash.trim();
    // The shape is checked HERE as well as in the core, so an obviously unusable key never leaves the
    // browser: `readResultFile` makes the same check and returns null, and doing it first keeps a
    // malformed key from being reported as a read failure.
    if (!/^[0-9a-f]{64}$/.test(key)) {
        return null;
    }
    const content = await readResultFile(key);
    if (!content || !content.dataUrl) {
        return null;
    }
    if (content.size > maxFrameBytes) {
        // Refused rather than truncated. A truncated image is a frame the provider cannot decode, and
        // the failure would surface at the provider as a message about the IMAGE rather than about this
        // application's limit.
        return null;
    }
    return {
        dataUrl: content.dataUrl,
        // The core's own answer when it reported none, defaulted to PNG because that is what the panel
        // chain produces and what the request's MIME field will carry.
        mime: content.mime || "image/png",
        storageKey: key,
        size: content.size,
    };
}

/**
 * isFrameLoadingAvailable reports whether a frame can be loaded at all.
 *
 * It is the same question `readResultFile` answers, asked once so a section can disable the frame
 * controls and say why, rather than offering a picker whose every choice resolves to nothing.
 */
export function isFrameLoadingAvailable(): boolean {
    const jobs = (globalThis as { window?: { go?: { desktop?: Record<string, Record<string, unknown>> } } })
        .window?.go?.desktop?.JobsBinding;
    return typeof jobs?.ReadResultFile === "function";
}
