# ADR-0025 MONOFORM Deep Integration: the Snapshot That Was Being Dropped

Status: Accepted
Date: 2026-09-25
Work package: WP-21 (P3 item 19)
Supersedes: none
Related: ADR-0013 (the production pipeline and the MONOFORM envelope), PRD FR-060,
ARCHITECTURE §17, SECURITY §12

## Context

PRD §16 lists MONOFORM 深度双向集成, and FR-060's acceptance names three clauses:

1. 从一个 Shot 可打开导演预演并带入上下文;
2. 保存后可在 Shot 中看到摄像机参数**和预览图**;
3. 所有跨 iframe 消息校验来源、类型和 Schema, 不授予与功能无关的浏览器权限.

WP-09 built the bridge and delivered clause 3 in full: `monoform-bridge.ts` checks `event.origin`
against the two allowed origins, a per-mount NONCE, an EXACT `schemaVersion` match, and an 8 MiB
payload bound, with twelve unit tests. Clause 1 works through `director-panel.tsx`'s `open_shot`. And
clause 2's camera half works.

**THE PREVIEW HALF HAD NO PATH, AND THE WAY IT WAS MISSING IS THE POINT.** `shot_updated` carries
`thumbnail?: Blob`; `monoform-bridge.ts` validates it; `director-panel.tsx` forwards it as
`thumbnail: result.message.thumbnail`. It then reaches `director-view.tsx`'s `reportCamera`, whose
parameter type names `shotId` and `camera` and nothing else — so the thumbnail was dropped at the last
step, and **no studio component mentioned a thumbnail at all**. The clause was half-built with nothing
broken anywhere: each layer did what it said, and the field was narrowed away in a type signature.

The reason it could not have been built is the other half: nothing could turn a Blob into a stored
file. `AttachFile` links an ALREADY-COMMITTED hash to a version, and the only Blob→store path in the
build was the document upload, which commits a DOCUMENT rather than an asset file.

## Decision

**1. A snapshot is a `reference` file on the asset version the shot names.** NOT a new
`previs_snapshots` table: the asset aggregate already answers "what files does this version have"
(DOMAIN_MODEL §8.4), and a second table would be a second versioning system for the same bytes. The
role is the domain's own vocabulary, and `reference` is what a snapshot is — a picture the shot refers
to rather than the picture being shot.

**2. The transfer is the IMPORT UPLOAD's shape, and its constants.** Base64 in bounded chunks, because
a Wails binding carries text and a whole image in one message would be materialised on the webview's
main thread. `ChunkBytes` comes from the core's reply rather than the client choosing, so a client
cannot ask for the whole snapshot in one message even by accident. The bound is the import's own
`importChunkBytes`, so there is one figure rather than two to keep in step.

**3. The bytes are committed BEFORE the link.** 「失败时不影响主项目数据」 is an acceptance clause,
and this ordering is how it holds: a store failure leaves the version untouched, while the reverse
order would leave a version advertising bytes that are not there. A store failure also leaves the
transfer gone, so a retry starts cleanly rather than resuming a broken one.

**4. `camera.movement` and `camera.notes` are OPTIONAL fields, and the SCHEMA VERSION DOES NOT MOVE.**
ARCHITECTURE §17 lists Movement and Notes among what the bridge returns, and neither crossed.
The version check is EXACT EQUALITY, so a bump would refuse every message from the studio build this
repository ships (`web/public/monoform`, a separate artifact this repository does not rebuild on our
schedule). A version moves when a change would make an OLD peer MISREAD a message; every field added
here is optional in both directions, so a peer that does not know it reads the message it always read.

**5. `sceneReferenceAssetVersionIds` is added to `open_shot`, by reference.** FR-060's 「发送角色站位、
相机、镜头参数和场景参考」 names the scene reference as something the studio RECEIVES, and the message
carried none. Ids rather than bytes: ARCHITECTURE §17's 「只接收 Shot、资产引用和已批准参数」.

**6. A malformed optional field is REFUSED rather than dropped.** `optionalShortString` distinguishes
absent-or-empty (undefined, accepted), a valid value (a trimmed string), and present-but-wrong (null,
refused). An empty string is absence — a studio that renders an empty text field sends `""` — while a
number or an oversized string is a message this host will not reinterpret.

**7. The camera still saves when the snapshot will not.** The two are reported separately and the
failure is a WARNING naming which half failed, because a shot is built from its parameters and losing
them because a preview image would not store is the wrong trade.

**8. Writing back to a ShotVersion is NOT built, and the reason is recorded rather than implied.**
FR-060 says 「将结果写回 DirectorPlan **或** ShotVersion」 — a choice, not both. The plan is where a
per-shot override document exists (`SetShotOverrides`, §9.1), and `shots` has no camera column; adding
one would need a migration AND would create a second source of truth beside the overrides document.
The choice is the plan's, and the camera write-back's own comment states it.

## Consequences

- FR-060's clause 2 is whole: a snapshot the studio sends is stored, linked to the shot's version,
  recorded in the shot's overrides with the camera it was framed by, and readable back for display.
- The protocol carries three more fields without a version bump, and the reasoning is written where
  the version constant lives so the next person does not bump it reflexively.
- `MonoformBinding` declares its store as a narrow PORT (`SnapshotStore`) rather than taking the asset
  service, and the composition root supplies an adapter joining the file store and the asset service.
  Neither layer learns about the other, which is the seam this repository keeps at the root.
- **The studio does not send `movement` or `notes` yet.** The host ACCEPTS them and the tests prove it
  with constructed messages; the studio side is a separate artifact. That is recorded in STATUS §0z
  rather than implied by this ADR's title.

## Alternatives rejected

**A `previs_snapshots` table.** Rejected: the asset aggregate exists for exactly this question, and a
second table would need its own versioning, its own approval rules and its own garbage-collection
predicate — four things the `reference` role gets for free.

**Sending the image as a byte array in one message.** Rejected, and the import upload's own header
records the measurement: a 300 KB document became a 300,000-element JSON array, materialised twice,
on the main thread. A 1920×1080 PNG is larger than that document.

**Bumping `MONOFORM_SCHEMA_VERSION` to 2.** Rejected for the reason in the decision above: the check
is exact equality and the studio is a separate artifact, so a bump would break the sender this
repository ships to gain nothing — optional fields are readable by an old peer.

**Making `thumbnail` required in `shot_updated`.** Rejected: the studio's current build sends it only
when the user asks for a snapshot, and requiring it would refuse every message that carries just a
camera — the common case.

**Writing the camera into a `shots` column.** Rejected: it needs a migration, and it would make the
shot's camera exist in two places (the column and the plan's overrides document) with no rule about
which wins.

## Verification

- `go test ./... -count=1` — PASS, 57 packages. `npm test` — PASS, 116 tests (3 new).
- `sh scripts/verify.sh` — PASS, exit 0 (25 Playwright, security scans over 671 files, all fixture
  checks, SBOM, Wails production build).
- **10 mutations, 10/10 killed**, each restored byte-identically — covering the size check, the chunk
  bound, the MIME set, the unknown-upload refusal, the store-before-link order, the link failure, the
  abort, and both new validation paths in the bridge.
