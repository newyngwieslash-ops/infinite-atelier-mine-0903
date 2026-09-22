import assert from "node:assert/strict";
import test from "node:test";

import {
    ATELIER_SOURCE,
    MONOFORM_MAX_PAYLOAD_BYTES,
    MONOFORM_SCHEMA_VERSION,
    MONOFORM_SOURCE,
    buildMonoformHostMessage,
    createMonoformNonce,
    isAllowedMonoformOrigin,
    monoformTargetOrigin,
    validateMonoformMessage,
} from "../desktop/monoform-bridge";

/**
 * The MONOFORM bridge's validation, which is the security boundary ARCHITECTURE §17 and
 * SECURITY §12 ask for and which did not exist before WP-09.
 *
 * What the previous state accepted: any message whose `source` field said "monoform", from
 * any frame, with any payload, at any size. A `source` field is a claim rather than an
 * identity — every document in the page can set a string — so these cases are about the
 * checks that a sender cannot forge: the ORIGIN the browser reports, and the NONCE this host
 * minted for one mount.
 *
 * Every rejection case asserts the REASON, not merely that something was refused. A bridge
 * that refused everything would pass a test that only checked "not ok", and it would be as
 * broken as one that accepted everything.
 */

const SELF_ORIGIN = "http://localhost:34115";
const NONCE = "nonce-1";

/** A well-formed envelope, which each case then breaks in exactly one way. */
function validMessage(overrides: Record<string, unknown> = {}) {
    return {
        source: MONOFORM_SOURCE,
        schemaVersion: MONOFORM_SCHEMA_VERSION,
        nonce: NONCE,
        type: "export",
        kind: "image",
        blob: { size: 1024 },
        ...overrides,
    };
}

function validate(data: unknown, origin = SELF_ORIGIN) {
    return validateMonoformMessage({ data, origin, selfOrigin: SELF_ORIGIN, nonce: NONCE });
}

test("a well-formed export is accepted, and its kind is carried through", () => {
    const result = validate(validMessage());
    assert.equal(result.ok, true);
    if (!result.ok) return;
    assert.equal(result.message.kind, "export");
    if (result.message.kind !== "export") return;
    // The kind is `exportKind` on the way out rather than `kind`, because `kind` is the
    // discriminated union's own tag and an inner `kind` would collide with it.
    assert.equal(result.message.exportKind, "image");
});

test("a message from another origin is refused before its payload is read", () => {
    // The payload is WELL-FORMED here, which is the point: the origin is what refuses it.
    const result = validate(validMessage(), "http://evil.example");
    assert.equal(result.ok, false);
    if (result.ok) return;
    assert.equal(result.reason, "wrong-origin");
    // A frame with no origin of its own reports "null" or the empty string, and neither can
    // be attributed to the document this host embedded.
    assert.equal(validate(validMessage(), "null").ok, false);
    assert.equal(validate(validMessage(), "").ok, false);
});

test("a wrong nonce is refused, so a message replayed from a previous mount is rejected", () => {
    const result = validate(validMessage({ nonce: "another-mount" }));
    assert.equal(result.ok, false);
    if (result.ok) return;
    assert.equal(result.reason, "wrong-nonce");
    // A message with no nonce at all is refused for the same reason: absent and wrong are
    // both "not from this mount".
    assert.equal(validate(validMessage({ nonce: undefined })).ok, false);
});

test("a different schema version is refused rather than parsed optimistically", () => {
    const result = validate(validMessage({ schemaVersion: MONOFORM_SCHEMA_VERSION + 1 }));
    assert.equal(result.ok, false);
    if (result.ok) return;
    assert.equal(result.reason, "wrong-version");
    // A version stated as a STRING is a different version, not a coercible one.
    assert.equal(validate(validMessage({ schemaVersion: String(MONOFORM_SCHEMA_VERSION) })).ok, false);
});

test("a message whose source is not the studio is refused", () => {
    const result = validate(validMessage({ source: "atelier" }));
    assert.equal(result.ok, false);
    if (result.ok) return;
    assert.equal(result.reason, "wrong-source");
});

test("an unknown type is refused, and so is a message that is not an object", () => {
    const unknown = validate(validMessage({ type: "delete_everything" }));
    assert.equal(unknown.ok, false);
    if (!unknown.ok) assert.equal(unknown.reason, "unknown-type");
    for (const value of [null, undefined, "a string", 42, [], [validMessage()]]) {
        const result = validate(value);
        assert.equal(result.ok, false, `accepted ${JSON.stringify(value)}`);
        if (!result.ok) assert.equal(result.reason, "not-an-object");
    }
});

test("an oversized payload is refused, and the bound is what refuses it", () => {
    const tooLarge = validate(validMessage({ blob: { size: MONOFORM_MAX_PAYLOAD_BYTES + 1 } }));
    assert.equal(tooLarge.ok, false);
    if (!tooLarge.ok) assert.equal(tooLarge.reason, "too-large");
    // A body at the bound is accepted, so the check is a bound rather than a prohibition.
    const atBound = validate(validMessage({ blob: { size: MONOFORM_MAX_PAYLOAD_BYTES } }));
    assert.equal(atBound.ok, true);
    // An empty body is refused: a zero-byte export is a send that went wrong, and accepting
    // it would create a canvas node with no content.
    assert.equal(validate(validMessage({ blob: { size: 0 } })).ok, false);
});

test("a shot_updated message requires a real camera", () => {
    // The camera is the STUDIO's own shape rather than one invented for the bridge: the
    // bridge carries a camera the studio produced and hands it back.
    const camera = {
        position: [1, 2, 3],
        rotation: [0, 45, 12],
        focalLength: 42,
        aspectRatio: "16:9",
    };
    const good = validate(validMessage({ type: "shot_updated", kind: undefined, blob: undefined, shotId: "shot-1", camera }));
    assert.equal(good.ok, true);
    if (!good.ok || good.message.kind !== "shot_updated") return;
    assert.equal(good.message.shotId, "shot-1");
    assert.equal(good.message.camera.focalLength, 42);
    assert.deepEqual(good.message.camera.position, [1, 2, 3]);

    // No shot id: a camera update about no shot is a message nothing can be done with.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "  ", camera })).ok, false);
    // A camera whose position is not three components — a vector of two would produce a
    // camera the studio could not build, and the stored shot would be unopenable.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, position: [1, 2] } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, rotation: null } })).ok, false);
    // A component that is not a finite number — NaN survives JSON and would poison the
    // stored camera, so it is refused rather than written.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, position: [1, Number.NaN, 3] } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, position: ["1", 2, 3] } })).ok, false);
    // A focal length nothing can be rendered from. It is REFUSED rather than clamped,
    // because clamping would silently change the shot the user composed.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, focalLength: 0 } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, focalLength: 5000 } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, focalLength: "42" } })).ok, false);
    // An aspect ratio is required and bounded: an empty one names no frame shape.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, aspectRatio: "  " } })).ok, false);
});

test("a shot_updated message may carry a thumbnail, and an oversized one fails the whole message", () => {
    const camera = {
        position: [0, 1.6, 4] as [number, number, number],
        rotation: [0, 0, 0] as [number, number, number],
        focalLength: 35,
        aspectRatio: "16:9",
    };
    const withThumbnail = validate(validMessage({
        type: "shot_updated", shotId: "shot-1", camera, thumbnail: { size: 2048 },
    }));
    assert.equal(withThumbnail.ok, true);
    if (withThumbnail.ok && withThumbnail.message.kind === "shot_updated") {
        assert.equal(withThumbnail.message.thumbnail?.size, 2048);
    }
    // Dropping the thumbnail and keeping the camera would hide the sender's bug, so the
    // whole message is refused.
    const oversized = validate(validMessage({
        type: "shot_updated", shotId: "shot-1", camera, thumbnail: { size: MONOFORM_MAX_PAYLOAD_BYTES + 1 },
    }));
    assert.equal(oversized.ok, false);
    if (!oversized.ok) assert.equal(oversized.reason, "too-large");
    // No thumbnail at all is the ordinary case, and it is accepted.
    const withoutThumbnail = validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera }));
    assert.equal(withoutThumbnail.ok, true);
});

test("the host's messages carry the envelope, and the target origin is exact", () => {
    const message = buildMonoformHostMessage(
        {
            kind: "open_shot",
            shot: {
                shotId: "shot-1", shotNumber: "6", shotSize: "MS", cameraAngle: "eye level",
                cameraMovement: "static", durationSeconds: 4, visualDescription: "a corridor",
                firstFrameDescription: "the door ajar", lastFrameDescription: "the door open",
                videoMotionDescription: "the camera pushes in",
            },
        },
        NONCE,
    );
    // The host's own source, so a studio can ignore its echo; the same version; the same
    // mount's nonce.
    assert.equal(message.source, ATELIER_SOURCE);
    assert.equal(message.schemaVersion, MONOFORM_SCHEMA_VERSION);
    assert.equal(message.nonce, NONCE);
    assert.equal(message.type, "open_shot");
    const shot = message.shot as Record<string, unknown>;
    assert.equal(shot.shotId, "shot-1");
    // The target is the window's own origin rather than '*'. Posting to '*' would send the
    // shot's content to whatever document the frame holds, so a frame that navigated away
    // would receive it.
    assert.equal(monoformTargetOrigin(SELF_ORIGIN), SELF_ORIGIN);
    assert.notEqual(monoformTargetOrigin(SELF_ORIGIN), "*");
});

test("the origin check compares exactly and refuses an empty or null origin", () => {
    assert.equal(isAllowedMonoformOrigin(SELF_ORIGIN, SELF_ORIGIN), true);
    assert.equal(isAllowedMonoformOrigin("http://localhost:34115/", SELF_ORIGIN), false);
    assert.equal(isAllowedMonoformOrigin("HTTPS://localhost:34115", SELF_ORIGIN), false);
    assert.equal(isAllowedMonoformOrigin("null", SELF_ORIGIN), false);
    assert.equal(isAllowedMonoformOrigin("", SELF_ORIGIN), false);
});

test("a nonce is unpredictable and differs between mounts", () => {
    const first = createMonoformNonce();
    const second = createMonoformNonce();
    assert.notEqual(first, second);
    assert.ok(first.length >= 16, `the nonce is only ${first.length} characters`);
});
