import { useState } from "react";
import { Alert, App, Button, Input, InputNumber, Modal, Select, Space, Steps } from "antd";
import { useTranslation } from "react-i18next";

import { isDramaBindingsAvailable } from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";

/**
 * DramaCreateWizard creates a drama project with its FR-020 settings.
 *
 * Two steps, following the migrate dialog's approach: a stage state rather than
 * an antd Form, because the create call is one command with a flat request and a
 * form library would add a second source of truth for values this component
 * already holds.
 *
 * Three rules it keeps:
 *
 *  1. The bindings are checked before the request is built. In a browser session
 *     there is no core to write to, and a command that silently did nothing would
 *     leave the user believing a project exists.
 *  2. The name is validated here as well as in Go. The core refuses an empty name
 *     with a domain error, but the wizard must not submit one in the first place,
 *     because that turns a form mistake into a round-trip.
 *  3. The fields are the flat ones on `desktop.CreateProjectRequest`. FR-020's
 *     settings are not nested under a `settings` object on the request — Go reads
 *     them only when `projectType` is "drama".
 */

// The binding is imported lazily through a cached module promise, the same
// pattern `drama.ts` uses: a browser build has no Wails runtime, and an eager
// import would fail there.
let projectsModule: Promise<typeof import("@/wailsjs/go/desktop/ProjectsBinding")> | undefined;

async function loadProjectsBinding() {
    projectsModule ??= import("@/wailsjs/go/desktop/ProjectsBinding").catch((error: unknown) => {
        projectsModule = undefined;
        throw error;
    });
    return projectsModule;
}

type Step = 0 | 1;

/** The FR-020 fields the wizard collects, before they become a request. */
type Draft = {
    name: string;
    language: string;
    targetPlatform: string;
    aspectRatio: string;
    resolution: string;
    expectedEpisodeCount: number | null;
    defaultEpisodeDurationSecs: number | null;
    audience: string;
    contentRating: string;
    adaptationMode: string;
};

const EMPTY_DRAFT: Draft = {
    name: "",
    language: "zh-CN",
    targetPlatform: "",
    aspectRatio: "",
    resolution: "",
    expectedEpisodeCount: null,
    defaultEpisodeDurationSecs: null,
    audience: "",
    contentRating: "",
    // "balanced" is `DefaultSettings`' value in the Go domain, so the wizard's
    // untouched state and a project created without a mode agree.
    adaptationMode: "balanced",
};

export type DramaCreateWizardProps = {
    open: boolean;
    onClose: () => void;
    /** Receives the id of the project the core created. */
    onCreated: (projectId: string) => void;
};

export function DramaCreateWizard({ open, onClose, onCreated }: DramaCreateWizardProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [step, setStep] = useState<Step>(0);
    const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);
    const [submitting, setSubmitting] = useState(false);

    const close = () => {
        setStep(0);
        setDraft(EMPTY_DRAFT);
        setSubmitting(false);
        onClose();
    };

    const patch = <K extends keyof Draft>(key: K, value: Draft[K]) => {
        setDraft((current) => ({ ...current, [key]: value }));
    };

    const nameValid = draft.name.trim() !== "";

    const submit = async () => {
        if (!nameValid) {
            // Belt and braces: the button is disabled without a name, and this
            // guards the Enter key and any future caller.
            message.error(t("studio.wizard.nameRequired"));
            return;
        }
        setSubmitting(true);
        try {
            // The core is checked before the request is built. `drama.ts` answers
            // "is the studio's surface present" from a representative method set;
            // this wizard additionally requires the one function it calls, because
            // a build with the drama binding and without the project binding would
            // otherwise fail with a type error rather than an explanation.
            if (!isDramaBindingsAvailable()) throw new Error(t("studio.desktopOnly.body"));
            const { CreateProject } = await loadProjectsBinding();
            if (typeof CreateProject !== "function") throw new Error(t("studio.desktopOnly.body"));
            const request: desktop.CreateProjectRequest = {
                name: draft.name.trim(),
                description: "",
                projectType: "drama",
                language: draft.language,
                targetPlatform: draft.targetPlatform.trim(),
                aspectRatio: draft.aspectRatio.trim(),
                resolution: draft.resolution.trim(),
                audience: draft.audience.trim(),
                contentRating: draft.contentRating.trim(),
                adaptationMode: draft.adaptationMode,
                // A cleared number input reads as null; the request field is an
                // int with `omitempty`, so an unanswered field is sent as 0,
                // which `Settings.Validate` accepts as "not stated".
                expectedEpisodeCount: draft.expectedEpisodeCount ?? 0,
                defaultEpisodeDurationSecs: draft.defaultEpisodeDurationSecs ?? 0,
            };
            const created = await CreateProject(request);
            message.success(t("studio.wizard.created"));
            onCreated(created.id);
            close();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.wizard.createFailed"));
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Modal
            title={t("studio.wizard.title")}
            open={open}
            centered
            width={640}
            onCancel={submitting ? undefined : close}
            maskClosable={!submitting}
            footer={
                <Space>
                    <Button onClick={close} disabled={submitting}>
                        {t("common.cancel")}
                    </Button>
                    {step === 1 ? (
                        <Button onClick={() => setStep(0)} disabled={submitting}>
                            {t("studio.wizard.back")}
                        </Button>
                    ) : null}
                    {step === 0 ? (
                        <Button type="primary" disabled={!nameValid} onClick={() => setStep(1)} data-testid="studio-wizard-next">
                            {t("studio.wizard.next")}
                        </Button>
                    ) : (
                        <Button type="primary" loading={submitting} onClick={() => void submit()} data-testid="studio-wizard-submit">
                            {t("studio.wizard.submit")}
                        </Button>
                    )}
                </Space>
            }
        >
            <div className="space-y-4">
                <Steps size="small" current={step} items={[{ title: t("studio.wizard.stepBasics") }, { title: t("studio.wizard.stepSettings") }]} />

                {step === 0 ? (
                    <div className="space-y-4">
                        <label className="block">
                            <span className="mb-1 block text-sm">{t("studio.wizard.name")}</span>
                            <Input
                                value={draft.name}
                                autoFocus
                                maxLength={200}
                                placeholder={t("studio.wizard.namePlaceholder")}
                                data-testid="studio-wizard-name"
                                onChange={(event) => patch("name", event.target.value)}
                                onPressEnter={() => nameValid && setStep(1)}
                            />
                            {!nameValid ? <span className="mt-1 block text-xs text-stone-500">{t("studio.wizard.nameRequired")}</span> : null}
                        </label>
                        <label className="block">
                            <span className="mb-1 block text-sm">{t("studio.wizard.language")}</span>
                            <Select
                                className="w-full"
                                value={draft.language}
                                data-testid="studio-wizard-language"
                                onChange={(value: string) => patch("language", value)}
                                options={[
                                    { value: "zh-CN", label: t("locale.zhCN") },
                                    { value: "en-US", label: t("locale.enUS") },
                                ]}
                            />
                            <span className="mt-1 block text-xs text-stone-500">{t("studio.wizard.languageHint")}</span>
                        </label>
                    </div>
                ) : (
                    <div className="space-y-4">
                        <Alert type="info" showIcon message={t("studio.wizard.stepSettings")} description={t("studio.sections.overview.description")} />
                        <div className="grid gap-4 sm:grid-cols-2">
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.targetPlatform")}</span>
                                <Select
                                    className="w-full"
                                    allowClear
                                    value={draft.targetPlatform === "" ? undefined : draft.targetPlatform}
                                    data-testid="studio-wizard-platform"
                                    placeholder={t("studio.wizard.targetPlatform")}
                                    onChange={(value?: string) => patch("targetPlatform", value ?? "")}
                                    options={[
                                        { value: "vertical_short", label: t("studio.wizard.platformVertical") },
                                        { value: "horizontal_web", label: t("studio.wizard.platformHorizontal") },
                                        { value: "cinema", label: t("studio.wizard.platformCinema") },
                                        { value: "tv", label: t("studio.wizard.platformTv") },
                                        { value: "web", label: t("studio.wizard.platformWeb") },
                                    ]}
                                />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.adaptationMode")}</span>
                                <Select
                                    className="w-full"
                                    value={draft.adaptationMode}
                                    data-testid="studio-wizard-adaptation"
                                    onChange={(value: string) => patch("adaptationMode", value)}
                                    options={[
                                        { value: "faithful", label: t("studio.wizard.modeFaithful") },
                                        { value: "balanced", label: t("studio.wizard.modeBalanced") },
                                        { value: "aggressive", label: t("studio.wizard.modeAggressive") },
                                    ]}
                                />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.aspectRatio")}</span>
                                <Input value={draft.aspectRatio} maxLength={500} placeholder="9:16" onChange={(event) => patch("aspectRatio", event.target.value)} />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.resolution")}</span>
                                <Input value={draft.resolution} maxLength={500} placeholder="1080x1920" onChange={(event) => patch("resolution", event.target.value)} />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.expectedEpisodeCount")}</span>
                                <InputNumber
                                    className="w-full"
                                    min={0}
                                    precision={0}
                                    value={draft.expectedEpisodeCount}
                                    data-testid="studio-wizard-episode-count"
                                    onChange={(value) => patch("expectedEpisodeCount", typeof value === "number" ? value : null)}
                                />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.defaultEpisodeDurationSecs")}</span>
                                <InputNumber
                                    className="w-full"
                                    min={0}
                                    precision={0}
                                    value={draft.defaultEpisodeDurationSecs}
                                    data-testid="studio-wizard-duration"
                                    onChange={(value) => patch("defaultEpisodeDurationSecs", typeof value === "number" ? value : null)}
                                />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.audience")}</span>
                                <Input value={draft.audience} maxLength={500} onChange={(event) => patch("audience", event.target.value)} />
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm">{t("studio.wizard.contentRating")}</span>
                                <Input value={draft.contentRating} maxLength={500} onChange={(event) => patch("contentRating", event.target.value)} />
                            </label>
                        </div>
                    </div>
                )}
            </div>
        </Modal>
    );
}
