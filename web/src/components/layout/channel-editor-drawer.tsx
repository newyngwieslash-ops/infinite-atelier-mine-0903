import { Button, Drawer, Input, Segmented, Select, Space } from "antd";
import { ListPlus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { isSecureProviderMode, saveProviderConfig } from "@/services/desktop/providers";
import { toProviderConfigInput } from "@/services/desktop/provider-sync";
import { defaultBaseUrlForApiFormat, guessCapability, normalizeChannelModels, type ApiCallFormat, type ChannelModel, type ModelCapability, type ModelChannel } from "@/stores/use-config-store";
import { SecureSecretField, toProviderId } from "./secure-secret-field";
import { ModelSelectModal } from "./model-select-modal";

/**
 * The channel editor, with the per-model script control REMOVED rather than
 * hidden.
 *
 * A model could previously carry a user-authored JavaScript "call script" that
 * the frontend executed with `new Function`. PRD section 18 makes "仍存在任意模型
 * JavaScript 执行路径" a release blocker and `docs/SECURITY.md` section 5 forbids
 * the mechanism outright, so the runner and its editor are gone.
 *
 * # The gap this leaves, stated rather than papered over
 *
 * A model whose stored `script` was what made it work against a provider shape
 * the built-in adapters do not speak (a relay's custom polled video API, for
 * instance) now falls through to the standard OpenAI-compatible or Gemini call,
 * which may not serve it. There is NO replacement: the Go core has no
 * model-script runner, and adding one would be the same arbitrary-execution
 * surface under a different owner. The supported resolution is a provider that
 * speaks one of the two built-in formats. The editor therefore does not merely
 * lose a button — for such a model it loses the route to that provider, and
 * that is a capability loss this change accepts deliberately.
 *
 * The stored `script` value itself is NOT deleted: it is a field the user
 * authored, and silently dropping rows of their configuration during an upgrade
 * is the data loss AGENTS section 3 forbids. It is inert — nothing reads it —
 * and `normalizeChannelModels` still round-trips it.
 */
export function ChannelEditorDrawer({ open, channel, onSave, onClose }: { open: boolean; channel: ModelChannel | null; onSave: (channel: ModelChannel) => void; onClose: () => void }) {
    const { t } = useTranslation();
    const secureMode = isSecureProviderMode();
    const [draft, setDraft] = useState<ModelChannel | null>(channel);
    const [selectOpen, setSelectOpen] = useState(false);
    const apiFormatOptions: Array<{ label: string; value: ApiCallFormat }> = [
        { label: "OpenAI", value: "openai" },
        { label: "Gemini", value: "gemini" },
    ];
    const capabilityOptions: Array<{ label: string; value: ModelCapability }> = ["image", "video", "text", "audio"].map((value) => ({ label: t(`config.channelEditor.capabilities.${value}`), value: value as ModelCapability }));

    useEffect(() => {
        if (open && channel) setDraft(channel);
    }, [open, channel]);

    if (!draft) return null;

    const patch = (value: Partial<ModelChannel>) => setDraft((current) => (current ? { ...current, ...value } : current));
    const setModels = (models: ChannelModel[]) => patch({ models });

    const changeApiFormat = (apiFormat: ApiCallFormat) => {
        const baseUrl = !draft.baseUrl.trim() || draft.baseUrl.trim() === defaultBaseUrlForApiFormat(draft.apiFormat) ? defaultBaseUrlForApiFormat(apiFormat) : draft.baseUrl;
        patch({ apiFormat, baseUrl });
    };

    const applySelection = (names: string[]) => {
        const map = new Map(draft.models.map((model) => [model.name, model]));
        setModels(names.map((name) => map.get(name) || { name, capability: guessCapability(name) }));
    };

    const setCapability = (name: string, capability: ModelCapability) => setModels(draft.models.map((model) => (model.name === name ? { ...model, capability } : model)));
    const removeModel = (name: string) => setModels(draft.models.filter((model) => model.name !== name));

    const save = () => {
        const saved = { ...draft, name: draft.name.trim() || t("config.channels.unnamed"), models: normalizeChannelModels(draft.models) };
        onSave(saved);
        // Register the non-secret provider metadata in the Go registry so a
        // stored secret has a provider to attach to. The key itself is written
        // only through the secure field's secrets binding.
        if (secureMode && saved.baseUrl.trim()) {
            void saveProviderConfig(toProviderConfigInput(saved)).catch(() => undefined);
        }
        onClose();
    };

    return (
        <Drawer
            open={open}
            width={640}
            title={t("config.channelEditor.title")}
            onClose={onClose}
            styles={{ body: { paddingTop: 16 } }}
            extra={
                <Space>
                    <Button onClick={onClose}>{t("common.cancel")}</Button>
                    <Button type="primary" onClick={save}>
                        {t("common.save")}
                    </Button>
                </Space>
            }
        >
            <div className="grid gap-4 md:grid-cols-2">
                <label className="block">
                    <span className="mb-1 block text-sm font-medium">{t("config.channelEditor.name")}</span>
                    <Input value={draft.name} onChange={(event) => patch({ name: event.target.value })} />
                </label>
                <label className="block">
                    <span className="mb-1 block text-sm font-medium">{t("config.channelEditor.protocol")}</span>
                    <Select className="w-full" value={draft.apiFormat} options={apiFormatOptions} onChange={changeApiFormat} />
                </label>
                <label className="block md:col-span-2">
                    <span className="mb-1 block text-sm font-medium">{t("config.channelEditor.baseUrl")}</span>
                    <Input value={draft.baseUrl} onChange={(event) => patch({ baseUrl: event.target.value })} placeholder="https://api.example.com" />
                </label>
                {/*
                  Secure mode stores keys in the OS credential store through the
                  Go binding. The legacy persisted key field exists only for
                  browser development compatibility and is hidden here so a
                  secure desktop build never writes a key into local storage.
                */}
                <SecureSecretField providerId={toProviderId(draft.id)} />
                {!secureMode ? (
                    <label className="block md:col-span-2">
                        <span className="mb-1 block text-sm font-medium">API Key</span>
                        <Input.Password value={draft.apiKey} onChange={(event) => patch({ apiKey: event.target.value })} placeholder="sk-..." />
                    </label>
                ) : null}
            </div>

            <div className="mt-6 mb-3 flex flex-wrap items-center justify-between gap-2">
                <div>
                    <div className="text-sm font-semibold">{t("config.channelEditor.models")}</div>
                    <div className="mt-0.5 text-xs text-stone-500">{t("config.channelEditor.modelDescription", { count: draft.models.length })}</div>
                </div>
                <Button type="primary" icon={<ListPlus className="size-4" />} onClick={() => setSelectOpen(true)}>
                    {t("config.channelEditor.selectModels")}
                </Button>
            </div>

            <div className="space-y-2 rounded-lg border border-stone-200 p-2 dark:border-stone-800">
                {draft.models.length ? (
                    draft.models.map((model) => (
                        <div key={model.name} className="flex flex-wrap items-center gap-3 rounded-md px-2 py-1.5 hover:bg-stone-50 dark:hover:bg-stone-900/40">
                            <span className="min-w-0 flex-1 truncate text-sm" title={model.name}>
                                {model.name}
                            </span>
                            <div className="flex shrink-0 items-center gap-2">
                                <Segmented size="small" value={model.capability} options={capabilityOptions} onChange={(value) => setCapability(model.name, value as ModelCapability)} />
                                <Button size="small" danger type="text" icon={<Trash2 className="size-3.5" />} onClick={() => removeModel(model.name)} />
                            </div>
                        </div>
                    ))
                ) : (
                    <div className="px-2 py-8 text-center text-sm text-stone-500">{t("config.channelEditor.empty")}</div>
                )}
            </div>

            <ModelSelectModal open={selectOpen} channel={draft} selectedNames={draft.models.map((model) => model.name)} onConfirm={applySelection} onClose={() => setSelectOpen(false)} />
        </Drawer>
    );
}
