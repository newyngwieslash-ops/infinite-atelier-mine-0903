import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Input, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Mic, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { listJobs, submitAudioJob } from "@/services/desktop/jobs";
import { collectAudioJobResults, createAsset, getScriptStructure, listAssets, isDramaBindingsAvailable, listScriptVersions, listStoryEntities } from "@/services/desktop/drama";
import { isMediaBindingsAvailable, readTimeline } from "@/services/desktop/media";
import {
    assignCharacterVoice,
    clearCharacterVoice,
    isVoiceSurfaceAvailable,
    listCharacterVoices,
    resolveCharacterVoice,
} from "@/services/desktop/voices";
import { channelIdForModel, decodeModelSelection } from "@/services/desktop/model-selection";
import { useEffectiveConfig } from "@/stores/use-config-store";
import type { desktop } from "@/wailsjs/go/models";

/**
 * AudioSection is WP-11's audio surface: which shots have speech, and where a line's TTS is missing.
 *
 * # The reads this section picks from, and the one that arrived later
 *
 * The per-line STATE FR-080 asks for — "TTS Voice/Dialogue mapping" — WAS not reachable from this
 * build's binding, and the section said so rather than guessing; **WP-27 closed it**. Which voice a
 * character speaks with is now a stored fact (`internal/infrastructure/database/character_voices.go`)
 * and `ResolveCharacterVoice` answers it with its PROVENANCE, so this section shows whether a line's
 * voice came from the character's own cast or from the project's settings — the two are
 * indistinguishable by sound alone. What remains true of this comment is the rest of it: what IS
 * reachable is the script:
 *
 *  - `ListScriptVersions(episodeId)` returns the episode's script versions, newest first. It is the
 *    read that was missing, and an earlier version of this comment overstated the gap. The SCRIPT
 *    family was the one of four whose history had no BINDING: `ListStorySkeletonVersions` and
 *    `ListAdaptationStrategyVersions` have had theirs since WP-08, while the repository method behind
 *    the script family was unreachable from the webview. With no way to enumerate versions, this
 *    section asked the user to TYPE a dialogue line id — which is what this picker replaces.
 *  - `GetScriptStructure(scriptVersionId)` returns a version's scenes with their `dialogueLines`,
 *    each carrying `lineId`, `type`, `characterEntityId` and `text`. This read has always existed,
 *    so the claim that "no method lists a script version's dialogue lines" was FALSE. The version
 *    LIST was the only thing missing.
 *  - There is NO method that reads a line's approved audio. The audio join lives in the timeline
 *    read's audio column, which is per SHOT ("whether audio is approved for any line in this row's
 *    scene"), so the status table above reports at that granularity and invents no per-line status.
 *
 * # The dialogue line the request names
 *
 * `SubmitAudioJob` requires a non-empty `dialogueLineId` — the core refuses the request without one
 * — and it is the job's entity. The line is CHOSEN here rather than typed: the picker offers the
 * selected version's lines that a person hears, filtered by the domain's own rule
 * (`IsSpoken` in `internal/domain/media/subtitle.go`: "dialogue" or "narration"), and choosing one
 * fills the id and the text the request carries.
 *
 * The line ID carries the character, which is why no character field is sent: `SubmitAudioJobRequest`
 * has none, and the core resolves the character from the line. The character shown beside the picker
 * is a caption derived from the same line, not a second field.
 */
export type AudioSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

/** The job types this section lists: one line's speech, and nothing else. */
const AUDIO_JOB_TYPES = ["audio_generation"];

/**
 * SPOKEN_LINE_TYPES is the domain's IsSpoken rule (`internal/domain/media/subtitle.go`).
 *
 * It is written out here rather than inferred from the structure, because a picker that offered
 * action lines would let a user submit a TTS job for a line nobody hears. The subtitle service
 * applies the same two values when it decides which lines a cue must cover, so a line offered here
 * and a line counted as missing there are the same set.
 */
const SPOKEN_LINE_TYPES = ["dialogue", "narration"] as const;

/** A spoken line flattened out of its scenes, which is the shape a picker needs. */
type SpokenLine = {
    lineId: string;
    type: string;
    characterEntityId: string;
    text: string;
    /** The scene's own label, so two lines with the same text are distinguishable. */
    sceneLabel: string;
};

/** flattenSpokenLines pulls the lines a person hears out of a structure, in scene then line order. */
function flattenSpokenLines(structure: desktop.ScriptStructureDTO): SpokenLine[] {
    const out: SpokenLine[] = [];
    for (const scene of structure.scenes) {
        const label = scene.slugline || scene.sceneNumber || String(scene.ordinal);
        for (const line of scene.dialogueLines) {
            if (!(SPOKEN_LINE_TYPES as readonly string[]).includes(line.type)) continue;
            out.push({
                lineId: line.lineId,
                type: line.type,
                characterEntityId: line.characterEntityId ?? "",
                text: line.text,
                sceneLabel: label,
            });
        }
    }
    return out;
}

/**
 * audioAssetFor finds or creates the asset a role's versions belong to.
 *
 * # Why one asset per role rather than one per line
 *
 * Every take of a line is a VERSION of the same idea, which is what makes re-recording it a new version
 * instead of a second object — and it is what `AttachJobResult` expects, since it takes an asset id and
 * returns a version. One asset for all speech and another for all effects is the coarsest arrangement
 * that still tells the two apart, and it matches what the asset list can be searched by: `ListAssets`
 * filters on TYPE, and every one of these is an `audio` asset.
 *
 * The name carries the role so a user browsing the library sees which is which rather than two rows
 * called "audio".
 */
async function audioAssetFor(projectId: string, role: string): Promise<string> {
    const label = role === "audio_effect" ? "Sound effects" : "Line speech";
    const existing = await listAssets({ projectId, types: ["audio"] } as never);
    const match = existing.find((record) => record.name === label);
    if (match) return match.id;
    const created = await createAsset({ projectId, type: "audio", name: label } as never);
    return created.id;
}

export function AudioSection({ projectId, episodes, activeEpisodeId, onSelectEpisode, onChanged }: AudioSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const config = useEffectiveConfig();

    const [timeline, setTimeline] = useState<desktop.TimelineDTO | null>(null);
    const [jobs, setJobs] = useState<desktop.JobDTO[]>([]);
    const [loading, setLoading] = useState(false);
    const [refreshing, setRefreshing] = useState(false);
    const [error, setError] = useState("");
    const [selectedShotId, setSelectedShotId] = useState("");
    /**
     * The script history and the structure of the version picked from it.
     *
     * `versions` is `null` until the read has answered, so "could not read" and "the episode has no
     * script version yet" stay distinguishable — the same rule the timeline section keeps.
     */
    const [versions, setVersions] = useState<desktop.ScriptVersionDTO[] | null>(null);
    const [versionId, setVersionId] = useState("");
    const [structure, setStructure] = useState<desktop.ScriptStructureDTO | null>(null);
    const [structureLoading, setStructureLoading] = useState(false);
    const [lineId, setLineId] = useState("");
    const [entities, setEntities] = useState<desktop.StoryEntityDTO[]>([]);
    const [text, setText] = useState("");
    const [submitting, setSubmitting] = useState(false);
    /**
     * The project's casting decisions, and the draft the casting form holds.
     *
     * The draft is separate from the list because the form is a form: a user typing a voice name has
     * not decided yet, and writing through on every keystroke would make the mapping table a log of
     * half-typed words.
     */
    const [voices, setVoices] = useState<desktop.CharacterVoiceDTO[]>([]);
    const [castCharacterId, setCastCharacterId] = useState("");
    const [castVoice, setCastVoice] = useState("");
    const [casting, setCasting] = useState(false);
    /**
     * The job whose result is being attached, and the role it is attached as.
     *
     * The role is the USER's choice rather than a guess from the job's text: a line of speech and a
     * sound effect are both audio jobs, and which one this is decides how the clip is layered — a bed at
     * 0.35, an effect at unity, a line placed at its shot.
     */
    const [attaching, setAttaching] = useState("");
    /**
     * The resolved voice for the picked line, and its PROVENANCE.
     *
     * The source is carried rather than inferred from which of the two inputs matched, because the
     * resolution has three levels and the levels are invisible without it: a user who hears the
     * project's default on a character they cast needs to see that the cast is not what decided.
     */
    const [lineVoice, setLineVoice] = useState<desktop.VoiceChoiceDTO | null>(null);

    const activeEpisode = useMemo(() => episodes.find((episode) => episode.id === activeEpisodeId) || null, [episodes, activeEpisodeId]);

    const bindingsAvailable = isMediaBindingsAvailable();
    /**
     * The script reads below go through the DRAMA binding, which a media-only build does not carry.
     *
     * That is a third availability question, and it is asked here rather than folded into
     * `bindingsAvailable` because the answers differ: a build with both surfaces is the normal case,
     * and a build with the media surface alone can still show the shot table while the version picker
     * has nothing to read. In that case the section says WHICH read is missing instead of reporting
     * "no script version", which would be a claim about the episode's content drawn from an absent
     * binding.
     */
    const scriptBindingsAvailable = isDramaBindingsAvailable();
    /**
     * The casting surface is a FOURTH availability question, and it is asked separately for the same
     * reason as the third: a build with the media and drama surfaces but without the drama STORE
     * answers empty for the voice list and refuses an assignment, and reporting that as "no cast"
     * would be a claim about the project drawn from an absent table.
     */
    const voiceSurfaceAvailable = isVoiceSurfaceAvailable();

    const reload = useCallback(async () => {
        if (!activeEpisodeId) {
            setTimeline(null);
            setJobs([]);
            setVersions(null);
            return;
        }
        setLoading(true);
        setError("");
        try {
            const [read, all] = await Promise.all([readTimeline({ episodeId: activeEpisodeId }), listJobs({ limit: 200 })]);
            setTimeline(read);
            // The project filter is applied here: ListJobsRequest has no project field — it is
            // {statuses, activeOnly, limit, offset} — so a comment claiming the core scoped this
            // would be false.
            setJobs(all.filter((job) => job.projectId === projectId && AUDIO_JOB_TYPES.includes(job.jobType)));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.audio.loadFailed"));
            setTimeline(null);
            setJobs([]);
        }
        // The script history is read OUTSIDE the block above, for two reasons that both matter. It is
        // a DRAMA read while the pair above are media reads, so a failure here must not null a
        // timeline that answered — a user would lose a working shot list to a picker. And it is read
        // only when its own binding is present: the query answers `[]` for an absent binding, and an
        // empty list ("this episode has no script version") is a different fact from a missing
        // binding ("this build cannot read the script"), which one `[]` cannot tell apart.
        if (!scriptBindingsAvailable) {
            setVersions(null);
        } else {
            try {
                setVersions(await listScriptVersions(activeEpisodeId));
            } catch (failure) {
                setVersions(null);
                // The first failure on screen wins: the media block above has already reported the
                // more consequential problem if there was one.
                setError((current) => current || (failure instanceof Error ? failure.message : t("studio.audio.versionsUnread")));
            }
        }
        setLoading(false);
    }, [activeEpisodeId, projectId, scriptBindingsAvailable, t]);

    useEffect(() => {
        void reload();
    }, [reload]);

    /**
     * The project's entities, read so a line's character can be NAMED rather than shown as a uuid.
     *
     * It is a separate, best-effort read rather than a fourth member of `reload`'s `Promise.all`,
     * because it is a caption rather than a fact this section acts on: a project whose entities
     * cannot be read still has script versions and lines, and the detail line below falls back to
     * the identifier instead of the section failing. The read itself answers `[]` when the drama
     * binding is absent, so a media-only build degrades the same way.
     */
    useEffect(() => {
        if (!projectId || !scriptBindingsAvailable) {
            setEntities([]);
            return;
        }
        let cancelled = false;
        void listStoryEntities(projectId)
            .then((found) => {
                if (!cancelled) setEntities(found);
            })
            .catch(() => {
                if (!cancelled) setEntities([]);
            });
        return () => {
            cancelled = true;
        };
    }, [projectId, scriptBindingsAvailable]);

    /**
     * The project's casting decisions, read on the same best-effort terms as the entities.
     *
     * A failure here leaves the list empty rather than failing the section: the shot table, the
     * version picker and the submit button all work without a cast, and a user who cannot read the
     * mapping has lost a convenience rather than the ability to render a line.
     */
    const reloadVoices = useCallback(async () => {
        if (!projectId || !voiceSurfaceAvailable) {
            setVoices([]);
            return;
        }
        try {
            setVoices(await listCharacterVoices(projectId));
        } catch {
            setVoices([]);
        }
    }, [projectId, voiceSurfaceAvailable]);

    useEffect(() => {
        void reloadVoices();
    }, [reloadVoices]);

    /**
     * loadStructure reads the picked version's lines.
     *
     * Everything below the picker is reset with it: a line id from the previous version would name
     * a line this version may not have, and submitting it would create a job about a line the user
     * is no longer looking at.
     */
    const loadStructure = useCallback(
        async (nextVersionId: string) => {
            setVersionId(nextVersionId);
            setLineId("");
            setText("");
            setStructure(null);
            if (!nextVersionId) return;
            setStructureLoading(true);
            try {
                setStructure(await getScriptStructure(nextVersionId));
            } catch (failure) {
                setError(failure instanceof Error ? failure.message : t("studio.audio.structureFailed"));
                setStructure(null);
            } finally {
                setStructureLoading(false);
            }
        },
        [t],
    );

    // The newest version is not selected automatically, and that is the choice the version's own
    // status makes for us: `listScriptVersions` returns newest first, but the newest is not
    // necessarily the one in force, and a TTS job submitted against a draft would be work for a
    // script nobody approved. The picker states which status each version carries and lets the user
    // decide — the same ruling the subtitle section's draft picker makes, except that its picker has
    // an approved version to prefer and this one has no equivalent rule: a voice is recorded line by
    // line, and a user re-recording one line of a draft is an ordinary thing to do.
    useEffect(() => {
        if (versions === null) return;
        // A version that has gone (or an episode switch) clears the selection AND its structure,
        // rather than leaving lines on screen that belong to another episode's script.
        if (versionId && !versions.some((version) => version.id === versionId)) {
            void loadStructure("");
        }
    }, [versions, versionId, loadStructure]);

    /** refreshJobs re-reads only the job list: a TTS job's status is the thing that changes. */
    const refreshJobs = useCallback(async () => {
        setRefreshing(true);
        try {
            const all = await listJobs({ limit: 200 });
            setJobs(all.filter((job) => job.projectId === projectId && AUDIO_JOB_TYPES.includes(job.jobType)));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.audio.loadFailed"));
        } finally {
            setRefreshing(false);
        }
    }, [projectId, t]);

    const shots = timeline?.shots ?? [];
    const withoutAudio = shots.filter((shot) => !shot.hasAudio).length;
    const spokenLines = useMemo(() => (structure ? flattenSpokenLines(structure) : []), [structure]);

    /**
     * chooseLine fills the request from a picked line.
     *
     * The text is filled rather than fixed: TTS is billed by the character and a line's wording is
     * sometimes adjusted before synthesis (a name's pronunciation, a number spelled out), so the
     * field stays editable and this only seeds it.
     *
     * Only the line ID is remembered as state. `SubmitAudioJobRequest` has no character field — the
     * core resolves the character FROM the line, which is what AC-MEDIA-002's "audio linked to
     * character/line" means — so the character below is derived from the picked line for display
     * rather than stored as a second copy that could drift from it.
     */
    const chooseLine = (picked: string) => {
        const line = spokenLines.find((candidate) => candidate.lineId === picked);
        setLineId(picked);
        setText(line?.text ?? "");
    };

    /** The picked line, which is what supplies the scene and character the detail line shows. */
    const chosenLine = useMemo(() => spokenLines.find((line) => line.lineId === lineId) ?? null, [spokenLines, lineId]);

    /**
     * The picked line's voice, read whenever the line or the cast changes.
     *
     * It is a READ rather than the submit path's own resolution, so the caption below the picker and
     * the request the button sends cannot disagree — the defect shape a second implementation of the
     * same rule would produce.
     */
    useEffect(() => {
        const characterId = chosenLine?.characterEntityId ?? "";
        if (!projectId || !lineId || !chosenLine) {
            setLineVoice(null);
            return;
        }
        let cancelled = false;
        void resolveCharacterVoice({
            projectId,
            characterEntityId: characterId,
            projectVoice: config.audioVoice ?? "",
            projectModel: config.audioModel ?? "",
        })
            .then((resolved) => {
                if (!cancelled) setLineVoice(resolved);
            })
            .catch(() => {
                if (!cancelled) setLineVoice(null);
            });
        return () => {
            cancelled = true;
        };
        // `voices` is a dependency because casting a character changes the answer for that character's
        // line, and the caption must follow the cast rather than the configuration it replaced.
    }, [projectId, lineId, chosenLine, config.audioVoice, config.audioModel, voices]);

    /**
     * attachJobResult turns a succeeded audio job's result into the version the mix reads.
     *
     * # Why the user must press this
     *
     * Three facts are the caller's to state and cannot all be inferred: WHICH asset the version belongs
     * to, WHICH shot consumes it, and WHETHER the audio is a line's speech or a sound effect. The job
     * names a dialogue LINE, and a line is not a shot — so the shot comes from the section's own
     * selection, and a section with no shot selected says so rather than guessing.
     *
     * # The asset
     *
     * One asset per role, reused across lines: a line's speech and a sound effect are different things to
     * a reader, and every line's take is a VERSION of the same idea rather than an asset of its own — which
     * is what makes re-recording a line a new version instead of a second object. The asset is created on
     * first use.
     */
    const attachJob = async (jobID: string, role: string) => {
        if (!selectedShotId) {
            message.error(t("studio.audio.attachNoShot"));
            return;
        }
        setAttaching(jobID);
        try {
            const assetID = await audioAssetFor(projectId, role);
            const collected = await collectAudioJobResults({
                assetByJob: { [jobID]: assetID },
                jobIds: [jobID],
                usageRole: role,
                consumerType: "shot",
                consumerId: selectedShotId,
            } as never);
            if (collected.length === 0) {
                message.error(t("studio.audio.attachNothing"));
                return;
            }
            message.success(t(collected[0].duplicate ? "studio.audio.attachAlready" : "studio.audio.attached"));
            await reload();
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.audio.attachFailed"));
        } finally {
            setAttaching("");
        }
    };

    /** castVoice assigns the drafted voice to the picked character. */
    const castCharacter = async () => {
        if (!castCharacterId || castVoice.trim() === "") {
            return;
        }
        setCasting(true);
        try {
            // The revision the panel is showing is sent, so a second tab that saved first wins and this
            // one is told to reload rather than silently overwriting it.
            const existing = voices.find((voice) => voice.characterEntityId === castCharacterId);
            await assignCharacterVoice({
                projectId,
                characterEntityId: castCharacterId,
                voice: castVoice.trim(),
                expectedRevision: existing?.revision ?? 0,
            } as never);
            message.success(t("studio.audio.castSaved"));
            setCastVoice("");
            await reloadVoices();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.audio.castFailed"));
        } finally {
            setCasting(false);
        }
    };

    /** removeCast clears a character's casting decision. */
    const removeCast = async (characterEntityId: string) => {
        try {
            await clearCharacterVoice({ projectId, characterEntityId } as never);
            await reloadVoices();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.audio.castFailed"));
        }
    };


    /**
     * submit sends one line's TTS request.
     *
     * The voice, format and speed come from the project's own audio configuration — the same
     * values the canvas's speech panel sends — and the model selection is DECODED, because the
     * drawer stores it as `"<channelId>::<model>"` while the Go gateway wants a provider id and a
     * bare model name.
     */
    const submit = async () => {
        if (!activeEpisode) return;
        const selection = config.audioModel || config.model;
        const decoded = decodeModelSelection(selection);
        const channelId = channelIdForModel(config, selection);
        if (!decoded.model || !channelId) {
            message.error(t("studio.audio.modelRequired"));
            return;
        }
        if (lineId.trim() === "") {
            // The core refuses a request with no dialogue line, so the form asks rather than
            // sending one the core would reject.
            message.error(t("studio.audio.lineRequired"));
            return;
        }
        if (text.trim() === "") {
            message.error(t("studio.audio.textRequired"));
            return;
        }
        setSubmitting(true);
        try {
            // THE VOICE IS RESOLVED, not taken from the settings wholesale. The character's own cast
            // wins when there is one, the project's configuration settles it otherwise, and an unset
            // choice sends NO voice field at all so the provider applies its own default — which is
            // the honest outcome for a project that has stated nothing. Sending `config.audioVoice`
            // directly, as this did before casting existed, is what made every character in a project
            // sound the same.
            const resolved = await resolveCharacterVoice({
                projectId,
                characterEntityId: chosenLine?.characterEntityId ?? "",
                projectVoice: config.audioVoice ?? "",
                projectModel: decoded.model,
                projectProvider: channelId,
            });
            const job = await submitAudioJob({
                projectId,
                episodeId: activeEpisode.id,
                dialogueLineId: lineId.trim(),
                providerId: resolved.providerConfigId || channelId,
                model: resolved.model || decoded.model,
                text: text.trim(),
                voice: resolved.isSet ? resolved.voice : undefined,
                format: config.audioFormat || undefined,
                speed: config.audioSpeed || undefined,
            } as never);
            message.success(t("studio.audio.submitted", { job: job.id.slice(0, 8) }));
            setText("");
            await refreshJobs();
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.audio.submitFailed"));
        } finally {
            setSubmitting(false);
        }
    };

    const columns: ColumnsType<desktop.TimelineShotDTO> = [
        { title: t("studio.audio.ordinal"), dataIndex: "ordinal", key: "ordinal", width: 70 },
        {
            title: t("studio.audio.duration"),
            dataIndex: "durationMs",
            key: "duration",
            width: 110,
            render: (value: number) => (value / 1000).toFixed(1),
        },
        {
            title: t("studio.audio.stateLabel"),
            key: "state",
            render: (_value, row) => (row.hasAudio ? <Tag color="green">{t("studio.audio.hasAudio")}</Tag> : <Tag color="orange">{t("studio.audio.noAudio")}</Tag>),
        },
        {
            title: t("studio.audio.cuesLabel"),
            dataIndex: "cueCount",
            key: "cueCount",
            width: 120,
        },
        {
            title: "",
            key: "actions",
            width: 150,
            render: (_value, row) => (
                <Button size="small" type={row.shotId === selectedShotId ? "primary" : "default"} data-testid={`studio-audio-select-${row.shotId}`} onClick={() => setSelectedShotId(row.shotId)}>
                    {t("studio.audio.selectShot")}
                </Button>
            ),
        },
    ];

    if (episodes.length === 0) {
        return <Empty description={t("studio.audio.selectEpisode")} />;
    }

    // The SHELL's notice is about the drama binding. A build with the drama surface and no media
    // surface reaches this body, and every read below answers empty — which would render "the episode
    // has no shots" about a question nobody asked. The section carries its own guard for that case.
    if (!bindingsAvailable) {
        return <Alert type="info" showIcon data-testid="studio-audio-no-media-core" message={t("studio.audio.noMediaCoreTitle")} description={t("studio.audio.noMediaCoreBody")} />;
    }

    return (
        <div className="space-y-8" data-testid="studio-audio">
            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.audio.shots")}</h2>
                    <Space wrap>
                        <Select
                            className="min-w-56"
                            value={activeEpisodeId || undefined}
                            placeholder={t("studio.audio.selectEpisode")}
                            data-testid="studio-audio-episode"
                            onChange={onSelectEpisode}
                            options={episodes.map((episode) => ({
                                value: episode.id,
                                label: `S${episode.seasonNumber}E${episode.episodeNumber} · ${episode.title}`,
                            }))}
                        />
                        <Button icon={<RefreshCw className="size-4" />} loading={loading} data-testid="studio-audio-reload" onClick={() => void reload()}>
                            {t("studio.audio.reload")}
                        </Button>
                    </Space>
                </div>

                {/* The scope of the answer, stated rather than implied: this section reports
                    audio at the SHOT's granularity because the timeline's audio column is the only
                    read of it this build exposes. A per-line list would be a second answer nobody
                    computed. */}
                <Alert className="mb-3" type="info" showIcon message={t("studio.audio.scopeTitle")} description={t("studio.audio.scopeBody")} data-testid="studio-audio-scope" />

                {error ? <Alert className="mb-3" type="error" showIcon message={error} data-testid="studio-audio-error" /> : null}

                {shots.length === 0 && !loading ? (
                    <Empty description={t("studio.audio.noShots")} />
                ) : (
                    <Table<desktop.TimelineShotDTO>
                        rowKey="itemId"
                        size="small"
                        loading={loading}
                        pagination={false}
                        columns={columns}
                        dataSource={shots}
                        data-testid="studio-audio-shots"
                        rowClassName={(row) => (row.shotId === selectedShotId ? "bg-stone-50 dark:bg-stone-900" : "")}
                    />
                )}

                {timeline && shots.length > 0 ? <Typography.Paragraph className="mt-2 text-xs text-stone-500">{t("studio.audio.totals", { total: shots.length, missing: withoutAudio })}</Typography.Paragraph> : null}
            </section>

            <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <h2 className="text-base font-medium">{t("studio.audio.requestTitle")}</h2>
                <Typography.Paragraph className="mt-1 text-xs text-stone-500">{t("studio.audio.requestHint")}</Typography.Paragraph>

                {!scriptBindingsAvailable ? (
                    <Alert className="mb-3" type="info" showIcon data-testid="studio-audio-no-script-core" message={t("studio.audio.noScriptCoreTitle")} description={t("studio.audio.noScriptCoreBody")} />
                ) : versions !== null && versions.length === 0 ? (
                    // The reason AND the step that produces one: a disabled control with no
                    // explanation reads as a broken build, and the version a TTS request needs comes
                    // from a stage the user can go and run.
                    <Alert className="mb-3" type="info" showIcon data-testid="studio-audio-no-version" message={t("studio.audio.noVersionTitle")} description={t("studio.audio.noVersionBody")} />
                ) : null}

                <Space wrap align="end">
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.audio.shotLabel")}</span>
                        <Select
                            className="min-w-48"
                            value={selectedShotId || undefined}
                            placeholder={t("studio.audio.selectShot")}
                            data-testid="studio-audio-shot"
                            onChange={(value: string) => setSelectedShotId(value)}
                            options={shots.map((shot) => ({
                                value: shot.shotId,
                                label: `#${shot.ordinal} · ${shot.hasAudio ? t("studio.audio.hasAudio") : t("studio.audio.noAudio")}`,
                            }))}
                        />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.audio.versionLabel")}</span>
                        <Select
                            className="min-w-56"
                            value={versionId || undefined}
                            placeholder={scriptBindingsAvailable ? t("studio.audio.selectVersion") : t("studio.audio.scriptCoreMissing")}
                            loading={loading}
                            disabled={!scriptBindingsAvailable || versions === null || versions.length === 0}
                            data-testid="studio-audio-script-version"
                            onChange={(value: string) => void loadStructure(value)}
                            options={(versions ?? []).map((version) => ({
                                value: version.id,
                                label: `v${version.versionNumber} · ${t(`studio.versionStatus.${version.status}`, { defaultValue: version.status })}`,
                            }))}
                        />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.audio.lineLabel")}</span>
                        <Select
                            className="min-w-80"
                            value={lineId || undefined}
                            placeholder={t("studio.audio.selectLine")}
                            loading={structureLoading}
                            disabled={!versionId || spokenLines.length === 0}
                            data-testid="studio-audio-line"
                            onChange={(value: string) => chooseLine(value)}
                            options={spokenLines.map((line) => ({
                                value: line.lineId,
                                label: `${t(`studio.lineType.${line.type}`, { defaultValue: line.type })} · ${line.text.slice(0, 24)}${line.text.length > 24 ? "…" : ""}`,
                            }))}
                        />
                    </label>
                    <Button type="primary" icon={<Mic className="size-4" />} loading={submitting} data-testid="studio-audio-submit" onClick={() => void submit()}>
                        {t("studio.audio.submit")}
                    </Button>
                </Space>

                {/* What the picked line is, said out loud: which scene it sits in and whose line it
                    is. The scene label and the character ID both come from the structure the core
                    returned — no second call decides them — and the ID is turned into a name only
                    through the project's own entity list. A line citing an entity this project does
                    not list keeps its raw ID on screen, which is the honest rendering: inventing a
                    name for it would be a claim about a fact nobody read. */}
                {versionId && structure ? (
                    <div className="mt-2 text-xs text-stone-500" data-testid="studio-audio-line-detail">
                        {spokenLines.length === 0 ? (
                            <span>{t("studio.audio.noSpokenLines")}</span>
                        ) : lineId ? (
                            <span>
                                {t("studio.audio.lineDetail", {
                                    scene: chosenLine?.sceneLabel ?? "—",
                                    character: characterName(chosenLine?.characterEntityId ?? "", entities) || t("studio.audio.noCharacter"),
                                })}
                            </span>
                        ) : (
                            <span>{t("studio.audio.lineHint", { count: spokenLines.length })}</span>
                        )}
                    </div>
                ) : null}

                <label className="mt-3 block">
                    <span className="mb-1 block text-sm">{t("studio.audio.textLabel")}</span>
                    <Input.TextArea rows={3} value={text} maxLength={2000} data-testid="studio-audio-text" placeholder={t("studio.audio.textPlaceholder")} onChange={(event) => setText(event.target.value)} />
                </label>
                {/* WHICH VOICE THIS LINE RENDERS IN, and where it came from.
                    The three sources are indistinguishable by sound alone, so the caption names the
                    one that decided: a user who hears the project's default on a character they cast
                    would otherwise have no way to see that the cast is not what applied. */}
                {lineId && chosenLine ? (
                    <Typography.Paragraph className="mt-3 text-xs text-stone-500" data-testid="studio-audio-voice-source">
                        {lineVoice?.isSet
                            ? t(
                                  lineVoice.source === "character"
                                      ? "studio.audio.voiceFromCharacter"
                                      : "studio.audio.voiceFromProject",
                                  { voice: lineVoice.voice, format: config.audioFormat || "—" },
                              )
                            : t("studio.audio.voiceUnset", { format: config.audioFormat || "—" })}
                    </Typography.Paragraph>
                ) : (
                    <Typography.Paragraph className="mt-3 text-xs text-stone-500">{t("studio.audio.voiceNote", { voice: config.audioVoice || "—", format: config.audioFormat || "—" })}</Typography.Paragraph>
                )}
            </section>

            <section>
                <h2 className="mb-3 text-lg font-medium">{t("studio.audio.castTitle")}</h2>
                {/* The casting table is the answer to "who sounds like what", which before this
                    section did not exist: the voice travelled per submission and was stored nowhere,
                    so a project with two characters had one voice for both. */}
                {voices.length === 0 ? (
                    <Empty description={t("studio.audio.noCast")} />
                ) : (
                    <Table<desktop.CharacterVoiceDTO>
                        rowKey="characterEntityId"
                        size="small"
                        pagination={false}
                        dataSource={voices}
                        data-testid="studio-audio-cast-table"
                        columns={[
                            {
                                title: t("studio.audio.castCharacter"),
                                dataIndex: "characterEntityId",
                                key: "character",
                                render: (value: string, row) => characterName(value, entities) || row.characterName || value,
                            },
                            { title: t("studio.audio.castVoice"), dataIndex: "voice", key: "voice" },
                            {
                                title: t("studio.audio.castModel"),
                                key: "model",
                                render: (_value, row) => row.model || t("studio.audio.castInherited"),
                            },
                            {
                                title: "",
                                key: "actions",
                                render: (_value, row) => (
                                    <Button size="small" data-testid={`studio-audio-cast-clear-${row.characterEntityId}`} onClick={() => void removeCast(row.characterEntityId)}>
                                        {t("studio.audio.castClear")}
                                    </Button>
                                ),
                            },
                        ]}
                    />
                )}
                {/* The form is always offered, so a project with no cast yet has a way to start rather
                    than only a list that says it is empty. */}
                <Space wrap className="mt-3">
                    <Select
                        className="min-w-48"
                        value={castCharacterId || undefined}
                        placeholder={t("studio.audio.castCharacterPlaceholder")}
                        data-testid="studio-audio-cast-character"
                        onChange={(value: string) => setCastCharacterId(value)}
                        options={entities
                            .filter((entity) => entity.type === "character")
                            .map((entity) => ({ value: entity.id, label: entity.canonicalName || entity.id }))}
                    />
                    <Input
                        className="min-w-40"
                        value={castVoice}
                        placeholder={t("studio.audio.castVoicePlaceholder")}
                        data-testid="studio-audio-cast-voice"
                        onChange={(event) => setCastVoice(event.target.value)}
                    />
                    <Button type="primary" loading={casting} disabled={!castCharacterId || castVoice.trim() === ""} data-testid="studio-audio-cast-save" onClick={() => void castCharacter()}>
                        {t("studio.audio.castSave")}
                    </Button>
                </Space>
                <Typography.Paragraph className="mt-2 text-xs text-stone-500">{t("studio.audio.castNote")}</Typography.Paragraph>
            </section>

            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.audio.jobs")}</h2>
                    <Button size="small" icon={<RefreshCw className="size-4" />} loading={refreshing} data-testid="studio-audio-refresh-jobs" onClick={() => void refreshJobs()}>
                        {t("studio.audio.refresh")}
                    </Button>
                </div>
                {jobs.length === 0 ? (
                    <Empty description={t("studio.audio.noJobs")} />
                ) : (
                    <Table<desktop.JobDTO>
                        rowKey="id"
                        size="small"
                        pagination={false}
                        dataSource={jobs}
                        data-testid="studio-audio-jobs"
                        columns={[
                            { title: t("studio.audio.jobId"), dataIndex: "id", key: "id", width: 120, render: (value: string) => <code className="text-xs">{value.slice(0, 8)}</code> },
                            { title: t("studio.audio.lineLabel"), dataIndex: "entityId", key: "entityId", render: (value: string) => <code className="text-xs">{value.slice(0, 16) || "—"}</code> },
                            {
                                title: t("studio.audio.jobStatus"),
                                dataIndex: "status",
                                key: "status",
                                width: 160,
                                render: (value: string) => <Tag color={audioStatusColour(value)}>{t(`jobs.status.${value}`, { defaultValue: value })}</Tag>,
                            },
                            { title: t("studio.audio.jobError"), dataIndex: "errorCode", key: "errorCode", width: 160, render: (value?: string) => value || "—" },
                            {
                                // THE STEP THAT MAKES GENERATED SPEECH AUDIBLE. A job's bytes were
                                // committed and the job succeeded — and nothing turned the result into the
                                // version a mix reads, so the speech a user generated never reached the
                                // film. The role is the user's choice because a line and an effect are
                                // both audio jobs and only the caller knows which this is.
                                title: "",
                                key: "attach",
                                width: 200,
                                render: (_value, row) =>
                                    row.status === "succeeded" && row.resultFiles && row.resultFiles.length > 0 ? (
                                        <Space size="small">
                                            <Button size="small" loading={attaching === row.id} disabled={!selectedShotId} data-testid={`studio-audio-attach-line-${row.id}`} onClick={() => void attachJob(row.id, "audio_dialogue")}>
                                                {t("studio.audio.attachLine")}
                                            </Button>
                                            <Button size="small" loading={attaching === row.id} disabled={!selectedShotId} data-testid={`studio-audio-attach-effect-${row.id}`} onClick={() => void attachJob(row.id, "audio_effect")}>
                                                {t("studio.audio.attachEffect")}
                                            </Button>
                                        </Space>
                                    ) : null,
                            },
                            {
                                title: t("studio.audio.jobFiles"),
                                key: "files",
                                render: (_value, row) =>
                                    row.resultFiles && row.resultFiles.length > 0 ? (
                                        <ul className="space-y-0.5">
                                            {row.resultFiles.map((file) => (
                                                <li key={file.storageKey} className="text-xs">
                                                    <code>{file.storageKey.slice(0, 12)}…</code> <span className="text-stone-500">{file.mime}</span>
                                                </li>
                                            ))}
                                        </ul>
                                    ) : (
                                        <span className="text-xs text-stone-500">—</span>
                                    ),
                            },
                        ]}
                    />
                )}
            </section>
        </div>
    );
}

/**
 * characterName resolves a line's `characterEntityId` against the project's entities.
 *
 * It returns the empty string rather than the identifier when nothing matches, so the caller
 * decides what to show: a name when there is one, the core's own identifier when the entity list
 * does not carry it, and a "no character" note when the line names none at all. `narration` is the
 * common case for the last of those — a narrator's line is not attributed to a character — and the
 * three cases are different facts about the line rather than one fallback.
 *
 * A DELETED entity is still resolved: `deletedAt` is a soft delete, and a line whose character was
 * removed still names an id a reader needs to see identified rather than as a bare uuid.
 */
function characterName(characterEntityId: string, entities: desktop.StoryEntityDTO[]): string {
    if (characterEntityId.trim() === "") return "";
    const found = entities.find((entity) => entity.id === characterEntityId);
    return found ? found.canonicalName : characterEntityId;
}

/** audioStatusColour maps a job status to a tag colour. Text always accompanies it. */
function audioStatusColour(status: string): string {
    switch (status) {
        case "succeeded":
            return "green";
        case "failed":
        case "orphaned":
            return "red";
        case "cancelled":
            return "default";
        case "remote_only":
            return "gold";
        default:
            return "blue";
    }
}
