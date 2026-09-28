import { useCallback, useEffect, useState } from "react";
import { Alert, App, Empty, Select, Space, Spin, Switch, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTranslation } from "react-i18next";

import {
    agentInventory,
    setAgentEnabled,
    getAgentRunTrace,
    isAgentBindingsAvailable,
    isAgentInventoryAvailable,
    listAgentRuns,
} from "@/services/desktop/agents";
import type { agentruntime, desktop } from "@/wailsjs/go/models";

/**
 * AgentCenterSection is WP-07's surface: what the agent runtime did.
 *
 * Three rules it keeps, and each is a rule the rest of the studio keeps too:
 *
 *  1. IT NEVER RENDERS A READ THAT FAILED AS EMPTY. "The core could not be
 *     reached" and "no runs exist" are different situations and only one of them
 *     is true, so a failed read shows the failure.
 *  2. IT NEVER SHOWS A RUN'S CONTENT IT DID NOT READ. The list is the run
 *     summaries the binding returned; the detail pane is one trace, fetched when
 *     a row is selected. Nothing here composes a plausible trace from a summary.
 *  3. IT SHOWS WHAT SECTION 16 ALLOWS. There is no reasoning trace on this
 *     screen because the record does not carry one: what a run holds is the
 *     reasonSummary the model itself produced, the tools it called and the
 *     versions it reported. A surface that displayed more would be inventing it.
 *
 * The inventory and the runs are separate reads with separate availability,
 * because they have different requirements in Go: the inventory reads the
 * assembled registry and the runs read a database. A safe-mode build therefore
 * lists its agents and shows no runs, which this renders as two independent
 * states rather than one.
 */
export type AgentCenterSectionProps = {
    projectId: string;
};

export function AgentCenterSection({ projectId }: AgentCenterSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [runs, setRuns] = useState<agentruntime.RunSummary[] | null>(null);
    const [runsError, setRunsError] = useState("");
    const [agents, setAgents] = useState<desktop.AgentSpecDTO[] | null>(null);
    const [selected, setSelected] = useState<agentruntime.RunTrace | null>(null);
    const [traceLoading, setTraceLoading] = useState(false);
    const [filter, setFilter] = useState<string>("");

    const loadRuns = useCallback(async () => {
        setRunsError("");
        try {
            setRuns(await listAgentRuns(projectId));
        } catch (error) {
            // A read that failed is reported rather than shown as an empty list.
            setRuns([]);
            setRunsError(error instanceof Error ? error.message : t("studio.agents.readFailed"));
        }
    }, [projectId, t]);

    useEffect(() => {
        if (!isAgentBindingsAvailable()) {
            setRuns([]);
            return;
        }
        void loadRuns();
    }, [loadRuns]);

    useEffect(() => {
        if (!isAgentInventoryAvailable()) {
            setAgents([]);
            return;
        }
        void agentInventory().then(setAgents);
    }, []);

    /**
     * toggleAgent starts or stops one agent (T09). The core refuses an
     * unknown key; the list is re-read so the switch and the core agree.
     */
    const toggleAgent = useCallback(async (agentKey: string, enabled: boolean) => {
        try {
            await setAgentEnabled(agentKey, enabled);
            setAgents(await agentInventory());
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : String(failure));
        }
    }, []);

    const selectRun = useCallback(
        async (run: agentruntime.RunSummary) => {
            setTraceLoading(true);
            try {
                setSelected(await getAgentRunTrace(projectId, run.id));
            } finally {
                setTraceLoading(false);
            }
        },
        [projectId],
    );

    if (!isAgentBindingsAvailable()) {
        return (
            <Alert
                type="info"
                showIcon
                message={t("studio.desktopOnly.title")}
                description={t("studio.agents.noCore")}
                data-testid="studio-agents-no-core"
            />
        );
    }

    const runColumns: ColumnsType<agentruntime.RunSummary> = [
        {
            title: t("studio.agents.agentLabel"),
            dataIndex: "agentKey",
            key: "agentKey",
            render: (value: string) => <span className="font-mono text-xs">{value}</span>,
        },
        {
            title: t("studio.agents.layerLabel"),
            dataIndex: "layer",
            key: "layer",
            width: 120,
            render: (value: string) => <Tag>{t(`studio.agents.layer.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.agents.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 130,
            render: (value: string) => (
                <Tag color={statusColor(value)}>{t(`studio.agents.status.${value}`, { defaultValue: value })}</Tag>
            ),
        },
        {
            title: t("studio.agents.errorLabel"),
            dataIndex: "errorCode",
            key: "errorCode",
            width: 200,
            render: (value?: string) => (value ? <span className="font-mono text-xs">{value}</span> : "—"),
        },
        {
            title: t("studio.agents.startedAt"),
            dataIndex: "startedAt",
            key: "startedAt",
            width: 180,
            render: (value: string) => (value ? new Date(value).toLocaleString() : "—"),
        },
    ];

    const visible = (runs ?? []).filter((run) => filter === "" || run.layer === filter);

    return (
        <div className="space-y-8">
            {runsError !== "" && (
                <Alert
                    type="error"
                    showIcon
                    message={t("studio.agents.readFailed")}
                    description={runsError}
                    data-testid="studio-agents-read-error"
                />
            )}

            <section>
                <div className="flex items-center justify-between">
                    <h2 className="text-lg font-medium">{t("studio.agents.runs")}</h2>
                    <Space>
                        <Select
                            allowClear
                            size="small"
                            placeholder={t("studio.agents.filterLayer")}
                            style={{ width: 180 }}
                            value={filter === "" ? undefined : filter}
                            onChange={(value) => setFilter(value ?? "")}
                            options={[
                                { value: "decision", label: t("studio.agents.layer.decision") },
                                { value: "execution", label: t("studio.agents.layer.execution") },
                                { value: "supervision", label: t("studio.agents.layer.supervision") },
                            ]}
                        />
                    </Space>
                </div>
                {runs === null ? (
                    <Spin className="mt-3" />
                ) : visible.length === 0 ? (
                    <Empty description={t("studio.agents.noRuns")} />
                ) : (
                    <Table<agentruntime.RunSummary>
                        rowKey="id"
                        size="small"
                        pagination={false}
                        className="mt-3"
                        columns={runColumns}
                        dataSource={visible}
                        onRow={(record) => ({ onClick: () => void selectRun(record) })}
                        data-testid="studio-agent-runs"
                    />
                )}
            </section>

            {selected && (
                <section data-testid="studio-agent-trace">
                    <h2 className="text-lg font-medium">
                        {t("studio.agents.trace")}
                        {traceLoading && <Spin size="small" className="ml-2" />}
                    </h2>
                    <div className="mt-3 space-y-4">
                        {/* The run's own summary, which is what AGENT_CONTRACTS section 16
                            permits instead of a reasoning trace. */}
                        {selected.run.inputSummary !== "" && (
                            <div>
                                <div className="text-xs text-neutral-500">{t("studio.agents.inputSummary")}</div>
                                <div className="font-mono text-xs">{selected.run.inputSummary}</div>
                            </div>
                        )}
                        <div>
                            <div className="text-xs text-neutral-500">{t("studio.agents.toolCalls")}</div>
                            {selected.toolCalls.length === 0 ? (
                                <Empty description={t("studio.agents.noToolCalls")} />
                            ) : (
                                <Table<agentruntime.ToolCallView>
                                    rowKey="id"
                                    size="small"
                                    pagination={false}
                                    columns={[
                                        { title: "#", dataIndex: "sequence", key: "sequence", width: 60 },
                                        {
                                            title: t("studio.agents.toolLabel"),
                                            dataIndex: "toolKey",
                                            key: "toolKey",
                                            render: (value: string) => <span className="font-mono text-xs">{value}</span>,
                                        },
                                        {
                                            title: t("studio.agents.statusLabel"),
                                            dataIndex: "status",
                                            key: "status",
                                            width: 120,
                                            render: (value: string) => (
                                                <Tag color={statusColor(value)}>
                                                    {t(`studio.agents.toolStatus.${value}`, { defaultValue: value })}
                                                </Tag>
                                            ),
                                        },
                                        {
                                            title: t("studio.agents.errorLabel"),
                                            dataIndex: "errorCode",
                                            key: "errorCode",
                                            width: 220,
                                            render: (value?: string) =>
                                                value ? <span className="font-mono text-xs">{value}</span> : "—",
                                        },
                                    ]}
                                    dataSource={selected.toolCalls}
                                    data-testid="studio-agent-tool-calls"
                                />
                            )}
                        </div>
                        <div>
                            <div className="text-xs text-neutral-500">{t("studio.agents.messages")}</div>
                            {selected.messages.length === 0 ? (
                                <Empty description={t("studio.agents.noMessages")} />
                            ) : (
                                <ul className="mt-2 space-y-2" data-testid="studio-agent-messages">
                                    {selected.messages.map((message) => (
                                        <li key={message.id} className="rounded border border-neutral-200 p-2">
                                            <div className="flex items-center gap-2">
                                                <Tag>{t(`studio.agents.role.${message.role}`, { defaultValue: message.role })}</Tag>
                                                <span className="text-xs text-neutral-500">
                                                    {new Date(message.createdAt).toLocaleString()}
                                                </span>
                                            </div>
                                            <pre className="mt-1 whitespace-pre-wrap break-words text-xs">{message.content}</pre>
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </div>
                    </div>
                </section>
            )}

            <section data-testid="studio-agent-inventory">
                <h2 className="text-lg font-medium">{t("studio.agents.inventory")}</h2>
                {agents === null ? (
                    <Spin className="mt-3" />
                ) : agents.length === 0 ? (
                    <Empty description={t("studio.agents.noAgents")} />
                ) : (
                    <ul className="mt-3 space-y-2">
                        {agents.map((agent) => (
                            <li key={agent.key} className="rounded border border-neutral-200 p-2">
                                <div className="flex items-center justify-between">
                                    <span className="font-mono text-xs">{agent.key}</span>
                                    <Space>
                                        {/* THE STOP SWITCH (T09): enabled is the core's
                                            own state read back, and flipping it re-reads the
                                            list so the two agree. */}
                                        <Switch
                                            size="small"
                                            checked={agent.enabled}
                                            checkedChildren={t("studio.agents.enabled")}
                                            unCheckedChildren={t("studio.agents.disabled")}
                                            data-testid={`studio-agent-toggle-${agent.key}`}
                                            onChange={(checked) => void toggleAgent(agent.key, checked)}
                                        />
                                        <Tag>{t(`studio.agents.layer.${agent.layer}`, { defaultValue: agent.layer })}</Tag>
                                        <span className="text-xs text-neutral-500">
                                            {t("studio.agents.toolBudget", {
                                                count: agent.maxToolCalls,
                                                seconds: agent.maxDurationSeconds,
                                            })}
                                        </span>
                                    </Space>
                                </div>
                                {/* The tool KEYS, because a reviewer asking why a call was refused
                                    needs to see what the agent was allowed to call. */}
                                <div className="mt-1 flex flex-wrap gap-1">
                                    {agent.allowedTools.map((tool) => (
                                        <Tag key={tool} className="font-mono text-xs">
                                            {tool}
                                        </Tag>
                                    ))}
                                </div>
                            </li>
                        ))}
                    </ul>
                )}
            </section>
        </div>
    );
}

/** statusColor maps a run or tool-call status to a tag colour. */
function statusColor(status: string): string | undefined {
    switch (status) {
        case "succeeded":
            return "green";
        case "failed":
        case "denied":
            return "red";
        case "cancelled":
            return "orange";
        case "running":
            return "blue";
        default:
            return undefined;
    }
}
