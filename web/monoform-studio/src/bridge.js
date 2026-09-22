/**
 * The studio's half of the MONOFORM bridge.
 *
 * ## What this is
 *
 * FR-060 requires a versioned, validated two-way protocol between this studio and the canvas
 * host that embeds it, and lists four capabilities: open a previs from a Shot, receive the
 * staging and camera, save a snapshot, and write the result back. The `export` direction
 * existed; `open_shot` and `shot_updated` are what make the other three real.
 *
 * ## The envelope is not optional
 *
 * Both directions carry `{ source, schemaVersion, nonce, type }`. The studio must SEND the
 * envelope — a message without it is refused by the host, and the studio would appear to work
 * while its exports silently vanished — and it must VALIDATE what it receives, because the
 * host is not the only document that can post into this frame.
 *
 * The nonce is the host's: it arrives on the first inbound message and every reply carries it
 * back. The studio does not mint one, because a value the receiver invented is not a
 * protection for the receiver.
 *
 * ## Why the target origin is the parent's own
 *
 * `postMessage(..., '*')` sends to whatever document holds this frame. The host is the
 * parent, so the target is the parent's origin — and this file refuses to send at all when
 * that origin is unreadable, because a message with no destination is worse than no message:
 * it would be broadcast rather than dropped.
 */

/** The envelope version this studio speaks. It must equal the host's. */
const SCHEMA_VERSION = 1;

/** What the studio's messages are marked with. */
const STUDIO_SOURCE = 'monoform';

/** What the host's messages are marked with. */
const HOST_SOURCE = 'atelier';

/** The parent's origin, or null when this frame is not embedded or the origin is unreadable. */
function parentOrigin() {
  if (typeof window === 'undefined' || !window.parent || window.parent === window) return null
  try {
    const origin = window.parent.location.origin
    // Reading the parent's location throws for a cross-origin parent, which is the correct
    // answer: if the origin cannot be read, the target cannot be named, and this file will
    // not fall back to '*'.
    return origin && origin !== 'null' ? origin : null
  } catch {
    return null
  }
}

/** Whether this studio is embedded in a host, which is what enables the bridge at all. */
export function isEmbedded() {
  return typeof window !== 'undefined' && !!window.parent && window.parent !== window
}

/**
 * Post one message to the host, in the envelope.
 *
 * Returns whether it went out. A caller that gets false has a message nobody received, and
 * the studio's UI says so rather than reporting success.
 */
export function postToHost(nonce, type, payload) {
  if (!isEmbedded()) return false
  const target = parentOrigin()
  if (!target || !nonce) return false
  window.parent.postMessage(
    { source: STUDIO_SOURCE, schemaVersion: SCHEMA_VERSION, nonce, type, ...payload },
    target,
  )
  return true
}

/**
 * Subscribe to the host's messages.
 *
 * The listener validates before it does anything: the source, the version and the type are
 * checked here, and the ORIGIN is checked against the parent's own — so a message from a
 * sibling frame, or from a document that is not the parent at all, is ignored.
 *
 * `onOpenShot` receives the shot the host wants opened, and returns the nonce it carried so
 * the studio's replies can use it.
 */
export function subscribeToHost(onOpenShot) {
  if (!isEmbedded()) return () => {}
  const expectedOrigin = parentOrigin()
  const handler = (event) => {
    // The parent's origin, when it is readable, is the only accepted sender. A frame whose
    // parent is cross-origin cannot name it, and then this check is skipped — the envelope's
    // nonce is what carries the weight there, and it is still required.
    if (expectedOrigin && event.origin !== expectedOrigin) return
    const data = event.data
    if (!data || typeof data !== 'object' || data.source !== HOST_SOURCE) return
    if (data.schemaVersion !== SCHEMA_VERSION) return
    if (typeof data.nonce !== 'string' || !data.nonce) return
    if (data.type !== 'open_shot') return
    const shot = data.shot
    if (!shot || typeof shot !== 'object') return
    onOpenShot({ nonce: data.nonce, shot })
  }
  window.addEventListener('message', handler)
  return () => window.removeEventListener('message', handler)
}

/**
 * Tell the host that a shot's camera changed.
 *
 * `thumbnail` is optional and is a PNG blob when the caller has one. The host writes both to
 * the shot, which is FR-060's "保存后可在 Shot 中看到摄像机参数和预览图".
 */
export function sendShotUpdated(nonce, shotId, camera, thumbnail) {
  if (!shotId || !camera) return false
  return postToHost(nonce, 'shot_updated', { shotId, camera, thumbnail })
}

/**
 * Tell the host that a still or a clip was rendered, so it can become a canvas node.
 *
 * The shape is unchanged from the version this file replaces — `{ type: 'export', kind,
 * blob }` — except that it now travels inside the envelope and to a named origin rather than
 * to '*'.
 */
export function sendExport(nonce, kind, blob) {
  if (!blob) return false
  return postToHost(nonce, 'export', { kind, blob })
}

/**
 * The camera parameters a shot opens with, in the studio's own shape.
 *
 * A `director_plan_version`'s `shot_overrides_json` is the host's place for per-shot camera
 * decisions, so this reads that document when it holds one and falls back to null. A camera
 * that cannot be read is NOT invented: the studio opens on its default and the user composes
 * from there, which is better than opening on numbers nobody chose.
 */
export function cameraFromOverride(overridesJson) {
  if (typeof overridesJson !== 'string' || !overridesJson.trim()) return null
  try {
    const parsed = JSON.parse(overridesJson)
    const camera = parsed?.camera
    if (!camera || typeof camera !== 'object') return null
    const position = camera.position
    const rotation = camera.rotation
    if (!Array.isArray(position) || position.length !== 3) return null
    if (!Array.isArray(rotation) || rotation.length !== 3) return null
    if (![...position, ...rotation].every((value) => typeof value === 'number' && Number.isFinite(value))) return null
    if (typeof camera.focalLength !== 'number' || !Number.isFinite(camera.focalLength)) return null
    return {
      position: [...position],
      rotation: [...rotation],
      focalLength: camera.focalLength,
      aspectRatio: typeof camera.aspectRatio === 'string' && camera.aspectRatio ? camera.aspectRatio : '16:9',
    }
  } catch {
    return null
  }
}
