// audio-track-params.ts is the track editor's parametr algebra (RP-01.2).
//
// The editor's write command (SetUsageParams) REPLACES the whole placement
// document, so a UI that sends only the fields it edited would silently clear
// every field it did not — the trim, the duration and the dialogue-line link
// would vanish the first time a user nudged the volume. The fix is not a new
// backend semantics: the save sends the FULL document it read, with only the
// user's three editable fields overwritten by the edit. `mergeTrackEdit` is
// that merge, as a pure function so its tests do not need the desktop binding.

/**
 * TrackParams is the placement document one audio usage carries (the wire
 * shape of media.TrackParams). Every field is optional because a stored
 * document may name only what it overrides.
 */
export type TrackParams = {
    offsetMs?: number | null;
    sourceStartMs?: number | null;
    sourceEndMs?: number | null;
    durationMs?: number | null;
    volume?: number | null;
    muted?: boolean | null;
    dialogueLineId?: string;
};

/**
 * TrackEdit is what the track editor lets a user change: the placement
 * offset, the gain and the mute. The trim, duration and dialogue identity are
 * NOT editor fields — they arrive from the stored document and survive.
 */
export type TrackEdit = Pick<TrackParams, "offsetMs" | "volume" | "muted">;

/**
 * mergeTrackEdit overlays one edit onto the stored document.
 *
 * The spread's own semantics are the contract here: an explicit `0` volume
 * stays a zero (a real "silence this clip" value, not a missing one), an
 * explicit `false` muted stays false, and fields the edit does not name are
 * carried through untouched. The input document is never mutated — the save
 * builds the replacement from the snapshot it read.
 */
export function mergeTrackEdit(current: TrackParams, edit: TrackEdit): TrackParams {
    return { ...current, ...edit };
}
