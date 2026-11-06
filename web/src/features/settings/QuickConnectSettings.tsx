import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Button, Card, ErrorText, SuccessText, TextField } from "@/components/ui";

/** Approve a code shown on a new device; that device is then signed in as you. */
export function QuickConnectSettings() {
  const { t } = useTranslation();
  const [code, setCode] = useState("");
  const authorize = useMutation({
    mutationFn: async () => unwrap(await api.POST("/quickconnect/authorize", { body: { code } })),
    onSuccess: () => setCode(""),
  });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    authorize.mutate();
  };
  return (
    <Card title={t("settings.quickConnect")}>
      <p className="mb-4 text-sm text-muted">{t("quickconnect.settingsIntro")}</p>
      <form className="flex max-w-sm flex-col gap-4" onSubmit={onSubmit}>
        <TextField label={t("quickconnect.code")} inputMode="numeric" autoComplete="off" required maxLength={7}
          placeholder="123456" value={code} onChange={(e) => setCode(e.target.value)} />
        <ErrorText error={authorize.error} />
        {authorize.isSuccess && (
          <SuccessText>{t("quickconnect.approved", { device: authorize.data.deviceName || t("devices.unknown") })}</SuccessText>
        )}
        <Button type="submit" disabled={authorize.isPending} className="self-start">{t("quickconnect.approve")}</Button>
      </form>
    </Card>
  );
}
