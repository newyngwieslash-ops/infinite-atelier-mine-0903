# Infinite Atelier User Guide

This guide describes what this build of Infinite Atelier **actually does**. It is written from the
code and the running surface, not from the specification, and it names the things that do not work
rather than leaving them out. Where a feature is unbuilt, this guide says so and says why.

It is the first user-facing document in this repository. Every other document here is a
specification, an architecture record or a work-package report; those are written for people
building the product. This one is written for people using it.

---

## 1. What this is, and what it is not

Infinite Atelier is a **Windows desktop application** for planning and producing short-form drama:
importing a source story, building a story graph, running a script pipeline, designing shots,
storyboarding them, and exporting an episode.

Three facts about the current build matter before anything else:

1. **It is a desktop application, and most of the studio requires the desktop build.** Many sections
   read from a local database through the app's own core. Opened in a browser (the Vite development
   server), those sections show a notice saying the core is not available rather than a broken page.
2. **There is no working video or audio generation in this build.** Images work. Video and audio do
   not, on either the canvas or the studio. Section 9 and section 11 below state exactly what
   happens and why.
3. **Your data lives on your machine.** No account, no cloud, no telemetry. Section 15 says where it
   goes and how to back it up.

### What works, in one paragraph

You can create a drama project, import a TXT/Markdown/DOCX source, have it split into chapters and
extract story facts, build a story graph, run the script pipeline (skeleton → adaptation strategy →
script) with a model you configure, approve versions, generate director plans and storyboard tables,
generate **panel images** for shots, write subtitles, read the timeline, and **export a real playable
MP4** from your approved panels with audio and subtitles — provided `ffmpeg` is installed. You can
also use the free canvas, the asset library, the job centre, the agent centre, the memory centre and
the quality centre.

### What does not work

| Feature | State | Why |
|---|---|---|
| Video generation (studio) | **Fails at provider resolution** | No real video adapter exists. See §13. |
| Audio generation (studio) | **Fails at provider resolution** | No real audio adapter exists. See §13. |
| Video/audio on the free canvas | **Refused before submission** | A canvas node is not a shot or a dialogue line. See §13.2. |
| "Which lines have audio" list | **Does not exist** | The core has no per-line audio read. See §13.4. |
| Generating or approving a **panel image** | **No screen does it** | The core commands exist and nothing calls them. See §11.2 — this is what the export needs. |
| Encrypted backups | **Not built** | Decided against for v1; see `docs/adr/0016-*.md`. |
| Backup/restore **from the UI** | **Not built** | The commands exist in the core but no screen calls them. See §15.3. |
| Code-signed installer | **Not built** | Needs a purchased certificate. See `docs/INSTALL_AND_SIGNING.md`. |
| Per-asset licence/rights records | **Does not exist** | No column anywhere stores one. See §14.1. |

---

## 2. Installing and starting

See `docs/INSTALL_AND_SIGNING.md` for the full picture. The short version:

**Requirements:** Windows 10/11 x64, and the Microsoft Edge WebView2 runtime. Most Windows machines
already have WebView2; if yours does not, the application offers to download and install it on first
launch, which needs a network connection.

**To run it:** there is no installer. Copy `InfiniteAtelier.exe` anywhere you like and run it. The
executable embeds its entire user interface, so nothing else is needed — no Node.js, no Python, no
runtime to install.

**The binary is not code-signed**, so Windows will show an unknown-publisher warning the first time
you run it. That is expected in this build, not a sign of tampering.

**For full function, install `ffmpeg`** and put it on your `PATH`. Without it the application still
runs and everything except media export works; the export section says so in its own words rather
than failing silently. See §12.

---

## 3. The two halves of the application

The top navigation bar has **six** entries, and the logo returns to the home page:

**The free canvas side** — a general-purpose visual workspace that existed before the drama studio:

| Entry | What it is for |
|---|---|
| **我的画布** (My Canvases) | Free-form canvases: images, text, audio, video, generation nodes on an infinite board. |
| **导演台** (Director) | The MONOFORM previsualisation tool, editorially separate from the studio's director section. |
| **我的资产** (My Assets) | The general asset library: images, text, video, with search, export and import. |
| **任务中心** (Job Centre) | Every generation job: queue, progress, failures, retry, pause/resume. |
| **配置** (Settings) | Model channels, API keys, and the application's own backup/restore. |

**The drama studio side:**

| Entry | What it is for |
|---|---|
| **剧集工作室** (Drama Studio) | Create and open drama projects; the sixteen sections described in §6 onwards. |

The home page (the logo, or `/`) is the brand landing page with recent projects and the built-in
prompt library. Both halves share the same settings and the same job queue.

---

## 4. Configuring a model (do this first)

Almost nothing in the studio works without a configured model. Open **配置** and add a channel:

- **API address and model name** — an OpenAI-compatible or Gemini-compatible endpoint. The
  application speaks both protocols.
- **API key** — on the desktop build, keys are entered through a **secure field** and go straight
  into the **Windows Credential Manager**. The interface shows only whether a key is configured.
  **Keys never enter the browser storage, never appear in an ordinary backup, and are never shown
  back to you.**
- If you upgraded from an older browser-based version that stored keys in plain text, the
  application tells you once per session that plaintext keys were found. Re-enter the key through
  the secure field and then clear the old value. The application will not clear it for you, because
  deleting a key it cannot migrate would lose your configuration.

**Model capabilities matter.** A channel is used for text, images, video or audio depending on what
you configure. The studio's pipeline stages use text; panel images use image; video and audio
submissions require those capabilities but cannot succeed in this build (§13).

---

## 5. The Drama Studio: creating a project

Open **剧集工作室**. It lists the projects whose type is `drama`; a free canvas project does not
appear here and will tell you so if you try to open it.

**新建剧集项目** opens a two-step wizard:

**Step 1 — 基本信息 (Basics)**

| Field | Meaning |
|---|---|
| 项目名称 (Name) | Required. The only mandatory field. |
| 语言 (Language) | Defaults to `zh-CN`. Drives the default language for text processing. |
| 目标平台 (Target platform) | 竖屏短剧 / 横屏网剧 / 影院 / 电视 / 网络平台. |

**Step 2 — 剧集设置 (Episode settings)**

| Field | Meaning |
|---|---|
| 画幅 (Aspect ratio) | Free text, e.g. `9:16`. |
| 分辨率 (Resolution) | Free text, e.g. `1080x1920`. |
| 目标集数 (Target episode count) | A number. |
| 单集时长 (Default episode length, seconds) | A number. |
| 受众 (Audience) | Free text. |
| 内容分级 (Content rating) | Free text. |
| 改编尺度 (Adaptation mode) | 忠于原著 / 均衡 (default) / 大幅改编. |

The project is created through the core before you see it, so if creation fails you get an error and
no half-made project.

---

## 6. The sixteen sections

Opening a project shows a section navigation on the left. Each section is either built (it reads and
writes real data) or shows an honest empty state naming the work package that will fill it. **No
section displays placeholder data.** As of this build **every one of the sixteen sections is marked
available**, so there is nothing left in that empty state — but the mechanism stays for the case
where a section's content is removed or added.

Most sections are **episode-scoped**: you pick an episode in the section, and the studio remembers
that choice across sections, so moving between the script, director, storyboard, video, audio and
timeline sections keeps the same episode selected.

### 6.1 The sixteen sections, at a glance

The order is fixed by `STUDIO_SECTIONS` in the studio store, which follows PRD §8:

| # | Section | Built by | Covered in |
|---|---|---|---|
| 1 | 总览 (Overview) | WP-05 | §6.2 |
| 2 | 原著 (Source) | WP-05/WP-06 | §7 |
| 3 | 故事图谱 (Story Graph) | WP-05/WP-06 | §8 |
| 4 | 剧本 (Script) | WP-05/WP-08 | §9 |
| 5 | 角色 (Characters) | WP-05/WP-09 | §10 |
| 6 | 场景地点 (Locations) | WP-05/WP-09 | §10 |
| 7 | 道具 (Props) | WP-05/WP-09 | §10 |
| 8 | 导演方案 (Director) | WP-09 | §11.1 |
| 9 | 分镜表 (Storyboard Table) | WP-09 | §11.2 |
| 10 | 分镜画布 (Storyboard Canvas) | WP-05 | §11.3 |
| 11 | 视频 (Video) | WP-11 | §13 |
| 12 | 音频 (Audio) | WP-11 | §13 |
| 13 | 时间线 (Timeline) | WP-11 | §12 |
| 14 | 质量 (Quality) | WP-05/WP-10 | §14.1 |
| 15 | 智能体 (Agents) | WP-07 | §14.2 |
| 16 | 记忆 (Memory) | WP-10 | §14.3 |

### 6.2 总览 (Overview)

A read-only summary of the project: its settings (language, target platform, aspect ratio,
resolution, episode count, default episode length, audience, content rating, adaptation mode,
timezone) and its current size (episodes, source documents, project rules, and **open stale marks**).
It is a dashboard, not a form — the settings were fixed by the creation wizard.

---

## 7. 原著 (Source) — importing your story

This is where a project begins. The section has two parts.

### 7.1 Registering the document

**导入文档正文** (Import document text) opens a file picker accepting **TXT, Markdown and DOCX**.
Choose your file and the application shows a **preview before writing anything**:

- The detected encoding and format.
- How many chapters it detected and where the boundaries fall.
- Whether this file has been imported before.

**The format is decided by the bytes, not the extension.** A `.docx` that is really plain text is
treated as plain text. Encodings recognised: UTF-8 (with or without BOM), UTF-16 (by BOM), GBK and
GB18030. **Text that cannot be decoded losslessly is rejected rather than guessed at** — you get an
error instead of a mangled document.

**Importing the same file twice is refused by default.** The application recognises the file by its
content hash and tells you which document it is already part of. You can explicitly confirm to
import it again as a **new version**; the old version is never overwritten.

**Chapter boundaries can be adjusted before you confirm.** Detection uses Chinese chapter markers,
Markdown headings and English `Chapter N` through one shared rule set, so the same text in DOCX and
TXT produces the same boundaries. You can edit a heading or an offset; doing so marks the chapter's
source as `manual`. You can also **split and merge chapters** after import.

**DOCX is read conservatively.** Only `[Content_Types].xml` and `word/document.xml` are read.
Styles, footnotes and comments are ignored, and **no external relationship or URL is ever fetched**.
The container goes through a hardened ZIP reader that refuses path traversal, zip bombs and
entry-count bombs.

### 7.2 Reading the text and extracting facts

Once imported, **章节数** opens the chapter list for the document's current version. You can read the
source text a page at a time — a 100,000-character document never arrives as one message.

**抽取事件候选** (Extract event candidates) runs the extraction contract against your configured
text model. Two things are worth knowing:

- **Extraction only ever writes candidates.** A fact the model proposes is a proposal, not a
  decision. It cannot approve itself.
- **If the model's output fails validation, it gets exactly one retry** with the violated rule and
  the JSON pointer sent back. If it fails again, the extraction fails. The retry does not include
  your document text, so untrusted content is not fed back into the next prompt.

---

## 8. 故事图谱 (Story Graph)

The fact layer: entities, events, relations, and where each fact came from.

- **审查队列 (Review queue)** and **已确认事实 (Accepted facts)** are two filters over the same
  list — a candidate is a proposal and an accepted fact is a decision, and they are not the same
  thing.
- For each entity you can **accept**, **reject** or **lock** it. Locking protects a fact you are
  sure about. Events can be locked and unlocked the same way.
- **别名 (Aliases)**, **参与者 (Participants)** and **证据 (Evidence)** are all recorded. Evidence
  is shown as a **reference** — version, chapter and offsets — never as copied text, so a fact can
  always be traced back to the passage that produced it.
- **冲突 (Conflicts)** can be opened, listed and resolved.
- The visualisation is deliberately minimal: entities as nodes, relations as edges. It is a reading
  aid; the free canvas is the place for real visual work.

---

## 9. 剧本 (Script) — the three-stage pipeline

The script pipeline has three stages, and each must be approved before the next will run.

```text
故事骨架 (Story skeleton)
   ↓ approve
改编策略 (Adaptation strategy)
   ↓ approve
剧本生成 (Script generation)
```

**How it works.** Each stage has a **run** button and its own version history. Running a stage calls
your configured text model, produces a version, and a **supervisor** reviews it. You then decide.

**The gate.** Approving is not a formality: the next stage checks that the previous one has an
approved version, and refuses to run otherwise. The order is deliberate — **the artifact is approved
first and the stage moves second**, so a failure leaves you in a state you can retry rather than one
where the stage reports success and nothing is actually approved.

**Your five decisions at a gate:**

| Decision | What it does |
|---|---|
| **PASS** | Approve and move on. |
| **FIX** | Send the specific findings back and re-run. The next attempt reads those findings **from the database**, so a FIX survives an application restart. |
| **REDO** | Re-run from scratch. |
| **MANUAL_EDIT** | Write a version yourself. |
| **SKIP** | Skip the stage. Requires a written reason. |

**Field locks.** Any field of a version can be locked. A locked field **cannot change in the next
version** — if the model produces a different value for a locked field, the write is **rejected**,
whether or not the model appears to have understood the instruction. Use this to hold a line you are
happy with while regenerating everything around it.

**Version diff.** Compare any two versions side by side, aligned by position, with locked fields
marked.

**Projection.** Approving a script can project its scenes onto a canvas as real nodes, so the story
structure has a spatial representation.

**Automatic revisions are capped at two.** After two failed automatic attempts the pipeline stops
and waits for a person rather than looping.

---

## 10. 角色 / 场景地点 / 道具 (Characters / Locations / Props)

Three sections over one asset model. Each lists assets of its type with their approved version.

- **Create an asset** with a name. That is the one write these sections offer.
- **版本 (Versions)** opens a drawer listing every version, each with its status and its lineage —
  which job produced it, its seed.
- **Approving a version shows you the impact before you confirm**, and the core refuses an approval
  without your acknowledgement: what depends on the version you are replacing.
- **用途 (Usages)** is read for the currently approved version, because that is what an approval
  switch disturbs.

**What these sections cannot do, and it is the same shape of gap as §11.2.** A version can only be
**approved** here; **nothing in the interface creates one.** `AddVersion`, `AttachFile`,
`AttachJobResult`, `AddUsage` and `AddRelation` all exist in the core and have **zero frontend
callers** — counted, not guessed. So:

- An asset you create has **no versions**, and therefore nothing to approve.
- The route that was meant to fill this — the image batch in the storyboard section, whose results
  would arrive as **candidate** versions via `AttachJobResult` — has no UI either (§11.2).
- Items come from the generated-image pipeline, which produces files; attaching one to an asset as a
  version is a core command with no screen.

**The practical consequence:** the asset sections are usable as a registry and a reading surface,
and a version's approval lifecycle is implemented and tested underneath, but **a user cannot produce
an asset version through the interface in this build.** The export reads approved media, so this is
part of the same chain as §11.2 and §12.

---

## 11. 导演方案 (Director) and 分镜表 (Storyboard)

### 11.1 导演方案

- **规划版本 (Plan versions)** — run the `director_plan` stage to produce a plan from an approved
  script, review it, and approve a version. Approving names the version explicitly.
- **打开预演 (Open previsualisation)** launches the MONOFORM previs studio with that shot's framing,
  camera and description already loaded. The studio is the same build the free canvas embeds.
- **Camera write-back.** When you save in the previs studio, the camera parameters are written back
  to the shot row through the same command the storyboard table uses, so one code path owns a shot's
  camera whether the value came from a form or from the studio.

### 11.2 分镜表

A row per shot, generated from an approved script plus the director plan plus approved assets.

- Every per-shot field is present: framing, camera, description, and the first-frame /
  last-frame / motion descriptions.
- **Rows are edited one at a time.** This is not a limitation but the point: a fix to one shot
  cannot disturb the others, and the application proves it rather than promising it.
- **A stale row is refused, not overwritten.** The revision the table read travels with your edit,
  so a change made in another window is reported as a conflict instead of being silently replaced.
- **Run the supervisor** to get a verdict that can point at a **specific row**. A FIX applies to that
  row alone.
- **The board can leave as a document** — aligned text or CSV — rendered from a version you pick.

**What the storyboard table does NOT offer, and this is a real gap.** The core implements four things
the section has no control for, and none of them is reachable from any screen today:

| Core command | What it would do | Why it matters |
|---|---|---|
| `RunImageBatch` | Submit image jobs for the shots, N candidates per shot, with a capped concurrency and an idempotency key that survives a restart | **This is how panel images are produced.** Without it, a shot's panel has to be produced by some other route. |
| `CheckStoryboardGate` | Refuse a batch while the gate has not passed | The safety half of the above. |
| `CollectBatchResults` | Turn finished jobs into candidate asset versions | The step between "a job finished" and "you have a picture to approve". |
| `ApproveCandidate` / `ApprovePanelImage` | Make one candidate a shot's canonical panel | **Approving a panel is what the export reads.** |

`listPanelVersions` and `getApprovalImpact` exist as frontend wrappers but are not called by the
storyboard table. **So: panel images cannot be generated or approved through the interface in this
build**, even though the export in §12 composes from approved panels. The export's refusal to run
without approved media is therefore the honest end of a chain whose middle is missing.

This is recorded here rather than only in a status document because it is the one gap a user would
otherwise discover by trying. It is not in `docs/implementation/STATUS.md`'s outstanding lists; it is
established by the absence of any caller, which is the same shape of gap the repository has treated
as a finding before.

### 11.3 分镜画布

A read-only view of the **projected** canvas: the nodes and semantic edges the script and storyboard
commands wrote. Only nodes carrying an entity reference and only edges carrying a relation type are
shown, because those are the ones with domain meaning — a bare geometric node is canvas furniture,
not a domain fact.

This is a listing rather than an editor. The free canvas page is where a canvas is actually worked
on, and duplicating that here would give the same data two renderers. An empty result means no
projection has been written yet, and the section says so rather than showing a blank page.

---

## 12. 时间线 (Timeline) and export

This is the assembly section, and it is the largest of the media sections.

### 12.1 The three columns

The timeline joins three things per shot, in shot order:

- **媒体 (Media)** — the media approved for that shot. `已批准` with its kind, or `缺少媒体`.
- **音频 (Audio)** — whether audio is approved for any line in the row's scene.
- **字幕 (Subtitles)** — how many cues fall inside the shot's time span.

Plus totals: total duration, how many shots are missing media, cue count, and how many spoken lines
have no subtitle.

**An export refuses to run while any shot is missing approved media.** It says so rather than
producing a broken file.

### 12.2 Subtitles

- **生成字幕草稿 (Draft subtitles)** builds a track from the script's **audible lines** — dialogue
  and narration. Action, transition and note lines are production instructions and do not get
  subtitles. The draft and the missing-line check share **the same rule**, so they cannot disagree.
- **批准 goes in two steps**, and both controls are offered: a draft must be **submitted for review**
  first, and only a track under review can be approved. The buttons are enabled only when the core
  would accept the action.
- **The editor** lets you change start/end times and text, add and remove cues, and save all cues in
  one transaction. It warns you about a cue ending before it starts, or two cues overlapping.
- Subtitles export as **SRT or VTT**; the text is rendered first and shown to you, and saving is a
  separate action.

### 12.3 Export

1. Choose quality (preview or final), width and height, and a subtitle mode: **not included**,
   **sidecar file**, or **burned into the picture**.
2. **合成并导出 (Compose and export)** runs the export.
3. Approval is again two steps: submit for review, then approve.
4. **保存到… (Save to…)** opens a native save dialog. **The destination comes from the dialog and
   from nowhere else**, so a compromised interface cannot write anywhere you did not point at.
5. **清单 (Manifest)** expands per export row, showing exactly which versions of which artifacts the
   episode was made from, with hashes.

**What the export actually produces.** This is the part most worth being precise about: the export
composes a real MP4 from **your approved panel images**, each held for its shot's duration, with
audio and subtitles muxed in. It is a real, playable file with real timing. **It is a slideshow of
approved frames rather than moving footage**, because there is no working video generation in this
build (§13). The moment a real video adapter exists, the same export will use moving footage — the
composition shape does not change, only which files fill it.

**Where the approved frames come from, and the gap in the chain.** A shot's approved frame lives in
`storyboard_panel_versions.approved_image_asset_version_id`, and the export reads exactly that. But
**no screen in this build generates or approves a panel image** — see §11.2. So the export's refusal
to run while a shot has no approved media is usually the *first* thing you will hit, and it is a
refusal with no in-product remedy today. Images whose approval you set through the asset sections
(§10) are the route that exists. This is recorded as an outstanding item in
`docs/RELEASE_CHECKLIST.md` §8.2.

### 12.4 Documents

Four documents can be exported, each rendered by the core as text and then saved through the same
native save dialog:

| Document | Formats |
|---|---|
| 剧本 (Script) | plain text, or **Fountain** |
| 镜头表 (Shot list) | aligned text, or **CSV** |
| 字幕 (Subtitles) | SRT, VTT |
| 导出清单 (Manifest) | JSON |

Script and shot list offer a **version picker**, so a document is a render of a version you choose.
Changing the version or the episode **invalidates the preview** and you must re-render before saving
— the save button sends the text you are looking at, and leaving a stale preview would let a
different version reach your disk.

### 12.5 ffmpeg

The export needs **`ffmpeg` and `ffprobe` on your `PATH`**. If they are missing, the application
disables export and shows you the core's own diagnostic sentence naming what to install — it does
not pretend the capability exists. Everything else in the application works without ffmpeg.

---

## 13. 视频 (Video) and 音频 (Audio) — read this section carefully

**Neither video nor audio generation works in this build.** It is important to understand what
happens, because the screens are real and the failure is not.

### 13.1 What the video section does

**视频** lists the shots of the current episode's approved storyboard with their duration, approved
media and audio state. Below that, you can pick a shot, set a duration, write a prompt and **submit a
video job**. The job goes into the queue like any other and appears in the job list with its status.

**The job will fail at provider resolution.** The core resolves a video adapter for a provider
configuration, and **the only video adapter this build contains is a deterministic mock that no
configuration can select**. Real kinds (`openai_compatible`, `gemini_compatible`) return
"unsupported" for video. So a submission reaches the queue and fails there.

**What the video section does not offer, and why:** there is no per-shot version picker and no list
of a shot's video versions. The core exposes no command that enumerates or selects them, and adding
a control that invents a call would be a lie. The section shows the job's outcome and its stored
files instead.

**First frame, last frame and reference images are not offered in this section.** The core's request
accepts them, but this section has no image picker that could produce them, and sending empty values
would be indistinguishable from a request that genuinely meant "no references". The section says
this in its own text.

### 13.2 The free canvas has no video or audio either

On the free canvas, video and audio generation are **refused before submission**, with a message
explaining why: the core's video jobs are per **shot** and its audio jobs are per **dialogue line**,
and a free canvas node is neither. There is no honest value to put in those fields.

**Image generation on the canvas is unaffected** and works through the core like everything else.

### 13.3 What this means for you

You can complete the whole pipeline up to and including an **exported MP4 of your approved frames
with audio and subtitles**. What you cannot do is generate moving video or synthesised speech.

The application tells you this at startup with a notice, and says it again in the sections
themselves. This is a deliberate capability gap rather than a broken screen: the alternative — a
mock that returns a file that is not really a video — would be worse.

### 13.4 What the audio section does

**音频** shows, per shot, whether audio is approved for that shot's scene, and names the dialogue
line being voiced.

**提交一句对白的配音** lets you pick a **script version**, then a **line the audience hears**
(dialogue or narration — action and transition lines are excluded, and the list says so), see its
scene, character and text, and submit a TTS job. The line is **chosen**, not typed.

The voice and format come from the project's audio settings; this section does not define its own.

**The job fails at provider resolution, for the same reason video does.**

**There is no per-line "which lines have audio" list.** The core has no read for it: the audio join
lives in the timeline read and reports per **shot**, not per line. So the section reports the shot's
state and names the line it is voicing, and it says so rather than inventing a per-line status.

---

## 14. 质量 (Quality), 智能体 (Agents) and 记忆 (Memory)

### 14.1 质量 (Quality Centre)

- **工作流运行 (Workflow runs)** — every pipeline run, its status and its current stage.
- **发现 (Findings)** — select a run to read its review report: each finding with its severity, the
  entity it is about, and its **evidence** as references. Every finding is marked with which half
  produced it: **deterministic** (computed by code) or **llm** (the supervisor model's judgement).
- **过期标记 (Stale marks)** — when you change an artifact, everything downstream that consumed it
  is marked. Direct consumers are marked `review_required`, transitive ones `informational`. You can
  **clear** a mark or **waive** it. A waiver requires both a decision reference and a written
  reason, and the core refuses one without either.
- Findings link to the section where the entity lives, so you can jump straight to it.

**A minimum bar, stated plainly:** the deterministic checks cover costume continuity, prop
continuity, location continuity, shot coverage and ordering, duration sums, and asset version
approval. FR-110's **Safety and Cost categories are covered by neither half** — no deterministic
rule and no supervisor skill checks them — and nothing in the product says so except this guide and
`docs/implementation/STATUS.md`. **There is also no per-asset licence or rights metadata anywhere in
the schema**, so "is this asset cleared for use" is a question the application cannot answer; the
final review reports this as a known gap rather than silently passing it.

### 14.2 智能体 (Agent Centre)

What the agent runtime did, for this project.

- **运行记录 (Runs)** — filter by layer: Decision, Execution or Supervision.
- **A run's detail** shows the tools it called, their outcomes, and the messages exchanged.
- **智能体清单 (Inventory)** — which agents this build can run, and which tool keys each is allowed
  to call.

**There is no button here to start a run.** Runs are started by the application services — by the
pipeline stages — which is deliberate: who may run an agent stays on the server side, not in the
interface.

**No reasoning traces are shown, because none are recorded.** What a run stores is the reason
summary the model itself produced, the tools it called, and the versions it reported. The screen
shows exactly that and nothing more.

### 14.3 记忆 (Memory Centre)

What this project remembers. Memory is scoped to one project at a time — showing two projects'
memories together would be precisely the cross-project leak the design forbids.

- **列表 (List)** with a type filter, and an option to include deleted items.
- **固定 / 编辑 / 删除 (Pin / Edit / Delete)** — deleting invalidates summaries, and editing clears
  the vector so the memory stops being searchable until rebuilt. **Both consequences are confirmed
  before they happen**, not after.
- **重建向量 (Rebuild vector)** re-embeds a memory whose vector was cleared.
- **立即压缩 (Summarise now)** compresses recent messages into a summary.
- **召回预览 (Recall preview)** shows what a query would retrieve, with each candidate's **fused
  score beside its raw similarity**. That pairing is what makes "why did the agent not remember
  this?" answerable: you can tell "dropped because it was below the threshold" apart from "was never
  there".
- **摘要来源 (Summary sources)** opens a drawer listing the memories a summary was built from, each
  with its role, agent and time. Click through to the original. A source whose row is gone is shown
  as a hole rather than quietly dropped.

**Two limits worth knowing.** The semantic search can only reach a memory among the **newest 500
embedded rows** of the project — an older memory is not ranked lower, it is simply not a candidate.
And the deep-recall reranking is **lexical, not semantic**: a summary worded differently from your
query will not be promoted by it. Both are recorded in `docs/implementation/STATUS.md` §0m1 and
`docs/adr/0014-*.md`.

---

## 15. Your data, backups and restore

### 15.1 Where your data lives

Everything is under your Windows user profile:

```text
%AppData%\InfiniteAtelier\
├─ app.db          the SQLite database: projects, canvases, drama data, memories, runs
├─ files\          content-addressed media and generated images
├─ temp\           transient work, including a staged restore
├─ logs\           application logs (redacted: no keys, no request bodies)
└─ snapshots\      automatic database snapshots taken before a schema migration
```

**API keys are not in any of these.** They live in the **Windows Credential Manager** under the
target namespace `InfiniteAtelier:provider:<id>`. The database stores only a **reference** naming
which credential a provider uses.

### 15.2 The two storage modes, and which one you are in

- **Desktop build** (the executable): the database above is the source of truth for everything. The
  browser's local storage is not written with new facts.
- **Browser development mode** (Vite, for development): there is no core, so the older
  browser-storage path applies and the studio's sections say the core is unavailable.

If you have an older project in browser storage, the canvas page offers **导入旧项目** to migrate it
into the database. That flow previews, confirms, imports in a single transaction (**a failure leaves
no half-imported project**), reports what it did, and **warns about anything it could not map
instead of dropping it silently**. Running it twice detects the first import and skips; you can
explicitly import a copy instead. Your original browser data is never modified.

### 15.3 Backup and restore — the honest state

**The core has a complete, tested backup and restore implementation. The user interface does not
expose it.**

What exists:

- **Ordinary backup v1** is a ZIP with a manifest, a database snapshot, the media files and
  checksums. It **contains no API key**, by construction rather than by filtering: the exporter never
  reads the credential store, the provider metadata query selects only non-secret columns, and the
  reader scans every entry for credential shapes and **refuses to restore an archive that contains
  one**.
- **Restore** validates the manifest, every path, every size, the total size, the compression ratio
  and every checksum **before touching your live data**. Everything is staged in a temporary
  directory and the swap is atomic.
- **A restore keeps your previous data.** After restoring, the displaced database and object store
  are held aside, and **you must restart the application** — the database is closed to move its file,
  and only a restart reopens the restored one. The previous state is the only copy of your work until
  you have looked at the restored projects and confirmed they are what you wanted. Only then is
  **discard** offered, and it is the single irreversible action in the whole restore.

What does not exist:

- **No screen calls any of it.** The commands are generated and reachable in principle
  (`BackupBinding` has `ExportBackup`, `PreviewBackup`, `RestoreBackup`, `DiscardBackupState` and
  `BackupStateHeld`, and they are bound into the desktop application), but **no component in
  `web/src` invokes any of them.** Searching the frontend for those five names finds only the
  generated binding files. This is a real gap in a shipping build, and it is stated here and in
  `docs/RELEASE_CHECKLIST.md` §8.2. **It is not recorded in `docs/implementation/STATUS.md`** —
  checked, not assumed — which is why this guide says it rather than pointing at a status entry
  that does not exist.
- **The 数据备份 tab in 配置 is a different feature.** It exports and restores **browser-local data**
  — canvas projects, browser assets, browser media and configuration — through a download and a file
  picker. In the desktop build, **domain facts live in the database, not in browser storage**, so
  that tab does not back up the studio's projects, storyboards, memories or run history. The tab's
  own description says the data is "仅保存在浏览器本地", which is accurate for what it does and
  misleading about what it does not.

**Until a screen exists, back up by copying the data directory** (`%AppData%\InfiniteAtelier`)
**while the application is closed.** That is a file copy, not a supported operation: it has no
checksum verification and no manifest, and copying `app.db` while the application is running can
capture a torn database. Copying the whole directory with the application shut down is safe.

### 15.4 Encrypted backups are not in this version

An ordinary backup contains no credentials, so encrypting one would protect your project **content** —
scripts, storyboards, media — not keys. The decision not to build it in v1 is recorded in
`docs/adr/0016-encrypted-sensitive-backups-not-in-v1.md`, and the accepted risk is stated there
plainly: **a backup file on a shared drive is readable by anyone who can read the file.** The
reasoning is that a password-protected archive has a password to lose, and a user who forgets it
holds a backup they cannot open — which is worse than the exposure it prevents, for the situation a
backup exists for. If you move backups off your machine, treat the file as sensitive.

### 15.5 Security posture, briefly

- Your API keys are in the OS credential store and are never returned to the interface.
- All provider requests are made by the core, never by the interface, so a key never enters the
  page. Requests go through a controlled HTTP client with a domain/IP policy, DNS pinning, redirect
  re-checking, TLS verification, timeouts and response size limits.
- Logs are redacted; request headers and bodies are not logged.
- Generated files are verified — the **sniffed content type**, not the provider's claim — before a
  job can be marked successful.
- The only external program the application ever runs is **ffmpeg**, by name, with structured
  arguments and no shell involved.

---

## 16. The free canvas, assets and jobs

### 16.1 我的画布 (Free canvas)

An infinite board with nodes and connections. On it you can:

- Add image, text, audio and video nodes.
- **Generate and edit images through the core** — text-to-image, image-to-image, multi-image
  references, plus crop, split, upscale, angle and mask-edit tools.
- Ask questions about an image with a vision model.
- Connect nodes; connections carry a relation type and a validation status.
- Undo, redo, multi-select, box-select, zoom, pan, minimap.
- Import and export canvas projects as ZIP files, and **导入旧项目** to migrate browser data.

**Video and audio nodes cannot be generated in this build** — see §13.2.

### 16.2 我的资产 (Assets)

The general asset library: image, text and video assets with search, tags, covers and pagination.
Export and import as a package. **This is separate from the drama studio's assets**, which live in
the project database and are managed through the studio's 角色/场景地点/道具 sections.

### 16.3 任务中心 (Job Centre)

Every generation job: status, progress, attempt count, error code, and result files. You can **pause
and resume the queue**, **cancel jobs in bulk**, and **retry only the failed ones**.

Jobs are **persistent**. If you close the application mid-job, the job is still there on the next
launch: a job with a remote ID resumes by **polling** rather than resubmitting — so a resumed job
does not get billed twice — and a job that cannot be resumed fails safely instead of running twice.

This is also where you diagnose a failed video or audio job: the status and the error code appear
here even though the section that submitted it could not have succeeded (§13).

---

## 17. Keyboard, accessibility and language

- The interface is keyboard-navigable, and dialogs manage focus.
- **Status is never conveyed by colour alone** — every status tag carries text as well, so the
  screens stay readable in greyscale and for colour-blind users.
- The interface is available in **Chinese and English**, following your selection in settings.
- Large lists paginate or virtualise rather than rendering everything at once.

---

## 18. Troubleshooting

| Symptom | Cause and what to do |
|---|---|
| "剧集工作室仅在桌面模式可用" | You are in a browser, not the desktop application. Open the studio in the desktop build. |
| A section shows `该分区尚未实现` | That section is genuinely not built. Nothing is wrong with your installation. |
| The studio lists no projects | The studio shows only `drama` projects. A free canvas is opened from 我的画布. |
| Export says the machine cannot export | `ffmpeg`/`ffprobe` are not on your `PATH`. Install ffmpeg. |
| Export refuses to run | A shot has no approved media, or the episode has no approved storyboard. The error names which. |
| A video or audio job fails | **Expected in this build.** See §13. |
| "该剧集还没有剧本版本" | The script pipeline has not produced one. The stage needs an approved skeleton and strategy first. |
| A gate says "尚未批准上游版本" | The previous stage has no approved version. Approve one in its version table. |
| A save is refused as stale | Another window changed the same row. Reload and reapply your edit. |
| The model's output was rejected | The output failed schema validation twice. The error names the rule. |
| A stage parks asking for a person | Two automatic revisions failed. That is the cap, not a bug — review and decide. |
| A restore told you to restart | Correct. The database was closed to be replaced. Restart, then check your projects before discarding the previous state. |
| The application shows an unknown-publisher warning | The binary is not code-signed in this build. See `docs/INSTALL_AND_SIGNING.md`. |

---

## 19. Where to go for the specification

If you want to know why something behaves the way it does, these are the authoritative documents:

| Question | Document |
|---|---|
| What the product is meant to do | `PRD.md` |
| What this build actually does, package by package | `docs/implementation/STATUS.md` |
| Long-lived technical decisions and their costs | `docs/adr/` |
| Long-lived decisions, and the release status | `docs/adr/` and `docs/RELEASE_CHECKLIST.md` |
| Security rules and the release blockers | `docs/SECURITY.md` |
| Acceptance criteria | `docs/ACCEPTANCE.md` |
| What will be built next | `docs/ROADMAP.md` |
| Installing and shipping | `docs/INSTALL_AND_SIGNING.md` |
| Third-party licences | `THIRD_PARTY_NOTICES.md`, `sbom/` |
