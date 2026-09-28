import { useCallback, useEffect, useState } from "react";
import { Alert, Button, Input, Modal, Space, Tag, Typography } from "antd";
import { useTranslation } from "react-i18next";

import { activateSkillVersion, createSkillVersion, listSkillVersions } from "@/services/desktop/agent-skills";
import type { agent } from "@/wailsjs/go/models";

/**
 * AgentSkillEditor is RP-06.2's skill version management surface: view a
 * skill's version history, derive a new version from an existing one, and
 * roll back by switching the active pointer.
 *
 * # The rules the UI keeps
 *
 *   - The history shows each version's REAL number and content hash with its
 *     status — never a "current document" pretending to be the history.
 *   - Creating a version does NOT activate it: save derives and stores, the
 *     activate button is the separate, explicit switch. This is the review
 *     step between derive and use.
 *   - The document must carry every section 4.3 heading; the backend's
 *     validation message is rendered, not swallowed.
 *   - Rollback affects only LATER runs (the runtime keeps the snapshot it
 *     took) — the hint says so, because that is the property a user needs to
 *     trust a rollback.
 */

export function AgentSkillEditor(props: {
    agentKey: string;
    open: boolean;
    onClose: () => void;
}) {
    const { t } = useTranslation();
    const [versions, setVersions] = useState<agent.SkillVersion[] | null>(null);
    const [editing, setEditing] = useState(false);
    const [baseVersion, setBaseVersion] = useState("");
    const [label, setLabel] = useState("");
    const [document, setDocument] = useState("");
    const [error, setError] = useState("");
    const [working, setWorking] = useState(false);

    const reload = useCallback(async () => {
        setError("");
        try {
            setVersions(await listSkillVersions(props.agentKey));
        } catch (failure) {
            setVersions(null);
            setError(failure instanceof Error ? failure.message : String(failure));
        }
    }, [props.agentKey]);

    useEffect(() => {
        if (props.open) void reload();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [props.open, props.agentKey]);

    const runCreate = async () => {
        setWorking(true);
        setError("");
        try {
            await createSkillVersion({
                agentKey: props.agentKey,
                basedOnVersionId: baseVersion,
                newVersionLabel: label.trim(),
                document,
            });
            setEditing(false);
            await reload();
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
            await activateSkillVersion(props.agentKey, versionId);
            await reload();
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : String(failure));
        } finally {
            setWorking(false);
        }
    };

    return (
        <Modal
            open={props.open}
            width={680}
            title={t("agents.skillEditor.title", { key: props.agentKey })}
            onCancel={props.onClose}
            footer={null}
            data-testid="agents-skill-modal"
        >
            <div className="space-y-3">
                <Typography.Paragraph className="text-xs text-stone-500">
                    {t("agents.skillEditor.hint")}
                </Typography.Paragraph>

                {error ? <Alert type="error" showIcon message={error} data-testid="agents-skill-error" /> : null}

                {versions !== null && versions.length > 0 ? (
                    <div className="space-y-1">
                        {versions.map((version) => (
                            <div
                                key={version.ID}
                                className="flex items-center gap-2 rounded-md border border-stone-200 px-2 py-1.5 text-xs dark:border-stone-800"
                                data-testid={`agents-skill-version-${version.Version}`}
                            >
                                <span className="font-mono font-medium">{version.Version}</span>
                                <span className="min-w-0 flex-1 truncate font-mono text-stone-500" title={version.ContentHash}>
                                    {version.ContentHash.slice(0, 16)}…
                                </span>
                                {version.Status === "active" ? (
                                    <Tag color="green">{t("agents.skillEditor.active")}</Tag>
                                ) : (
                                    <button
                                        className="rounded border border-stone-300 px-2 py-0.5 dark:border-stone-700"
                                        onClick={() => void runActivate(version.ID)}
                                        disabled={working}
                                        data-testid={`agents-skill-activate-${version.Version}`}
                                    >
                                        {t("agents.skillEditor.activate")}
                                    </button>
                                )}
                                <button
                                    className="rounded border border-stone-300 px-2 py-0.5 dark:border-stone-700"
                                    onClick={() => {
                                        setBaseVersion(version.ID);
                                        setEditing(true);
                                    }}
                                    data-testid={`agents-skill-derive-${version.Version}`}
                                >
                                    {t("agents.skillEditor.derive")}
                                </button>
                            </div>
                        ))}
                    </div>
                ) : (
                    <div className="text-xs text-stone-500">{t("agents.skillEditor.noVersions")}</div>
                )}

                {editing ? (
                    <div className="space-y-2 rounded-md border border-stone-200 p-3 dark:border-stone-800">
                        <div className="text-sm font-medium">{t("agents.skillEditor.deriveTitle")}</div>
                        <Input
                            value={label}
                            onChange={(event) => setLabel(event.target.value)}
                            placeholder="1.1.0"
                            data-testid="agents-skill-label"
                        />
                        <Input.TextArea
                            rows={10}
                            value={document}
                            onChange={(event) => setDocument(event.target.value)}
                            data-testid="agents-skill-document"
                        />
                        <Space>
                            <Button size="small" onClick={() => setEditing(false)}>
                                {t("common.cancel")}
                            </Button>
                            <Button
                                size="small"
                                type="primary"
                                loading={working}
                                disabled={!label.trim() || !document.trim()}
                                onClick={() => void runCreate()}
                                data-testid="agents-skill-create"
                            >
                                {t("agents.skillEditor.create")}
                            </Button>
                        </Space>
                    </div>
                ) : null}
            </div>
        </Modal>
    );
}
