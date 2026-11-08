import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, Card, ErrorText, Select, SuccessText } from "@/components/ui";

type Prefs = components["schemas"]["PlaybackPreferences"];
const LANGS = ["hr", "en", "sr", "bs", "sl", "de", "fr", "es", "it", "pt", "nl", "pl", "cs", "hu", "ru", "ja", "ko", "zh"];
const MODES: Prefs["subtitleMode"][] = ["default", "smart", "always", "forced", "none"];

/** Per-profile playback preferences: languages, subtitle mode, autoplay. */
export function PlaybackSettings() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const current = useQuery({ queryKey: ["preferences"], queryFn: async () => unwrap(await api.GET("/me/preferences")) });
  const [prefs, setPrefs] = useState<Prefs>({ subtitleMode: "default", autoplayNext: true });
  useEffect(() => { if (current.data) setPrefs(current.data); }, [current.data]);
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/me/preferences", { body: prefs })),
    onSuccess: (d) => qc.setQueryData(["preferences"], d),
  });
  const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? "en"], { type: "language" });
  const langSelect = (label: string, value: string | undefined, onChange: (v: string) => void) => (
    <label className="flex flex-col gap-1.5 text-sm">
      <span className="font-medium text-text/90">{label}</span>
      <Select className="h-10" value={value ?? ""} onChange={(e) => onChange(e.target.value)} aria-label={label}>
        <option value="">{t("playbackPrefs.any")}</option>
        {LANGS.map((l) => <option key={l} value={l}>{names.of(l) ?? l}</option>)}
      </Select>
    </label>
  );
  return (
    <Card title={t("playbackPrefs.title")}>
      <p className="mb-5 text-sm text-muted">{t("playbackPrefs.intro")}</p>
      <form className="flex flex-col gap-5" onSubmit={(e) => { e.preventDefault(); save.reset(); save.mutate(); }}>
        <div className="grid gap-5 sm:grid-cols-2">
          {langSelect(t("playbackPrefs.audio"), prefs.audioLanguage, (v) => setPrefs({ ...prefs, audioLanguage: v || undefined }))}
          {langSelect(t("playbackPrefs.subtitles"), prefs.subtitleLanguage, (v) => setPrefs({ ...prefs, subtitleLanguage: v || undefined }))}
        </div>
        <fieldset className="flex flex-col gap-2 text-sm">
          <legend className="mb-1 font-medium text-text/90">{t("playbackPrefs.mode")}</legend>
          {MODES.map((m) => (
            <label key={m} className="flex items-start gap-3 rounded-md px-1 py-1.5">
              <input type="radio" name="subtitleMode" value={m} checked={prefs.subtitleMode === m} className="mt-1 accent-[var(--color-accent)]"
                onChange={() => setPrefs({ ...prefs, subtitleMode: m })} />
              <span>
                <span className="font-medium">{t(`playbackPrefs.mode_${m}`)}</span>
                <span className="block text-xs text-muted">{t(`playbackPrefs.mode_${m}_hint`)}</span>
              </span>
            </label>
          ))}
        </fieldset>
        <label className="flex items-center gap-3 text-sm">
          <input type="checkbox" className="accent-[var(--color-accent)]" checked={prefs.autoplayNext}
            onChange={(e) => setPrefs({ ...prefs, autoplayNext: e.target.checked })} />
          {t("playbackPrefs.autoplay")}
        </label>
        <div><Button type="submit" disabled={save.isPending}>{t("common.save")}</Button></div>
        {save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
        <ErrorText error={save.error ?? current.error} />
      </form>
    </Card>
  );
}
