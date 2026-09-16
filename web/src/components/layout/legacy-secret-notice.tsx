import { useEffect, useRef } from "react";
import { App } from "antd";
import { useTranslation } from "react-i18next";

import { useConfigStore } from "@/stores/use-config-store";
import { isSecureProviderMode } from "@/services/desktop/providers";
import { hasLegacyPlaintextKeys, legacyKeyLocations } from "@/services/desktop/legacy-config";

/**
 * Warns once per session when a persisted legacy configuration still contains
 * plaintext API keys, and points the user at the secure input.
 *
 * This component deliberately does NOT read, copy, or clear the keys: removing
 * them without migrating the value would lose user data, and WP-02's boundary
 * excludes real credential migration. The user decides when to re-enter the
 * key through the secure field and can then clear the legacy value.
 */
export function LegacySecretNotice() {
    const { message } = App.useApp();
    const { t } = useTranslation();
    const warned = useRef(false);

    useEffect(() => {
        if (warned.current) return;
        if (!isSecureProviderMode()) return;
        const { config } = useConfigStore.getState();
        if (!hasLegacyPlaintextKeys(config)) return;
        warned.current = true;
        const locations = legacyKeyLocations(config);
        message.warning(
            t("secureSecrets.legacyKeysDetected", {
                count: locations.channelCount + (locations.rootKey ? 1 : 0),
            }),
            8,
        );
    }, [message, t]);

    return null;
}
