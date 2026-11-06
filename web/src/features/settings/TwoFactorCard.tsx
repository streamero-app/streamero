import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, TextField } from "@/components/ui";
import { meQueryKey, useMe } from "@/features/auth/useAuth";

/** Turn TOTP two-factor authentication on (QR / link / manual key) or off. */
export function TwoFactorCard() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [recovery, setRecovery] = useState<string[] | null>(null);
  const refreshMe = () => qc.invalidateQueries({ queryKey: meQueryKey });

  const start = useMutation({ mutationFn: async () => unwrap(await api.POST("/auth/totp/setup")) });
  const enable = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/enable", { body: { code } })),
    onSuccess: (res) => {
      setRecovery(res.recoveryCodes);
      setCode("");
      start.reset();
      void refreshMe();
    },
  });
  const disable = useMutation({
    mutationFn: async () => unwrap(await api.POST("/auth/totp/disable", { body: { password } })),
    onSuccess: () => {
      setPassword("");
      void refreshMe();
    },
  });

  const onEnable = (e: FormEvent) => {
    e.preventDefault();
    enable.mutate();
  };
  const onDisable = (e: FormEvent) => {
    e.preventDefault();
    disable.mutate();
  };

  return (
    <Card title={<span className="flex items-center gap-2">{t("twofa.title")} {me?.totpEnabled
      ? <Badge tone="success">{t("twofa.on")}</Badge> : <Badge>{t("twofa.off")}</Badge>}</span>}>
      {recovery && (
        <div className="mb-4 flex flex-col gap-2 rounded-md border border-success/40 bg-success/10 p-4" data-testid="recovery-codes">
          <p className="text-sm font-medium">{t("twofa.recoveryTitle")}</p>
          <p className="text-xs text-muted">{t("twofa.recoveryText")}</p>
          <ul className="grid grid-cols-2 gap-1 font-mono text-sm select-all">
            {recovery.map((c) => <li key={c}>{c}</li>)}
          </ul>
          <Button variant="ghost" className="self-start" onClick={() => setRecovery(null)}>{t("apikeys.done")}</Button>
        </div>
      )}

      {me?.totpEnabled ? (
        <form className="flex max-w-sm flex-col gap-4" onSubmit={onDisable}>
          <p className="text-sm text-muted">{t("twofa.enabledText")}</p>
          <TextField label={t("login.password")} type="password" autoComplete="current-password" required
            value={password} onChange={(e) => setPassword(e.target.value)} />
          <ErrorText error={disable.error} />
          <Button type="submit" variant="danger" className="self-start" disabled={disable.isPending}>{t("twofa.disable")}</Button>
        </form>
      ) : !start.data ? (
        <div className="flex flex-col gap-4">
          <p className="text-sm text-muted">{t("twofa.intro")}</p>
          <ErrorText error={start.error} />
          <Button className="self-start" disabled={start.isPending} onClick={() => start.mutate()}>{t("twofa.enable")}</Button>
        </div>
      ) : (
        <form className="flex flex-col gap-4" onSubmit={onEnable}>
          <p className="text-sm">{t("twofa.step1")}</p>
          <div className="flex flex-wrap items-start gap-4">
            <img src={`data:image/png;base64,${start.data.qrPng}`} alt={t("twofa.qrAlt")} width={176} height={176}
              className="rounded-md bg-white p-2 [image-rendering:pixelated]" />
            <div className="flex min-w-0 flex-1 flex-col gap-2 text-sm">
              <a href={start.data.otpauthUri} className="font-medium text-accent">{t("twofa.openApp")}</a>
              <span className="text-muted">{t("twofa.manual")}</span>
              <code className="rounded bg-bg px-2 py-1 font-mono break-all select-all" data-testid="totp-secret">{start.data.secret}</code>
            </div>
          </div>
          <div className="flex max-w-sm flex-col gap-4">
            <TextField label={t("twofa.code")} inputMode="numeric" autoComplete="one-time-code" required maxLength={8}
              value={code} onChange={(e) => setCode(e.target.value)} />
            <ErrorText error={enable.error} />
            <div className="flex gap-2">
              <Button type="submit" disabled={enable.isPending}>{t("twofa.confirm")}</Button>
              <Button type="button" variant="ghost" onClick={() => start.reset()}>{t("common.cancel")}</Button>
            </div>
          </div>
        </form>
      )}
    </Card>
  );
}
