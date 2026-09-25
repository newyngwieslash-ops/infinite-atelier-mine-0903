# ADR-0027 The Asynchronous Video Adapter, the Protocol Shape It Assumes, and Two Defects a Boundary Test Found

Status: Accepted
Date: 2026-09-25
Work package: WP-26 (P3 item 21)
Supersedes: none
Related: ADR-0004 (job lifecycle and download policy), ADR-0015 (the mock video provider and the
audited subprocess), PRD FR-080, ARCHITECTURE §17, SECURITY §7 (SSRF and egress), AGENTS §4.3 (no paid
provider without authorisation)

## Context

P3 item 21 asks for a real video provider: PRD FR-080's four sentences are 「Provider 适配器支持提交、
轮询、Webhook（若供应商支持）、取消和结果下载」, 「任务状态必须持久化」, 「远程结果下载到本地资产存储
后才能标记为完整成功；若仅保留远程 URL，状态为 `remote_only`」, 「同一 Shot 可保留多个视频版本并批准
其中一个」. WP-11 shipped the **pipe** — the job lifecycle, the reference pipeline, the mock — and left
the adapter itself open, recorded in STATUS §0y as "a real async video adapter does not exist yet".

The reconnaissance found the shape of the gap:

| Fact | Evidence | Consequence |
|---|---|---|
| The port was ALREADY asynchronous | `VideoPort` has `Submit`/`Poll`/`Fetch`/`Cancel` (`jobs/ports.go`) | No port change was needed — the interface had waited for this |
| `VideoPortFor` resolved the mock for `mock_media` and **`unsupported` for everything else** | `registry.go`, comment: "A real async video adapter does not exist yet" | The async half of the pipeline had never been driven by a remote protocol |
| The test pattern was proven | `openai_image_test.go` injects `clientFactory`, plays a provider with `httptest` | A real adapter is testable **with no network and no key** |
| `provider_requests.capability` admitted `'video'` since migration 000003 | the CHECK constraint | No migration — but **nothing had ever written that value** |
| `provider_configs.kind` admits three kinds | migration 000002 | A new kind costs a migration to buy a NAME |

## Decision

**1. The protocol is ONE SHAPE, stated as the ASSUMPTION it is.**

```
POST   {base}/videos              {"model": …, "prompt": …, "seconds": …, "size": …}  -> {"id": "…"}
GET    {base}/videos/{id}         -> {"status": "queued|in_progress|completed|failed", "progress": n}
GET    {base}/videos/{id}/content -> the bytes, or a 302 to a CDN
DELETE {base}/videos/{id}         -> cancel
```

**Real providers differ.** This build has no authorised vendor call to compare against (AGENTS §4.3), so
the submission body's four field names are the first thing that would change against a real vendor, and
the code says so in place rather than implying otherwise. What IS verified is the protocol HANDLING:
that the async lifecycle, the status vocabulary, the error taxonomy, the audit and the cancellation
handshake all work — because those are what an `httptest` vendor can play.

The consequences of getting that wrong are asymmetric and decide the design: a field a vendor ignores
produces a generation without the user's reference image, which the user SEES; a protocol this adapter
misreads produces a job that never finishes, which the user does not.

**2. `openai_compatible` is REUSED rather than joined by a new kind.** The protocol — JSON in, an
identifier out, poll, download — is the OpenAI-compatible family's, and a fourth kind would have cost a
migration plus a validation change to buy a name. The risk of "a text provider asked for video" is met
by the provider's OWN answer: an endpoint with no video capability returns an HTTP error, the adapter
classifies it, and the job fails with a message. That is how a real provider behaves, and it is more
honest than a local guess about what a URL can do.

**3. Status mapping is a TABLE, and an UNKNOWN status is REFUSED rather than read as running.** The
vocabulary: `queued|pending|starting|in_progress|running|processing` → not done; `completed|succeeded|
success` → done; `failed|error|cancelled|canceled` → done and failed. Anything else is a
`response_invalid` refusal. A provider that renames a state would otherwise look to a user like a
generation that never finishes, with nothing in the report to say why; a refusal hands it to the retry
policy and then to a person.

`cancelled` maps to FAILED rather than to a state of its own because the port has two terminal states
and the job manager already knows about a cancellation the user made locally. A provider-side
cancellation nobody asked for is a failure of the generation.

**4. `Fetch` supports BOTH shapes, and the inline bound is the TRANSPORT'S ceiling.** Inline bytes become
a data URL the existing pipeline commits; a URL is returned so `DownloadAndCommit` streams it under the
untrusted-URL policy, where https-only, private-address refusal and the byte ceiling already live.

`maxInlineVideoBytes` is **8 MiB, and the first version's 64 MiB was wrong**: every call goes through
`guardedClient`, whose `MaxResponseBytes` is 10 MiB, so a 64 MiB bound was unreachable — the client would
truncate first, and the truncation would arrive as a short read rather than as this adapter's refusal.
The test that fed it 64 MiB is what found this. A real clip is tens of megabytes, so the refusal names
the URL mode as the alternative rather than failing at a swap file.

**5. The audit writes `provider.CapabilityVideo` — the first writer in this build.** The column has
admitted the value since migration 000003 and no adapter had ever used it, so a query for a project's
video calls would have found none regardless of how many were made.

**6. NO WEBHOOK.** PRD writes 「若供应商支持」, and a local desktop application cannot default to opening a
public listener to receive one. This remains open rather than impossible: a provider that offers webhooks
would need a tunnel and an explicit user decision, which is a different package.

**7. Cancel reports its failure rather than swallowing it.** The job table has a
`remote_cancel_unconfirmed` column for exactly this state, so `Cancel` returns the provider's error and
the caller decides. A `Cancel` that always returned nil would make that column unreachable.

## The defects the boundary test found

The adapter's own suite proves it speaks the protocol. That is a DIFFERENT claim from "the pipeline
accepts what it produces", made by different code — and driving the real adapter through the real runner
and result store (against a fake vendor and nothing else) found two things no adapter-level test could:

**D1. A FAILED GENERATION WAS AUDITED AS A SUCCESS.** The poll above answers `{"status":"failed"}` with an
HTTP 200, because the CALL succeeded. The adapter recorded `StatusSucceeded` with an empty `error_code` —
and `DiagnosticsReader.ErrorCodes` groups `provider_requests` by exactly that column. So the one place
this repository PERSISTS a stable code for an operation would have been silent about video failures: a
user asking "why do my videos never generate" would have found nothing. The image adapter already draws
the line the other way (a 200 whose body carries a provider refusal is audited `failed`), and the poll
now does the same. The mutation that reverts it is killed by the audit assertion.

**D2. AN ERROR-CODE FIELD WAS PARSED AND NEVER READ.** `document.Error.Code` was decoded into a struct
field nothing consumed — the same shape the repository's reviews keep finding in other forms. It is
removed, with the reason written where it was: the audit's `ErrorCode` is this adapter's own
CLASSIFICATION (the provider taxonomy the job manager retries on), not a vendor's vocabulary, so there is
nowhere for the vendor string to go. A future change wanting to surface it must add a field to
`RemoteStatus` or `RequestRecord` rather than quietly parsing it.

**D3 (a fixture, recorded because it cost time and will recur).** The first version of the boundary test
failed with "The provider could not be reached" — mapped to a *network* error — for a call that never
left the process. The cause was the test's own secret resolver returning the same backing array each
call: `authorize` zeroes the slice it is handed, so the second call built an Authorization header from
NUL bytes, and `http.Header.Set` made the transport refuse with
`net/http: invalid header field value for "Authorization"` wrapped in a `url.Error`. The registry's
`SecretResolver` interface now documents the contract, the fixture does what the package's `staticSecret`
has always done (copy), and the boundary test asserts the Bearer header on EVERY request rather than once
— because the second call is where a shared-buffer resolver stops working, and the failure does not
mention the credential.

## Consequences

- The async video pipeline is reachable from a real provider kind for the first time: submit → park →
  poll → fetch → verify → commit, with the reference images the runner assembles.
- **What is NOT claimed: that a real vendor accepts this submission body.** No authorised call was made.
  The tests prove the protocol handling, the error classification, the audit, the SSRF policy and the
  cancellation handshake — all offline, against a fake vendor. STATUS does not say "已对接真实视频
  Provider" and must not be edited to.
- `input_reference` and the three sibling field names are an assumption; a different vendor means editing
  one file (`openai_video.go`), not configuring anything.
- The mock remains registered and reachable for `mock_media`: a build keeps both, and neither replaces
  the other.
- The `SecretResolver` contract is now written down. An implementation that hands out a buffer it intends
  to reuse authorises the first call and fails every one after, with a message that blames the network.
