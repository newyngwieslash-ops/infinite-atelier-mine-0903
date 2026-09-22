import io

p = 'web/src/pages/studio/sections.tsx'
s = io.open(p, encoding='utf-8').read()

# The DTO read and the columns gain a way in.
old = '''    const columns: ColumnsType<desktop.AssetDTO> = [
        { title: t("studio.assets.nameLabel"), dataIndex: "name", key: "name" },
        {
            title: t("studio.assets.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 130,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.assets.versionLabel"),
            key: "version",
            render: (_, record) =>
                record.currentApprovedVersionId ? <code className="text-xs">{record.currentApprovedVersionId}</code> : <span className="text-xs text-stone-500">{t("studio.assets.noVersion")}</span>,
        },
    ];'''
new = '''    const columns: ColumnsType<desktop.AssetDTO> = [
        { title: t("studio.assets.nameLabel"), dataIndex: "name", key: "name" },
        {
            title: t("studio.assets.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 130,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.assets.versionLabel"),
            key: "version",
            render: (_, record) =>
                record.currentApprovedVersionId ? <code className="text-xs">{record.currentApprovedVersionId}</code> : <span className="text-xs text-stone-500">{t("studio.assets.noVersion")}</span>,
        },
        {
            title: "",
            key: "versions",
            width: 120,
            render: (_, record) => (
                <Button
                    size="small"
                    type="link"
                    data-testid={`studio-asset-versions-${record.id}`}
                    onClick={() => setVersionedAsset(record)}
                >
                    {t("studio.assets.versions")}
                </Button>
            ),
        },
    ];'''
assert old in s, "columns anchor"
s = s.replace(old, new, 1)

# The state the drawer needs, beside the existing ones.
old_state = '''    const columns: ColumnsType<desktop.AssetDTO> = ['''
new_state = '''    // The version drawer's state. It is per-asset rather than a page: opening one asset's
    // versions must not disturb the list a user is reading.
    const [versionedAsset, setVersionedAsset] = useState<desktop.AssetDTO | null>(null);

    const columns: ColumnsType<desktop.AssetDTO> = ['''
s = s.replace(old_state, new_state, 1)

# The drawer itself, after the table.
old_table = '''            {assets.length === 0 ? (
                <Empty description={t("studio.assets.empty", { type: typeLabel })} />
            ) : (
                <Table<desktop.AssetDTO> rowKey="id" size="small" pagination={false} columns={columns} dataSource={assets} data-testid="studio-asset-table" />
            )}
        </div>
    );
}'''
new_table = '''            {assets.length === 0 ? (
                <Empty description={t("studio.assets.empty", { type: typeLabel })} />
            ) : (
                <Table<desktop.AssetDTO> rowKey="id" size="small" pagination={false} columns={columns} dataSource={assets} data-testid="studio-asset-table" />
            )}
            <AssetVersionsDrawer
                asset={versionedAsset}
                onClose={() => setVersionedAsset(null)}
                onChanged={onChanged}
            />
        </div>
    );
}

/**
 * AssetVersionsDrawer is one asset's versions, its usages, and the approval switch.
 *
 * AC-ASSET-001's scenario runs through this: two candidate versions, approve v1, approve v2
 * and v1 becomes superseded, a shot using v1 triggers the impact analysis, and v1 is NOT
 * deleted. The impact is shown BEFORE the approval because DOMAIN_MODEL section 8.2 requires
 * it before the switch — and because the core refuses an unacknowledged approval, so a UI
 * that could not ask the question could only send the acknowledgement blind.
 *
 * The usages are READ rather than inferred: they are what the impact list is built from, and
 * a UI that guessed at them would show a user a list nobody computed.
 */
function AssetVersionsDrawer({
    asset,
    onClose,
    onChanged,
}: {
    asset: desktop.AssetDTO | null;
    onClose: () => void;
    onChanged: () => void;
}) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [versions, setVersions] = useState<desktop.AssetVersionDTO[]>([]);
    const [impact, setImpact] = useState<desktop.ApprovalImpactDTO | null>(null);
    const [impactFor, setImpactFor] = useState("");
    const [usages, setUsages] = useState<desktop.AssetUsageDTO[]>([]);
    const [loading, setLoading] = useState(false);

    const load = useCallback(async () => {
        if (!asset) {
            setVersions([]);
            setUsages([]);
            return;
        }
        setLoading(true);
        try {
            const found = await listAssetVersions(asset.id);
            setVersions(found);
            // The usages of the version IN FORCE are what an impact switch disturbs, which is
            // why they are read for that one rather than for the whole asset.
            if (asset.currentApprovedVersionId) {
                setUsages(await listAssetUsages(asset.currentApprovedVersionId));
            } else {
                setUsages([]);
            }
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setLoading(false);
        }
    }, [asset, message]);

    useEffect(() => {
        void load();
    }, [load]);

    const askImpact = useCallback(
        async (versionId: string) => {
            try {
                setImpact(await getApprovalImpact(versionId));
                setImpactFor(versionId);
            } catch (failure) {
                message.error(String(failure));
            }
        },
        [message],
    );

    const approve = useCallback(
        async (versionId: string) => {
            try {
                await approveAssetVersion({ versionId, impactAcknowledged: true });
                message.success(t("studio.assets.approved"));
                await load();
                onChanged();
            } catch (failure) {
                message.error(String(failure));
            } finally {
                setImpact(null);
                setImpactFor("");
            }
        },
        [load, onChanged, t, message],
    );

    const columns: ColumnsType<desktop.AssetVersionDTO> = [
        { title: "#", dataIndex: "versionNumber", width: 60, render: (value: number) => `v${value}` },
        {
            title: t("studio.assets.statusLabel"),
            dataIndex: "status",
            width: 130,
            render: (value: string) =>
                value === "approved" ? <Tag color="green">{value}</Tag> : <Tag>{value}</Tag>,
        },
        {
            title: t("studio.assets.lineage"),
            key: "lineage",
            ellipsis: true,
            render: (_, row) =>
                [row.generationJobId ? `${t("studio.assets.job")}: ${row.generationJobId.slice(0, 8)}` : "", row.seed ? `seed ${row.seed}` : ""]
                    .filter(Boolean)
                    .join(" · ") || "—",
        },
        {
            title: "",
            key: "actions",
            width: 200,
            render: (_, row) =>
                row.status === "approved" ? (
                    <Tag color="green">{t("studio.assets.approved")}</Tag>
                ) : row.status === "superseded" ? (
                    // A superseded version is KEPT rather than deleted: AC-ASSET-001's last
                    // clause is that v1 survives the switch to v2.
                    <Tooltip title={t("studio.assets.supersededHint")}>
                        <Tag>{t("studio.assets.superseded")}</Tag>
                    </Tooltip>
                ) : (
                    <Button size="small" type="primary" ghost onClick={() => void askImpact(row.id)}>
                        {t("studio.assets.approve")}
                    </Button>
                ),
        },
    ];

    return (
        <Drawer
            open={asset !== null}
            onClose={onClose}
            width="min(94vw, 760px)"
            title={asset ? asset.name : ""}
            destroyOnHidden
        >
            <Space direction="vertical" size="middle" className="w-full">
                <Table rowKey="id" size="small" loading={loading} pagination={false} columns={columns} dataSource={versions} />
                {usages.length > 0 ? (
                    <div>
                        <Typography.Text strong>{t("studio.assets.usedBy")}</Typography.Text>
                        <ul className="mt-1 list-disc pl-5 text-sm">
                            {usages.map((usage) => (
                                <li key={`${usage.consumerType}-${usage.consumerId}-${usage.usageRole}`}>
                                    {usage.consumerType} · {usage.consumerId}
                                    {usage.required ? ` (${t("studio.assets.required")})` : ""}
                                </li>
                            ))}
                        </ul>
                    </div>
                ) : null}
                <Modal
                    open={impact !== null}
                    title={t("studio.assets.impactTitle")}
                    onCancel={() => {
                        setImpact(null);
                        setImpactFor("");
                    }}
                    onOk={() => void approve(impactFor)}
                    okText={t("studio.assets.approveAnyway")}
                    width="min(92vw, 560px)"
                    destroyOnHidden
                >
                    {impact && impact.replaces ? (
                        <Space direction="vertical" size="small" className="w-full">
                            <Typography.Text>
                                {t("studio.assets.impactReplaces", { version: impact.replaces })}
                            </Typography.Text>
                            {impact.consumers && impact.consumers.length > 0 ? (
                                <ul className="list-disc pl-5 text-sm">
                                    {impact.consumers.map((consumer) => (
                                        <li key={`${consumer.consumerType}-${consumer.consumerId}-${consumer.usageRole}`}>
                                            {consumer.consumerType} · {consumer.consumerId}
                                            {consumer.required ? ` (${t("studio.assets.required")})` : ""}
                                        </li>
                                    ))}
                                </ul>
                            ) : (
                                <Typography.Text type="secondary">{t("studio.assets.impactNone")}</Typography.Text>
                            )}
                        </Space>
                    ) : (
                        <Typography.Text type="secondary">{t("studio.assets.impactFirst")}</Typography.Text>
                    )}
                </Modal>
            </Space>
        </Drawer>
    );
}'''
assert old_table in s, "table anchor"
s = s.replace(old_table, new_table, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("asset drawer added")
