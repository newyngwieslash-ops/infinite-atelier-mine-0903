import { useEffect, useRef } from "react";
import { App } from "antd";
import { useTranslation } from "react-i18next";

import { hasLegacyDirectCalls } from "@/services/desktop/image-jobs";
import { isSecureProviderMode } from "@/services/desktop/providers";

/**
 * Tells the user, once per session, that video and audio generation still use
 * the legacy browser path.
 *
 * WP-03 migrated image generation to the Go job manager but deliberately left
 * video/audio on the direct browser path until the media work package provides
 * real adapters. Saying so is more honest than letting the user assume every
 * generation now goes through the secure gateway.
 */
export function LegacyMediaNotice() {
    const { message } = App.useApp();
    const { t } = useTranslation();
    const warned = useRef(false);

    useEffect(() => {
        if (warned.current) return;
        if (!isSecureProviderMode()) return;
        if (!hasLegacyDirectCalls()) return;
        warned.current = true;
        message.info(t("secureJobs.legacyMediaNotice"), 8);
    }, [message, t]);

    return null;
}
