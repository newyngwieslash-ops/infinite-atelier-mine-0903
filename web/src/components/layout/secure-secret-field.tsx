import { Button, Input, Space, Tag } from "antd";
import { KeyRound, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { toProviderId } from "@/services/desktop/provider-id";
import { deleteSecret, getSecretStatus, isSecureProviderMode, setSecret } from "@/services/desktop/providers";

export { toProviderId };

type SecretState = { configured: boolean; hint: string; available: boolean };

/**
 * Secure secret input for a provider.
 *
 * The plaintext exists only in this component's local state while the user is
 * typing and is submitted directly to the Go binding, which writes it to the
 * OS credential store. It is never persisted, logged, or returned: the
 * status query only ever yields configured/not-configured plus a short
 * display hint.
 */
export function SecureSecretField({ providerId }: { providerId: string }) {
    const { t } = useTranslation();
    const [state, setState] = useState<SecretState>({ configured: false, hint: "", available: true });
    const [value, setValue] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");

    const refresh = useCallback(async () => {
        const status = await getSecretStatus(providerId);
        setState({
            configured: Boolean(status?.configured),
            hint: status?.displayHint || "",
            available: !status || Boolean(status.available),
        });
    }, [providerId]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const submit = async () => {
        if (!value.trim()) return;
        setBusy(true);
        setError("");
        try {
            await setSecret(providerId, value);
            // Clear the local plaintext immediately after the write.
            setValue("");
            await refresh();
        } catch {
            setError(t("config.channelEditor.secretSaveFailed"));
        } finally {
            setBusy(false);
        }
    };

    const remove = async () => {
        setBusy(true);
        setError("");
        try {
            await deleteSecret(providerId);
            await refresh();
        } catch {
            setError(t("config.channelEditor.secretDeleteFailed"));
        } finally {
            setBusy(false);
        }
    };

    if (!isSecureProviderMode()) {
        return null;
    }

    return (
        <div className="block md:col-span-2">
            <div className="mb-1 flex items-center gap-2">
                <span className="text-sm font-medium">{t("config.channelEditor.secret")}</span>
                {state.configured ? (
                    <Tag color="green">{t("config.channelEditor.secretConfigured")}{state.hint ? ` ${state.hint}` : ""}</Tag>
                ) : (
                    <Tag>{t("config.channelEditor.secretMissing")}</Tag>
                )}
                {!state.available ? <Tag color="orange">{t("config.channelEditor.secretUnavailable")}</Tag> : null}
            </div>
            <Space.Compact className="w-full">
                <Input.Password
                    value={value}
                    onChange={(event) => setValue(event.target.value)}
                    placeholder={state.configured ? t("config.channelEditor.secretReplacePlaceholder") : "sk-..."}
                    prefix={<KeyRound className="size-4 text-stone-400" />}
                    autoComplete="off"
                    disabled={!state.available || busy}
                    onPressEnter={() => void submit()}
                />
                <Button type="primary" onClick={() => void submit()} loading={busy} disabled={!state.available || !value.trim()}>
                    {state.configured ? t("config.channelEditor.secretReplace") : t("config.channelEditor.secretSave")}
                </Button>
                {state.configured ? <Button danger type="text" icon={<Trash2 className="size-4" />} onClick={() => void remove()} disabled={busy} aria-label={t("config.channelEditor.secretDelete")} /> : null}
            </Space.Compact>
            <div className="mt-1 text-xs text-stone-500">{t("config.channelEditor.secretHint")}</div>
            {error ? <div className="mt-1 text-xs text-red-500">{error}</div> : null}
        </div>
    );
}
