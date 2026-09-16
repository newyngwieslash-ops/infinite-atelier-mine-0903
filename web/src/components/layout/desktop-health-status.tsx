import { Tooltip } from "antd";
import { CheckCircle2, CircleAlert, LoaderCircle, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useDesktopHealth } from "@/hooks/use-desktop-health";
import { isDesktopHealthBindingAvailable } from "@/services/desktop/health";

export function DesktopHealthStatus() {
    const { t } = useTranslation();
    const { data: health, isError, isLoading } = useDesktopHealth();

    if (!isDesktopHealthBindingAvailable()) return null;

    if (isLoading || health === undefined) {
        return <LoaderCircle className="size-4 animate-spin text-stone-500 dark:text-stone-400" aria-label={t("desktopHealth.loading")} role="status" />;
    }

    if (isError || health === null) {
        return (
            <span className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium text-stone-600 dark:text-stone-300" aria-label={t("desktopHealth.unavailable")} role="status">
                <CircleAlert className="size-4" aria-hidden="true" />
                <span>{t("desktopHealth.unavailable")}</span>
            </span>
        );
    }

    if (health.safeMode) {
        const diagnostic = health.diagnostic || t("desktopHealth.diagnosticUnavailable");
        const label = t("desktopHealth.safeMode");

        return (
            <Tooltip title={t("desktopHealth.safeModeDiagnostic", { diagnostic })} mouseEnterDelay={0.2}>
                <span className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium text-amber-700 dark:text-amber-300" tabIndex={0} aria-label={t("desktopHealth.safeModeDiagnostic", { diagnostic })}>
                    <ShieldAlert className="size-4" aria-hidden="true" />
                    <span>{label}</span>
                </span>
            </Tooltip>
        );
    }

    return (
        <span className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium text-emerald-700 dark:text-emerald-300" aria-label={t("desktopHealth.ready")}>
            <CheckCircle2 className="size-4" aria-hidden="true" />
            <span>{t("desktopHealth.ready")}</span>
        </span>
    );
}
