import { saveAs } from "file-saver";

import i18n from "@/i18n";
import { stripSecretsFromConfig } from "@/services/config-secrets";
import { useConfigStore, type AiConfig } from "@/stores/use-config-store";

type AppConfigFile = {
    app: "infinite-canvas";
    version: 1;
    exportedAt: string;
    config: AiConfig;
};

/**
 * Ordinary config export. Secrets are stripped first; the resulting file
 * contains no API keys even when the in-app config still holds legacy ones.
 */
export function exportAppConfig() {
    const { config } = useConfigStore.getState();
    const sanitized = stripSecretsFromConfig(config);
    const data: AppConfigFile = { app: "infinite-canvas", version: 1, exportedAt: new Date().toISOString(), config: sanitized };
    saveAs(new Blob([JSON.stringify(data, null, 2)], { type: "application/json;charset=utf-8" }), "infinite-canvas-config.json");
}

/**
 * Ordinary config import. An imported file may still carry legacy raw keys
 * from an older version, so it is sanitized before reaching the store; the
 * user must re-enter the key through the secure input.
 */
export async function importAppConfig(file: File) {
    let data: AppConfigFile;
    try {
        data = JSON.parse(await file.text()) as AppConfigFile;
    } catch {
        throw new Error(i18n.t("config.invalidFile"));
    }
    if (data.app !== "infinite-canvas" || data.version !== 1 || !data.config) throw new Error(i18n.t("config.invalidFile"));
    useConfigStore.setState({ config: stripSecretsFromConfig(data.config) });
}
