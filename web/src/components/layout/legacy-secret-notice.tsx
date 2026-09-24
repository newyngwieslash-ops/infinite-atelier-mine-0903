import { useEffect, useRef, useState } from "react";
import { Alert, App, Button } from "antd";
import { useTranslation } from "react-i18next";

import { useConfigStore } from "@/stores/use-config-store";
import { isSecureProviderMode } from "@/services/desktop/providers";
import { clearLegacyPlaintextKeys, hasLegacyPlaintextKeys, legacyKeyLocations } from "@/services/desktop/legacy-config";

/**
 * Warns once per session when a persisted legacy configuration still contains
 * plaintext API keys, and points the user at the secure input.
 *
 * This component deliberately does NOT read, copy, or clear the keys: removing
 * them without migrating the value would lose user data, and WP-02's boundary
 * excludes real credential migration. The user decides when to re-enter the
 * key through the secure field and can then clear the legacy value.
 */
/**
 * LegacySecretStatus renders the warning AND the control that clears it.
 *
 * It is a component rather than a toast because a toast cannot host an action the user may take a
 * minute later. It renders in the channels tab, beside the secure field the key is re-entered
 * through — the order the documented migration path requires: re-enter, THEN remove the plaintext
 * copy.
 *
 * Until WP-15 the notice reported a state it offered no way out of, so a plaintext key stayed
 * readable in DevTools for the life of the installation.
 */
export function LegacySecretStatus() {
    const { t } = useTranslation();
    const { message, modal } = App.useApp();
    const config = useConfigStore((state) => state.config);
    const [busy, setBusy] = useState(false);

    if (!isSecureProviderMode()) return null;
    if (!hasLegacyPlaintextKeys(config)) return null;
    const locations = legacyKeyLocations(config);

    const clear = () => {
        modal.confirm({
            title: t("secureSecrets.clearLegacyTitle"),
            content: t("secureSecrets.clearLegacyBody"),
            okButtonProps: { danger: true },
            onOk: async () => {
                setBusy(true);
                try {
                    // Through the store's own state, so the write goes through `partialize` — which
                    // is what guarantees the PERSISTED copy carries no key.
                    const next = useConfigStore.getState().config;
                    useConfigStore.setState({ config: clearLegacyPlaintextKeys(next) });
                    message.success(t("secureSecrets.clearLegacyDone"));
                } finally {
                    setBusy(false);
                }
            },
        });
    };

    return (
        <Alert
            type="warning"
            showIcon
            data-testid="legacy-secret-status"
            message={t("secureSecrets.legacyKeysDetected", {
                count: locations.channelCount + (locations.rootKey ? 1 : 0),
            })}
            action={
                <Button size="small" danger loading={busy} onClick={clear} data-testid="legacy-secret-clear">
                    {t("secureSecrets.clearLegacy")}
                </Button>
            }
        />
    );
}

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
