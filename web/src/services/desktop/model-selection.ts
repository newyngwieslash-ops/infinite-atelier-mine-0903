/**
 * Channel/model encoding shared by the secure text path.
 *
 * The settings UI stores a model selection as `"<channelId>::<model>"`
 * (`encodeChannelModel` in the config store). The Go provider gateway only
 * knows provider IDs and bare model names, so the secure path must decode the
 * selection and route the request to the matching provider — sending the
 * encoded string as the API model name would ask the provider for a model
 * literally named `default::gpt-5.5`.
 */
const CHANNEL_MODEL_SEPARATOR = "::";

export type DecodedModelSelection = {
    /** Provider/channel ID, or empty when the value was a bare model name. */
    channelId: string;
    /** Bare model name the provider expects. */
    model: string;
};

export function decodeModelSelection(value: string): DecodedModelSelection {
    const trimmed = (value || "").trim();
    const index = trimmed.indexOf(CHANNEL_MODEL_SEPARATOR);
    if (index < 0) {
        return { channelId: "", model: trimmed };
    }
    return {
        channelId: trimmed.slice(0, index),
        model: trimmed.slice(index + CHANNEL_MODEL_SEPARATOR.length),
    };
}

/**
 * Resolves the canonical channel ID for a model selection, honoring the
 * channel the selection was encoded with and otherwise falling back to the
 * channel that lists the model.
 */
export function channelIdForModel(config: { channels: Array<{ id: string; models?: Array<{ name: string }> }> }, value: string): string {
    const decoded = decodeModelSelection(value);
    if (decoded.channelId) return decoded.channelId;
    const matched = (config.channels || []).find((channel) => (channel.models || []).some((model) => model.name === decoded.model));
    return matched?.id || "";
}
