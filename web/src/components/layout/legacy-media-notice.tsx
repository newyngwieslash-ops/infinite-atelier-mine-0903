import { useEffect, useRef } from "react";
import { App } from "antd";
import { useTranslation } from "react-i18next";

import { hasLegacyDirectCalls } from "@/services/desktop/image-jobs";
import { isSecureProviderMode } from "@/services/desktop/providers";

/**
 * Tells the user, once per session, what a secure desktop build does with canvas
 * video and audio generation.
 *
 * # Why the message is not "these still use the browser path"
 *
 * That was true while the browser-direct calls were the live transport. They are
 * not, in this build: `video-generation.ts` and `audio-generation.ts` refuse in
 * secure mode instead of calling a provider from the webview, because the Go
 * media jobs name a storyboard shot and a dialogue line and a FREE CANVAS NODE is
 * neither.
 *
 * # Why it does not simply redirect the user to the studio
 *
 * An earlier version of this notice did, and it was wrong. The studio's video and
 * audio sections are the structurally correct route — they submit against a real
 * shot and dialogue line — but `Registry.VideoPortFor`/`AudioPortFor` resolve an
 * adapter only for `mock_media`, which no configuration can carry, so a studio job
 * fails at provider resolution too. Telling the user to go there would have moved
 * them from a clear refusal to an opaque job failure. The message therefore states
 * the whole truth: neither route produces media in this build, and a real adapter
 * is what is missing.
 *
 * # Why it still checks `hasLegacyDirectCalls`
 *
 * The legacy files survive for browser development mode, where there is no Go
 * core and the direct call is the only transport. A build without them is
 * possible, and this notice says nothing about a path that is not there.
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
