import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Button, Card, ErrorText, Spinner, SuccessText } from "@/components/ui";
import type { Enhancements } from "@/features/enhancements/useEnhancements";
import { SettingRow, Toggle } from "./SettingRow";

const KEYS: (keyof Enhancements)[] = ["qualityTags", "randomButton", "pauseScreen", "removeFromContinue", "shortcutsHelp", "autoPip", "discoverInSearch", "arrLinks"];

/**
 * Features other media servers add through plugins, built in. Each can be switched
 * off; nothing third-party runs inside the server.
 */
export function EnhancementsSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const cur = useQuery({ queryKey: ["enhancements"], queryFn: async () => unwrap(await api.GET("/enhancements")) });
  const [draft, setDraft] = useState<Enhancements | null>(null);
  useEffect(() => { if (cur.data) setDraft(cur.data); }, [cur.data]);
  const save = useMutation({
    mutationFn: async (e: Enhancements) => unwrap(await api.PUT("/enhancements", { body: e })),
    onSuccess: (d) => qc.setQueryData(["enhancements"], d),
  });
  if (cur.isPending || !draft) return <Spinner />;
  return (
    <form className="flex flex-col gap-6" onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(draft); }}>
      <Card title={t("enhancements.title")}>
        <p className="mb-4 max-w-[70ch] text-sm text-muted">{t("enhancements.intro")}</p>
        {KEYS.map((k) => (
          <SettingRow key={k} label={t(`enhancements.${k}`)} hint={t(`enhancements.${k}_hint`)}>
            {(id) => <Toggle id={id} checked={draft[k]} onChange={(v) => setDraft({ ...draft, [k]: v })} />}
          </SettingRow>
        ))}
        <div className="mt-5 flex flex-wrap items-center gap-4">
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
          {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
          <ErrorText error={save.error ?? cur.error} />
        </div>
      </Card>
    </form>
  );
}
