import { useEffect, useMemo, useRef } from "react";
import { Modal } from "antd";
import { useTranslation } from "react-i18next";

import {
    buildMonoformHostMessage,
    createMonoformNonce,
    monoformTargetOrigin,
    validateMonoformMessage,
    type MonoformCamera,
    type MonoformOpenShot,
} from "@/services/desktop/monoform-bridge";

type DirectorPanelProps = {
    nodeId: string;
    open: boolean;
    onClose: () => void;
    onExport: (kind: "image" | "video", blob: Blob) => void;
    /**
     * The shot this panel was opened from, or null when it was opened from the node alone.
     *
     * FR-060's "从一个 Shot 可打开导演预演并带入上下文": a panel opened from a shot carries that
     * shot's framing, camera and descriptions INTO the studio, so the user starts from what
     * the board decided rather than from an empty scene.
     */
    shot?: MonoformOpenShot | null;
    /**
     * Called when the studio reports a camera the user saved.
     *
     * FR-060's "保存后可在 Shot 中看到摄像机参数和预览图" is this callback plus the host's own
     * write: the panel does not persist anything, it reports what came back.
     */
    onShotUpdated?: (update: { shotId: string; camera: MonoformCamera; thumbnail?: Blob }) => void;
};

// Embedded MONOFORM previs studio panel for a director node.
//
// THE BRIDGE IS `services/desktop/monoform-bridge.ts`, and this component does not parse a
// message itself: there is exactly one place that decides whether a cross-iframe message is
// acceptable, and ARCHITECTURE §17 / SECURITY §12 require the checks it makes — schema
// version, nonce and origin. What the panel owns is the LIFECYCLE: a nonce per mount, the
// listener's registration and removal, and posting the shot once the frame is ready.
//
// The nonce is minted per MOUNT rather than per page, so a message replayed from a previous
// panel is refused: the frame that sent it is gone, and the shot it names may have changed.
export function DirectorPanel({ nodeId, open, onClose, onExport, shot, onShotUpdated }: DirectorPanelProps) {
    const { t } = useTranslation();
    const onExportRef = useRef(onExport);
    onExportRef.current = onExport;
    const onShotUpdatedRef = useRef(onShotUpdated);
    onShotUpdatedRef.current = onShotUpdated;

    // One nonce for this mount, and it survives re-renders: minting a new one on every render
    // would invalidate the message the studio is in the middle of sending.
    const nonce = useMemo(() => (open ? createMonoformNonce() : ""), [open]);
    const frameRef = useRef<HTMLIFrameElement | null>(null);
    const selfOrigin = typeof window !== "undefined" ? window.location.origin : "";

    useEffect(() => {
        if (!open || !nonce) return;
        const handler = (event: MessageEvent) => {
            const result = validateMonoformMessage({
                data: event.data,
                origin: event.origin,
                selfOrigin,
                nonce,
            });
            // A REFUSED message is ignored rather than reported to the user. The two reasons
            // are different: an unexpected frame is an attack or a stray extension, and a
            // malformed payload is a studio bug — and neither is something the person looking
            // at a previs panel can act on. What matters is that nothing is parsed.
            if (!result.ok) return;
            if (result.message.kind === "export") {
                onExportRef.current(result.message.exportKind, result.message.blob);
                return;
            }
            onShotUpdatedRef.current?.({
                shotId: result.message.shotId,
                camera: result.message.camera,
                thumbnail: result.message.thumbnail,
            });
        };
        window.addEventListener("message", handler);
        return () => window.removeEventListener("message", handler);
    }, [open, nonce, selfOrigin]);

    // The shot is posted when the frame has LOADED, not when the component renders: a message
    // sent to an iframe that has not navigated yet goes nowhere, and the studio would open on
    // an empty scene with no way to tell that the context was lost.
    const postShot = () => {
        if (!shot || !nonce || !frameRef.current?.contentWindow) return;
        frameRef.current.contentWindow.postMessage(
            buildMonoformHostMessage({ kind: "open_shot", shot }, nonce),
            monoformTargetOrigin(selfOrigin),
        );
    };

    return (
        <Modal
            open={open}
            onCancel={onClose}
            footer={null}
            width="min(96vw, 1280px)"
            centered
            destroyOnHidden
            title={shot?.shotNumber ? t("canvas.director.titleWithShot", { number: shot.shotNumber }) : t("canvas.director.title")}
            styles={{ body: { height: "min(84vh, 820px)", padding: 0, overflow: "hidden" } }}
        >
            <iframe
                ref={frameRef}
                src={`${import.meta.env.BASE_URL}monoform/index.html?key=${nodeId}`}
                title="MONOFORM"
                className="h-full w-full border-0"
                // SECURITY §12's "不授予与功能无关的浏览器权限": the studio renders a 3D scene and
                // needs none of these. Camera and microphone are deliberately ABSENT — the
                // previous value granted both, and nothing in the studio opens either.
                allow="clipboard-write; fullscreen"
                onLoad={postShot}
            />
        </Modal>
    );
}
