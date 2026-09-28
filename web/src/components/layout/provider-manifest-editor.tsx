import { useEffect, useState } from "react";
import { Alert, Input, Modal, Space, Tag, Typography } from "antd";
import { useTranslation } from "react-i18next";

import { activateManifest, listManifestVersions, previewManifest, saveManifest } from "@/services/desktop/manifests";
import type { desktop } from "@/wailsjs/go/models";

/**
 * ProviderManifestEditor is RP-05.3's declarative manifest surface.
 *
 * # What it is, and what it deliberately is NOT
 *
 * A manifest is DATA the Go side validates and its adapters interpret — no
 * script, no evaluation, no plugin. The editor is therefore an import
 * preview + version history, NOT a JavaScript editor: a user pastes a JSON
 * document, the backend validates it through the domain, the preview shows
 * what would run (capability, paths under the configured base URL, headers),
 * and only an explicit save+activate puts it in force.
 *
 * # The flow, each step explicit
 *
 *   1. PREVIEW: the document is validated without storing. The domain's own
 *      error message comes back for a hostile document — the same refusal a
 *      save would produce, shown before anything persists.
 *   2. SAVE: stores an immutable version keyed by content hash. Identical
 *      content maps to the existing version; changed content derives a new
 *      number. It does NOT activate.
 *   3. ACTIVATE: switches the active pointer under the config's revision
 *      check. A running job keeps the protocol snapshot it started with.
 */

export function ProviderManifestEditor(props: { providerConfigId: string; open: boolean; onClose: () => void }) {
    const { t } = useTranslation();
    const [document, setDocument] = useState("");
    const [preview, setPreview] = useState<desktop.ManifestPreviewDTO | null>(null);
    const [versions, setVersions] = useState<desktop.ManifestVersionDTO[] | null>(null);
    const [working, setWorking] = useState(false);
    const [error, setError] = useState("");

    const reloadVersions = async () => {
        try {
            setVersions(await listManifestVersions(props.providerConfigId));
        } catch {
            // A build without the manifest store lists nothing; the editor
            // still allows previewing and saving.
            setVersions(null);
        }
    };

    useEffect(() => {
        if (props.open) {
            void reloadVersions();
        } else {
            setDocument("");
            setPreview(null);
            setError("");
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [props.open, props.providerConfigId]);

    const runPreview = async () => {
        setError("");
        try {
            setPreview(await previewManifest(document));
        } catch (failure) {
            setPreview(null);
            setError(failure instanceof Error ? failure.message : String(failure));
        }
    };

    const runSave = async () => {
        setWorking(true);
        setError("");
        try {
            const saved = await saveManifest({ providerConfigId: props.providerConfigId, document });
            await reloadVersions();
            setPreview(await previewManifest(document));
            if (saved && !saved.active) {
                // Saved but not active: the user decides activation, which is
                // the next explicit step.
                setPreview((current) => current);
            }
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : String(failure));
        } finally {
            setWorking(false);
        }
    };

    const runActivate = async (versionId: string) => {
        setWorking(true);
        setError("");
        try {
            await activateManifest({ providerConfigId: props.providerConfigId, versionId, expectedRevision: 0 });
            await reloadVersions();
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : String(failure));
        } finally {
            setWorking(false);
        }
    };

    return (
        <Modal
            open={props.open}
            width={720}
            title={t("config.manifest.title")}
            onCancel={props.onClose}
            footer={null}
            data-testid="config-manifest-modal"
        >
            <div className="space-y-3">
                <Typography.Paragraph className="text-xs text-stone-500">
                    {t("config.manifest.hint")}
                </Typography.Paragraph>

                <Input.TextArea
                    rows={10}
                    value={document}
                    onChange={(event) => setDocument(event.target.value)}
                    placeholder='{ "apiVersion": "atelier.provider/v1", ... }'
                    data-testid="config-manifest-document"
                />

                <Space wrap>
                    <button
                        className="rounded-md border border-stone-300 px-3 py-1.5 text-sm dark:border-stone-700"
                        onClick={() => void runPreview()}
                        disabled={!document.trim()}
                        data-testid="config-manifest-preview"
                    >
                        {t("config.manifest.preview")}
                    </button>
                    <button
                        className="rounded-md bg-stone-800 px-3 py-1.5 text-sm text-white disabled:opacity-40 dark:bg-stone-200 dark:text-stone-900"
                        onClick={() => void runSave()}
                        disabled={!document.trim() || working || (preview !== null && !preview.valid)}
                        data-testid="config-manifest-save"
                    >
                        {t("config.manifest.save")}
                    </button>
                </Space>

                {error ? (
                    <Alert type="error" showIcon message={error} data-testid="config-manifest-error" />
                ) : null}

                {preview !== null ? (
                    preview.valid ? (
                        <div className="rounded-md border border-emerald-300 bg-emerald-50 p-3 text-sm dark:border-emerald-700 dark:bg-emerald-900/20" data-testid="config-manifest-preview-ok">
                            <div className="flex flex-wrap items-center gap-2">
                                <Tag color="green">{preview.capability}</Tag>
                                <span className="font-medium">{preview.name}</span>
                                {preview.async ? <Tag color="blue">{t("config.manifest.async")}</Tag> : null}
                            </div>
                            <div className="mt-1 text-xs text-stone-600 dark:text-stone-300">
                                {t("config.manifest.submitPath")}: <code>{preview.submitPath}</code>
                            </div>
                        </div>
                    ) : (
                        <Alert type="error" showIcon message={preview.errorMessage} data-testid="config-manifest-preview-error" />
                    )
                ) : null}

                {versions !== null && versions.length > 0 ? (
                    <div>
                        <div className="mb-1 text-sm font-medium">{t("config.manifest.versions")}</div>
                        <div className="space-y-1">
                            {versions.map((version) => (
                                <div key={version.id} className="flex items-center gap-2 rounded-md border border-stone-200 px-2 py-1.5 text-xs dark:border-stone-800">
                                    <span className="font-mono">v{version.versionNumber}</span>
                                    <span className="min-w-0 flex-1 truncate font-mono text-stone-500" title={version.contentHash}>
                                        {version.contentHash.slice(0, 16)}…
                                    </span>
                                    {version.active ? (
                                        <Tag color="green" data-testid={`config-manifest-active-${version.versionNumber}`}>
                                            {t("config.manifest.active")}
                                        </Tag>
                                    ) : (
                                        <button
                                            className="rounded border border-stone-300 px-2 py-0.5 dark:border-stone-700"
                                            onClick={() => void runActivate(version.id)}
                                            disabled={working}
                                            data-testid={`config-manifest-activate-${version.versionNumber}`}
                                        >
                                            {t("config.manifest.activate")}
                                        </button>
                                    )}
                                </div>
                            ))}
                        </div>
                    </div>
                ) : null}
            </div>
        </Modal>
    );
}
