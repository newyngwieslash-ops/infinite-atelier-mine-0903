/**
 * The MONOFORM bridge: the one place a message crosses between the embedded previs studio
 * and this application.
 *
 * ## Why this file exists
 *
 * ARCHITECTURE §17 and SECURITY §12 require every cross-iframe message to be validated for
 * its source, its type and its schema, and the state WP-09 found was the opposite of that:
 * MONOFORM posted `{source: 'monoform', type: 'export', kind, blob}` to `'*'`, and the host
 * accepted any message whose `source` field said "monoform" — a string ANY frame in the page
 * can set. There was no schema version, no nonce, no origin check and no size bound.
 *
 * A `source` field is a claim, not an identity: `event.origin` is the browser's own record of
 * which document sent the message, and it is the only part of a message a sender cannot
 * forge. So the validation here is built on `event.origin` plus a NONCE the host minted and
 * handed to that one iframe, and the `source` field is kept as a cheap sanity check rather
 * than as the check.
 *
 * ## The envelope
 *
 * Both directions carry `{ source, schemaVersion, nonce, type, ... }`:
 *
 * - `source` is `"monoform"` from the studio and `"atelier"` from this host, so a message
 *   that came back to its own sender is ignorable.
 * - `schemaVersion` is checked for EXACT equality. A peer that speaks a different version is
 *   refused rather than parsed optimistically — the alternative is reading fields a future
 *   version renamed, which is how a bridge silently misinterprets a payload.
 * - `nonce` is per-mount, so a message replayed from a previous panel instance is refused.
 *
 * ## What crosses
 *
 * - `open_shot` (host → studio): the shot to previsualise, with its framing and camera.
 * - `shot_updated` (studio → host): the camera parameters and a thumbnail, which the host
 *   writes back to the shot.
 * - `export` (studio → host): a rendered still or clip, which becomes a canvas node.
 *
 * The `export` direction is the one that existed; `open_shot` and `shot_updated` are what
 * make FR-060's "从 Shot/StoryboardPanel 打开预演" and "保存后可在 Shot 中看到摄像机参数" real.
 */

/**
 * The envelope version both sides speak. A mismatch is refused, never negotiated.
 *
 * # Why WP-21 did NOT move it to 2
 *
 * The package added three fields — `camera.movement`, `camera.notes` and
 * `shot.sceneReferenceAssetVersionIds` — and the temptation was to call that a new version. It is
 * not, and the difference matters because the check is EXACT EQUALITY: a bump would refuse every
 * message from a studio build that had not changed, and the studio shipped in `web/public/monoform`
 * is a separate artifact this repository does not rebuild on our schedule.
 *
 * A version moves when a change would make an OLD peer misread a message. Every field added here has
 * the same property, stated in both directions:
 *
 *  - a host receiving them from a newer studio reads them where it understands them and ignores them
 *    where it does not, because they are optional;
 *  - a studio receiving an `open_shot` with no scene references sees exactly the message it saw
 *    before, because it was optional there too.
 *
 * What WOULD move it: renaming a field, changing a type, making an optional field required, or
 * redefining what one means. Those are the changes an old peer reads WRONGLY rather than partially,
 * and the exact-equality check is what refuses them.
 */
export const MONOFORM_SCHEMA_VERSION = 1;

/** What the studio's messages are marked with. */
export const MONOFORM_SOURCE = "monoform";

/** What this host's messages are marked with, so a studio can ignore its own echo. */
export const ATELIER_SOURCE = "atelier";

/**
 * The largest payload the bridge will accept, in bytes.
 *
 * A `shot_updated` message carries a thumbnail and an `export` carries a whole frame, so the
 * bound is generous. What it stops is a frame sending a multi-megabyte body to stall the
 * host's main thread — which is a denial of service the origin check cannot prevent, because
 * the sender IS the frame we trust.
 */
export const MONOFORM_MAX_PAYLOAD_BYTES = 8 * 1024 * 1024;

/**
 * A shot's camera parameters, in the STUDIO's own shape.
 *
 * The shape is the studio's rather than one invented here, and that is deliberate: the bridge
 * carries a camera the studio produced and hands it back to the studio, so a translation
 * layer would be a second definition of the same thing — and the two would drift. What this
 * type adds is a VALIDATION of that shape, which is what a boundary owes.
 */
export type MonoformCamera = {
    /** The camera's world position, in metres. */
    position: [number, number, number];
    /** Its rotation in degrees, as the studio's own controls express it. */
    rotation: [number, number, number];
    /** Millimetres, the way a lens is specified. */
    focalLength: number;
    /** The frame's shape, such as "16:9". */
    aspectRatio: string;
    /**
     * How the camera moves, in the studio's own words, when it says.
     *
     * ARCHITECTURE §17 lists Movement among what the bridge returns, and the studio's current build
     * does not send it — so it is OPTIONAL and the host accepts its absence. Adding a required field
     * would refuse every message the studio sends today, which is the opposite of what the protocol
     * is for.
     */
    movement?: string;
    /**
     * The previs notes a director wrote, when the studio carries them.
     *
     * Optional for the same reason as `movement`, and NOT the same field as a shot's
     * `performanceNote`: this is what the director said while framing the shot, and it belongs with
     * the camera the framing produced.
     */
    notes?: string;
};

/** What the host tells the studio to open. */
export type MonoformOpenShot = {
    shotId: string;
    /** Empty when the shot has no number yet, which a draft board allows. */
    shotNumber: string;
    shotSize: string;
    cameraAngle: string;
    cameraMovement: string;
    durationSeconds: number;
    visualDescription: string;
    firstFrameDescription: string;
    lastFrameDescription: string;
    videoMotionDescription: string;
    /** The camera the shot already recorded, when it has one. */
    camera?: MonoformCamera;
    /**
     * The approved scene references this shot's scene is built from, by asset version id.
     *
     * FR-060's 「发送角色站位、相机、镜头参数和场景参考」 names the scene reference as something the
     * studio RECEIVES, and before this field the message carried none: the shot's framing and its
     * camera went over and nothing about where it happens. The ids are REFERENCES rather than bytes,
     * which is ARCHITECTURE §17's 「只接收 Shot、资产引用和已批准参数」 — a studio that wanted to draw
     * the location would ask for it by id rather than be handed a file.
     *
     * Ids rather than a whole asset: a shot's scene may cite several, and the studio decides which it
     * can use.
     */
    sceneReferenceAssetVersionIds?: string[];
};

/** A message the studio sends back. */
export type MonoformStudioMessage =
    | { kind: "shot_updated"; shotId: string; camera: MonoformCamera; thumbnail?: Blob }
    | { kind: "export"; exportKind: "image" | "video"; blob: Blob };

/** A message this host sends to the studio. */
export type MonoformHostMessage = { kind: "open_shot"; shot: MonoformOpenShot };

/** Why a message was refused. Each reason is a distinct situation a reader can act on. */
export type MonoformRejection =
    | "not-an-object"
    | "wrong-source"
    | "wrong-version"
    | "wrong-nonce"
    | "wrong-origin"
    | "unknown-type"
    | "malformed-payload"
    | "too-large";

/** The outcome of validating one inbound message. */
export type MonoformValidation =
    | { ok: true; message: MonoformStudioMessage }
    | { ok: false; reason: MonoformRejection };

/**
 * A nonce for one panel mount.
 *
 * Two sources, because the browser decides: `crypto.randomUUID` where it exists, and
 * `getRandomValues` otherwise. The fallback is not a weak random — it is the same CSPRNG —
 * and the value is only ever compared for equality, never used as a secret.
 */
export function createMonoformNonce(): string {
    const cryptoRef = typeof globalThis !== "undefined" ? globalThis.crypto : undefined;
    if (cryptoRef && typeof cryptoRef.randomUUID === "function") {
        return cryptoRef.randomUUID();
    }
    if (cryptoRef && typeof cryptoRef.getRandomValues === "function") {
        const bytes = new Uint8Array(16);
        cryptoRef.getRandomValues(bytes);
        return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
    }
    // A build with no crypto is one where the nonce cannot be unpredictable, and a
    // PREDICTABLE nonce is worse than none: it would read as a protection while accepting a
    // replay. So this is a REFUSAL at the call site rather than a fallback to a counter.
    throw new Error("MONOFORM requires a cryptographic random source for its nonce.");
}

/**
 * Whether an origin is one this host will accept a studio message from.
 *
 * The studio is served from the same origin as the application — it is a build artefact under
 * `web/public/monoform/` — so the allowed origin is the window's own. That is checked rather
 * than assumed: a development server on another port, or a page that embedded this one, would
 * otherwise be accepted.
 *
 * `about:srcdoc` and `null` appear as origins for a frame with no origin of its own, and both
 * are REFUSED: a message from them cannot be attributed to the document this host embedded.
 */
export function isAllowedMonoformOrigin(origin: string, selfOrigin: string): boolean {
    const candidate = (origin || "").trim();
    if (candidate === "" || candidate === "null") {
        return false;
    }
    return candidate === selfOrigin;
}

/** A triple of finite numbers, which is what the studio's vectors are. */
function vectorFrom(value: unknown): [number, number, number] | null {
    if (!Array.isArray(value) || value.length !== 3) {
        return null;
    }
    const out: [number, number, number] = [0, 0, 0];
    for (let index = 0; index < 3; index += 1) {
        const component = value[index];
        if (typeof component !== "number" || !Number.isFinite(component)) {
            return null;
        }
        out[index] = component;
    }
    return out;
}

/** A value is a plausible camera when every component is a finite number in a usable range. */
function cameraFrom(value: unknown): MonoformCamera | null {
    if (!value || typeof value !== "object") {
        return null;
    }
    const source = value as Record<string, unknown>;
    const position = vectorFrom(source.position);
    const rotation = vectorFrom(source.rotation);
    if (!position || !rotation) {
        return null;
    }
    const focalLength = source.focalLength;
    if (typeof focalLength !== "number" || !Number.isFinite(focalLength)) {
        return null;
    }
    // A focal length outside the range a perspective camera can be built with is a camera
    // nothing can be rendered from, and it is refused rather than clamped: clamping would
    // silently change what the user set, and they would see a different shot from the one
    // they composed.
    if (focalLength < 1 || focalLength > 2000) {
        return null;
    }
    const aspectRatio = typeof source.aspectRatio === "string" ? source.aspectRatio.trim() : "";
    if (!aspectRatio || aspectRatio.length > 20) {
        return null;
    }
    // THE TWO OPTIONAL FIELDS ARE CARRIED, NOT DROPPED.
    //
    // This function returns a FRESH object built from named fields, which is what makes it a
    // validation rather than a cast — and it is also how a field gets silently lost. `movement` and
    // `notes` were added in WP-21 for ARCHITECTURE §17's "返回 Camera Pose、Lens、Framing、Movement、
    // Snapshot、Notes", and a validator that rebuilt the object without them would accept a message
    // carrying them and hand the host one without — which is the same narrowing that dropped the
    // thumbnail on the Go side and left FR-060's 预览图 clause unbuilt.
    const movement = optionalShortString(source.movement, 200);
    const notes = optionalShortString(source.notes, 2000);
    if (movement === null || notes === null) {
        // Present but wrong: a non-string, or longer than the bound. Refused rather than dropped,
        // because a message carrying a 40-kilobyte "movement" is not one this host will reinterpret.
        return null;
    }
    return { position, rotation, focalLength, aspectRatio, movement, notes };
}

/**
 * optionalShortString reads a field that may be absent, empty, or a bounded string.
 *
 * It returns undefined for absent-or-empty, a trimmed string for a valid one, and NULL for a value
 * that is present and wrong — the distinction the caller needs to refuse a malformed field rather
 * than silently ignore it. `undefined` and `null` mean different things here and that is the whole
 * point of the function.
 */
function optionalShortString(value: unknown, max: number): string | undefined | null {
    if (value === undefined || value === null) {
        return undefined;
    }
    if (typeof value !== "string") {
        return null;
    }
    const trimmed = value.trim();
    if (trimmed === "") {
        return undefined;
    }
    if (trimmed.length > max) {
        return null;
    }
    return trimmed;
}

/** A value is a blob when it has the shape one has and is within the payload bound. */
function blobFrom(value: unknown, limit: number): Blob | null {
    if (!value || typeof value !== "object" || typeof (value as Blob).size !== "number") {
        return null;
    }
    const blob = value as Blob;
    if (blob.size <= 0 || blob.size > limit) {
        return null;
    }
    return blob;
}

/**
 * Validate one inbound message.
 *
 * THE ORDER MATTERS. Origin and nonce are checked before anything in the payload is read, so a
 * message from an unexpected frame is refused without its contents being examined at all —
 * which is what SECURITY §12 means by validating the source rather than the claim.
 *
 * `event.origin` is passed in rather than read from the event so this function is a pure
 * function of its inputs, which is what lets the tests exercise every rejection without a
 * browser.
 */
export function validateMonoformMessage(input: {
    data: unknown;
    origin: string;
    selfOrigin: string;
    nonce: string;
    limitBytes?: number;
}): MonoformValidation {
    const limit = input.limitBytes ?? MONOFORM_MAX_PAYLOAD_BYTES;
    if (!input.data || typeof input.data !== "object" || Array.isArray(input.data)) {
        return { ok: false, reason: "not-an-object" };
    }
    const envelope = input.data as Record<string, unknown>;
    if (envelope.source !== MONOFORM_SOURCE) {
        return { ok: false, reason: "wrong-source" };
    }
    if (envelope.schemaVersion !== MONOFORM_SCHEMA_VERSION) {
        return { ok: false, reason: "wrong-version" };
    }
    if (typeof envelope.nonce !== "string" || envelope.nonce !== input.nonce) {
        return { ok: false, reason: "wrong-nonce" };
    }
    if (!isAllowedMonoformOrigin(input.origin, input.selfOrigin)) {
        return { ok: false, reason: "wrong-origin" };
    }
    switch (envelope.type) {
        case "shot_updated": {
            const shotId = typeof envelope.shotId === "string" ? envelope.shotId.trim() : "";
            if (!shotId) {
                return { ok: false, reason: "malformed-payload" };
            }
            const camera = cameraFrom(envelope.camera);
            if (!camera) {
                return { ok: false, reason: "malformed-payload" };
            }
            // The thumbnail is optional, and an oversized one makes the WHOLE message
            // malformed rather than being dropped: a sender that attached something too large
            // has a bug, and silently discarding it would hide that while accepting the rest.
            let thumbnail: Blob | undefined;
            if (envelope.thumbnail !== undefined && envelope.thumbnail !== null) {
                const blob = blobFrom(envelope.thumbnail, limit);
                if (!blob) {
                    return { ok: false, reason: "too-large" };
                }
                thumbnail = blob;
            }
            return { ok: true, message: { kind: "shot_updated", shotId, camera, thumbnail } };
        }
        case "export": {
            if (envelope.kind !== "image" && envelope.kind !== "video") {
                return { ok: false, reason: "malformed-payload" };
            }
            const blob = blobFrom(envelope.blob, limit);
            if (!blob) {
                return { ok: false, reason: "too-large" };
            }
            return { ok: true, message: { kind: "export", exportKind: envelope.kind, blob } };
        }
        default:
            return { ok: false, reason: "unknown-type" };
    }
}

/**
 * The postMessage target for this host's messages to the studio.
 *
 * It is the window's own origin rather than `'*'`, and this is not a formality: `'*'` sends
 * the shot's content to whatever document is in the frame, so a frame that navigated away
 * would receive the project's data. The studio is same-origin, so the target is exact.
 */
export function monoformTargetOrigin(selfOrigin: string): string {
    return selfOrigin;
}

/**
 * The envelope for one host message.
 *
 * Exported so the studio's own tests can assert the shape it must accept, and so this host
 * has exactly one place that builds one.
 */
export function buildMonoformHostMessage(message: MonoformHostMessage, nonce: string): Record<string, unknown> {
    return {
        source: ATELIER_SOURCE,
        schemaVersion: MONOFORM_SCHEMA_VERSION,
        nonce,
        type: message.kind,
        shot: message.kind === "open_shot" ? message.shot : undefined,
    };
}
