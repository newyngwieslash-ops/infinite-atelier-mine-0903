import { useCallback, useEffect, useState } from "react";
import { App, Button, InputNumber, Space } from "antd";
import { useTranslation } from "react-i18next";

import { getAutoBackup, setAutoBackup } from "@/services/desktop/app-settings";

/**
 * AppSettingsPanel is RP-09.2's application settings: the auto-backup
 * cadence and retention, persisted through the settings binding (migration
 * 000033's app_settings) and read by the scheduler at startup.
 *
 * # The honesty rules the panel keeps
 *
 *   - The DEFAULTS (24h / keep 3) are what a fresh install shows, and the
 *     panel says a change takes effect on the next app start — the running
 *     scheduler is not hot-reloaded, and claiming otherwise would be a false
 *     promise about when the user's data is protected.
 *   - Validation failures come from the BINDING (the boundary), and the
 *     panel renders them instead of a success message.
 *   - Disabling (interval 0) is an explicit user state, shown as such — not
 *     a silent absence of backups.
 */
export function AppSettingsPanel() {
    const { message } = App.useApp();
    const { t } = useTranslation();
    const [intervalHours, setIntervalHours] = useState<number | null>(24);
    const [retain, setRetain] = useState<number | null>(3);
    const [revision, setRevision] = useState(0);
    const [saving, setSaving] = useState(false);
    const [loaded, setLoaded] = useState(false);

    const reload = useCallback(async () => {
        try {
            const settings = await getAutoBackup();
            setIntervalHours(settings.intervalHours);
            setRetain(settings.retain);
            setLoaded(true);
        } catch {
            // The binding is unattached in safe mode; the panel stays on the
            // defaults and the save reports the unavailable state.
            setLoaded(false);
        }
    }, []);

    useEffect(() => {
        void reload();
    }, [reload]);

    const save = async () => {
        if (intervalHours === null || retain === null) return;
        setSaving(true);
        try {
            await setAutoBackup({ intervalHours, retain }, revision);
            message.success(t("settings.app.saved"));
            await reload();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("settings.app.saveFailed"));
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="space-y-4" data-testid="settings-app-panel">
            <div>
                <div className="text-sm font-medium">{t("settings.app.autoBackupTitle")}</div>
                <div className="mt-1 text-xs text-stone-500">{t("settings.app.autoBackupHint")}</div>
            </div>
            <div className="flex flex-wrap items-end gap-3">
                <label className="block">
                    <span className="mb-1 block text-xs text-stone-500">{t("settings.app.interval")}</span>
                    <InputNumber
                        min={0}
                        max={24 * 365}
                        value={intervalHours}
                        onChange={(value) => setIntervalHours(typeof value === "number" ? value : 0)}
                        addonAfter={t("settings.app.hours")}
                        data-testid="settings-autobackup-interval"
                    />
                </label>
                <label className="block">
                    <span className="mb-1 block text-xs text-stone-500">{t("settings.app.retain")}</span>
                    <InputNumber
                        min={1}
                        max={100}
                        value={retain}
                        onChange={(value) => setRetain(typeof value === "number" ? value : 3)}
                        data-testid="settings-autobackup-retain"
                    />
                </label>
                <Space>
                    <Button
                        type="primary"
                        loading={saving}
                        disabled={!loaded || intervalHours === null || retain === null}
                        onClick={() => void save()}
                        data-testid="settings-autobackup-save"
                    >
                        {t("settings.app.save")}
                    </Button>
                </Space>
            </div>
            {intervalHours === 0 ? (
                <div className="text-xs text-amber-600 dark:text-amber-400">{t("settings.app.disabledNote")}</div>
            ) : (
                <div className="text-xs text-stone-500">{t("settings.app.restartNote")}</div>
            )}
        </div>
    );
}
