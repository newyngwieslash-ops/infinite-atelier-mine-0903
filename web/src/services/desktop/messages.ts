/**
 * Message shaping for the secure text path. Pure and dependency-free so it
 * can be unit-tested and so the flattening rule has one definition.
 *
 * The WP-02 secure adapter is text-only. Multimodal parts are flattened into
 * an explicit marker instead of being silently dropped or forwarded as raw
 * image bytes (image/vision support belongs to a later work package).
 */
export type SecureTextMessage = { role: "system" | "user" | "assistant"; content: string };

type MultimodalPart = { type?: string; text?: string; image_url?: { url?: string } };
type CanvasMessage = { role: "system" | "user" | "assistant"; content: string | MultimodalPart[] };

export function toSecureMessages(messages: CanvasMessage[]): SecureTextMessage[] {
    return messages.map((message) => ({
        role: message.role,
        content: typeof message.content === "string" ? message.content : flattenParts(message.content),
    }));
}

function flattenParts(parts: MultimodalPart[]): string {
    return parts
        .map((part) => {
            if (typeof part?.text === "string") return part.text;
            if (part?.image_url?.url) return "[image reference omitted: secure text mode does not upload images in this work package]";
            return "";
        })
        .filter(Boolean)
        .join("\n");
}
