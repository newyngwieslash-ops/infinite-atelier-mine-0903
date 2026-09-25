import { useCallback, useEffect, useState } from "react";
import { Alert, App, Button, Checkbox, Space, Table, Typography } from "antd";
import { Download, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

/**
 * DiagnosticsPanel is FR-180's 「一键生成诊断包」, and SECURITY 14.2's two clauses are its shape.
 *
 * # 「生成前展示清单」
 *
 * The table below IS the manifest: one row per section, with the bytes it would contribute and a
 * sentence saying what it holds. The list is produced by a READ that writes nothing, so opening the
 * panel is safe — and `reload` can be pressed twice without consequence.
 *
 * # 「用户可取消内容」
 *
 * Each row has a checkbox, and only the CHECKED sections are requested. The list is not a
 * description of a fixed bundle: it is the choice. A section the user unchecks is left out of the
 * bundle AND recorded as omitted in the bundle's own manifest, which is what lets a reader of it tell
 * "they chose not to send their logs" from "there were no logs".
 *
 * # What the panel does with the bundle
 *
 * It writes the files into a downloaded directory — one file per section, named after it. It does NOT
 * zip them: the archive writer lives in the Go core and this endpoint does not use it, so inventing a
 * second archive format in the webview would produce a file the core cannot read back. A user who
 * wants one file can zip what they were given, and the manifest inside says what it is.
 */
export type DiagnosticsPanelProps = {
    /** Sections the user wants. Empty means every section, which is the panel's opening state. */
    sections: string[];
};

type DiagnosticsClient = {
    probe: () => Promise<boolean>;
    plan: (sections: string[]) => Promise<{ sections: DiagnosticsSection[]; totalBytes: number }>;
    collect: (sections: string[]) => Promise<{ files: Record<string, string>; totalBytes: number }>;
};

type DiagnosticsSection = {
    section: string;
    bytes: number;
    note: string;
    included: boolean;
};

/**
 * formatBytes renders a size the way a person reads one, matching the garbage panel's own.
 */
function formatBytes(bytes: number): string {
    if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB"];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit += 1;
    }
    return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function DiagnosticsPanel() {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [client, setClient] = useState<DiagnosticsClient | null>(null);
    const [sections, setSections] = useState<DiagnosticsSection[]>([]);
    const [chosen, setChosen] = useState<Set<string>>(new Set());
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");

    // The client is imported LAZILY, so a browser session loads nothing and the panel renders its
    // unavailable state rather than failing at import time.
    useEffect(() => {
        let cancelled = false;
        void import("@/services/desktop/diagnostics").then(async (module) => {
            if (cancelled) return;
            if (!module.isDiagnosticsAvailable()) {
                setClient(null);
                return;
            }
            setClient({
                probe: async () => true,
                plan: module.planDiagnostics,
                collect: module.collectDiagnostics,
            });
        });
        return () => {
            cancelled = true;
        };
    }, []);

    const reload = useCallback(async () => {
        if (!client) return;
        setBusy(true);
        setError("");
        try {
            // An empty list asks for EVERY section, which is what the manifest shows: a user chooses
            // what to REMOVE rather than what to add, so nothing is hidden by default.
            const plan = await client.plan([]);
            setSections(plan.sections);
            // The opening state selects everything INCLUDED by the plan, so the panel's default is the
            // whole bundle and unchecking is the act that means something.
            setChosen(new Set(plan.sections.filter((entry) => entry.included).map((entry) => entry.section)));
        } catch (failure) {
            setError(String(failure));
        } finally {
            setBusy(false);
        }
    }, [client]);

    useEffect(() => {
        void reload();
    }, [reload]);

    const collect = useCallback(async () => {
        if (!client) return;
        setBusy(true);
        try {
            const wanted = Array.from(chosen);
            const bundle = await client.collect(wanted);
            const written = writeBundleFiles(bundle.files);
            message.success(t("config.diagnostics.collected", { count: written }));
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
        }
    }, [client, chosen, message, t]);

    if (client === null) {
        return (
            <Alert
                type="info"
                showIcon
                data-testid="diagnostics-unavailable"
                message={t("config.diagnostics.unavailable")}
            />
        );
    }

    const total = sections
        .filter((entry) => chosen.has(entry.section))
        .reduce((sum, entry) => sum + entry.bytes, 0);

    return (
        <Space direction="vertical" size="middle" className="w-full" data-testid="diagnostics-panel">
            <Typography.Paragraph type="secondary" className="text-xs">
                {t("config.diagnostics.hint")}
            </Typography.Paragraph>
            <Space wrap>
                <Button
                    icon={<RefreshCw className="size-4" />}
                    loading={busy}
                    onClick={() => void reload()}
                    data-testid="diagnostics-refresh"
                >
                    {t("config.diagnostics.refresh")}
                </Button>
                <Button
                    type="primary"
                    icon={<Download className="size-4" />}
                    loading={busy}
                    disabled={chosen.size === 0}
                    onClick={() => void collect()}
                    data-testid="diagnostics-collect"
                >
                    {t("config.diagnostics.collect")}
                </Button>
                <Typography.Text type="secondary" data-testid="diagnostics-total">
                    {t("config.diagnostics.total", { bytes: formatBytes(total) })}
                </Typography.Text>
            </Space>
            {error !== "" ? <Alert type="error" showIcon message={error} /> : null}
            <Table<DiagnosticsSection>
                size="small"
                rowKey="section"
                pagination={false}
                dataSource={sections}
                data-testid="diagnostics-sections"
                columns={[
                    {
                        title: "",
                        key: "include",
                        width: 50,
                        render: (_: unknown, row: DiagnosticsSection) => (
                            <Checkbox
                                checked={chosen.has(row.section)}
                                data-testid={`diagnostics-include-${row.section}`}
                                onChange={(event) =>
                                    setChosen((current) => {
                                        const next = new Set(current);
                                        if (event.target.checked) next.add(row.section);
                                        else next.delete(row.section);
                                        return next;
                                    })
                                }
                            />
                        ),
                    },
                    {
                        title: t("config.diagnostics.section"),
                        dataIndex: "section",
                        width: 150,
                        render: (value: string) => <Typography.Text code>{value}</Typography.Text>,
                    },
                    {
                        title: t("config.diagnostics.size"),
                        dataIndex: "bytes",
                        width: 100,
                        render: (value: number) => formatBytes(value),
                    },
                    {
                        title: t("config.diagnostics.contents"),
                        dataIndex: "note",
                        render: (value: string) => <span className="text-xs">{value}</span>,
                    },
                ]}
            />
        </Space>
    );
}

/**
 * writeBundleFiles hands the bundle's files to the browser's download path.
 *
 * Each section becomes its own file, named after the section, because the bundle IS a directory of
 * text rather than an archive — see the component's header for why the webview does not zip it.
 * The name is the section's own identifier rather than a translated label: a support conversation
 * refers to files by the names the manifest lists.
 */
function writeBundleFiles(files: Record<string, string>): number {
    let written = 0;
    for (const [section, encoded] of Object.entries(files)) {
        // The content crossed as base64 because a Wails call carries JSON, and a log is not text an
        // encoder can be trusted with.
        const binary = atob(encoded);
        const bytes = new Uint8Array(binary.length);
        for (let index = 0; index < binary.length; index += 1) {
            bytes[index] = binary.charCodeAt(index);
        }
        const blob = new Blob([bytes], { type: "text/plain" });
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = `infinite-atelier-diagnostics-${section}.txt`;
        anchor.click();
        // The object URL is released immediately: a bundle can be several megabytes and holding every
        // section's URL until the page unloads would keep them all alive.
        URL.revokeObjectURL(url);
        written += 1;
    }
    return written;
}
