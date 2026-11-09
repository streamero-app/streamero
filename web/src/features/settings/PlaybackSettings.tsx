import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp } from "lucide-react";
import { Button, Card, ErrorText, Select, Spinner, SuccessText } from "@/components/ui";
import { subtitleCss } from "@/features/player/PlayerPage";
import { SettingRow, Toggle } from "./SettingRow";
import { HOME_SECTIONS, usePreferences, type HomeSection, type Prefs } from "./usePreferences";

const LANGS = ["hr", "en", "sr", "bs", "sl", "de", "fr", "es", "it", "pt", "nl", "pl", "cs", "hu", "ru", "ja", "ko", "zh"];
const MODES: Prefs["subtitleMode"][] = ["default", "smart", "always", "forced", "none"];
const SKIPS = [5, 10, 15, 30, 60];
const HEIGHTS = [0, 2160, 1440, 1080, 720, 480, 360];
const BITRATES = [0, 120_000, 80_000, 40_000, 20_000, 10_000, 8_000, 4_000, 2_000, 1_000];
const SEGMENT_MODES = ["button", "auto", "off"] as const;

function SaveBar({ p }: { p: ReturnType<typeof usePreferences> }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-4">
      <Button type="submit" disabled={p.save.isPending}>{t("common.save")}</Button>
      {p.save.isSuccess && <SuccessText>{t("common.saved")}</SuccessText>}
      <ErrorText error={p.save.error ?? p.current.error} />
    </div>
  );
}

/** Per-profile playback: languages, subtitle choice, controls, quality limits. */
export function PlaybackSettings() {
  const { t, i18n } = useTranslation();
  const p = usePreferences();
  if (p.current.isPending) return <Spinner />;
  const d = p.draft;
  const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? "en"], { type: "language" });
  const lang = (id: string, value: string | undefined, onChange: (v: string) => void) => (
    <Select id={id} className="h-10 w-full" value={value ?? ""} onChange={(e) => onChange(e.target.value)}>
      <option value="">{t("playbackPrefs.any")}</option>
      {LANGS.map((l) => <option key={l} value={l}>{names.of(l) ?? l}</option>)}
    </Select>
  );
  const fmtBitrate = (k: number) => (k >= 1000 ? `${k / 1000} Mbit/s` : `${k} kbit/s`);
  return (
    <form className="flex flex-col gap-6" onSubmit={(e) => { e.preventDefault(); p.submit(); }}>
      <Card title={t("playbackPrefs.languagesTitle")}>
        <p className="mb-4 text-sm text-muted">{t("playbackPrefs.intro")}</p>
        <SettingRow label={t("playbackPrefs.audio")} hint={t("playbackPrefs.audioHint")}>
          {(id) => lang(id, d.audioLanguage, (v) => p.set("audioLanguage", v || undefined))}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.subtitles")} hint={t("playbackPrefs.subtitlesHint")}>
          {(id) => lang(id, d.subtitleLanguage, (v) => p.set("subtitleLanguage", v || undefined))}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.mode")} hint={t(`playbackPrefs.mode_${d.subtitleMode}_hint`)}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.subtitleMode} onChange={(e) => p.set("subtitleMode", e.target.value as Prefs["subtitleMode"])}>
              {MODES.map((m) => <option key={m} value={m}>{t(`playbackPrefs.mode_${m}`)}</option>)}
            </Select>
          )}
        </SettingRow>
      </Card>

      <Card title={t("playbackPrefs.controlsTitle")}>
        <SettingRow label={t("playbackPrefs.skipBack")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.skipBackSeconds ?? 10} onChange={(e) => p.set("skipBackSeconds", Number(e.target.value))}>
              {SKIPS.map((s) => <option key={s} value={s}>{t("playbackPrefs.seconds", { count: s })}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.skipForward")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.skipForwardSeconds ?? 10} onChange={(e) => p.set("skipForwardSeconds", Number(e.target.value))}>
              {[...SKIPS, 120].map((s) => <option key={s} value={s}>{t("playbackPrefs.seconds", { count: s })}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.skipIntro")} hint={t("playbackPrefs.skipIntroHint")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.skipIntro ?? "button"} onChange={(e) => p.set("skipIntro", e.target.value as Prefs["skipIntro"])}>
              {SEGMENT_MODES.map((m) => <option key={m} value={m}>{t(`playbackPrefs.segment_${m}`)}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.skipCredits")} hint={t("playbackPrefs.skipCreditsHint")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.skipCredits ?? "button"} onChange={(e) => p.set("skipCredits", e.target.value as Prefs["skipCredits"])}>
              {SEGMENT_MODES.map((m) => <option key={m} value={m}>{t(`playbackPrefs.credits_${m}`)}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.autoplay")} hint={t("playbackPrefs.autoplayHint")}>
          {(id) => <Toggle id={id} checked={d.autoplayNext} onChange={(v) => p.set("autoplayNext", v)} />}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.upNext")} hint={t("playbackPrefs.upNextHint")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.upNextSeconds ?? 10} onChange={(e) => p.set("upNextSeconds", Number(e.target.value))}>
              {[0, 5, 10, 15, 20, 30].map((s) => <option key={s} value={s}>{s === 0 ? t("playbackPrefs.noCountdown") : t("playbackPrefs.seconds", { count: s })}</option>)}
            </Select>
          )}
        </SettingRow>
      </Card>

      <Card title={t("playbackPrefs.qualityTitle")}>
        <p className="mb-4 text-sm text-muted">{t("playbackPrefs.qualityIntro")}</p>
        <SettingRow label={t("playbackPrefs.maxHeight")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.maxHeight ?? 0} onChange={(e) => p.set("maxHeight", Number(e.target.value) || undefined)}>
              {HEIGHTS.map((h) => <option key={h} value={h}>{h === 0 ? t("playbackPrefs.original") : h === 2160 ? "4K (2160p)" : `${h}p`}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("playbackPrefs.maxBitrate")} hint={t("playbackPrefs.maxBitrateHint")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.maxBitrateKbps ?? 0} onChange={(e) => p.set("maxBitrateKbps", Number(e.target.value) || undefined)}>
              {BITRATES.map((b) => <option key={b} value={b}>{b === 0 ? t("playbackPrefs.noLimit") : fmtBitrate(b)}</option>)}
            </Select>
          )}
        </SettingRow>
      </Card>
      <SaveBar p={p} />
    </form>
  );
}

/** How subtitles look in the player, with a live sample. */
export function SubtitleSettings() {
  const { t } = useTranslation();
  const p = usePreferences();
  if (p.current.isPending) return <Spinner />;
  const d = p.draft;
  const css = subtitleCss(d).replace("video::cue", ".sub-sample");
  return (
    <form className="flex flex-col gap-6" onSubmit={(e) => { e.preventDefault(); p.submit(); }}>
      <Card title={t("subtitleLook.title")}>
        <style>{css}</style>
        <div className="relative mb-6 aspect-video overflow-hidden rounded-[6px] bg-[radial-gradient(120%_90%_at_30%_20%,#3b4a5a_0%,#151a20_60%)]" aria-hidden>
          <span className="sub-sample absolute inset-x-0 text-center leading-snug" style={{ bottom: `${8 + (d.subtitlePosition ?? 0)}%` }}>
            <span className="sub-sample px-2 py-0.5">{t("subtitleLook.sample")}</span>
          </span>
        </div>
        <SettingRow label={t("subtitleLook.size")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.subtitleSize ?? "normal"} onChange={(e) => p.set("subtitleSize", e.target.value as Prefs["subtitleSize"])}>
              {(["small", "normal", "large", "xlarge"] as const).map((v) => <option key={v} value={v}>{t(`subtitleLook.size_${v}`)}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("subtitleLook.color")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.subtitleColor ?? "white"} onChange={(e) => p.set("subtitleColor", e.target.value as Prefs["subtitleColor"])}>
              {(["white", "yellow", "cyan"] as const).map((v) => <option key={v} value={v}>{t(`subtitleLook.color_${v}`)}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("subtitleLook.background")}>
          {(id) => (
            <Select id={id} className="h-10 w-full" value={d.subtitleBackground ?? "shadow"} onChange={(e) => p.set("subtitleBackground", e.target.value as Prefs["subtitleBackground"])}>
              {(["none", "shadow", "box"] as const).map((v) => <option key={v} value={v}>{t(`subtitleLook.background_${v}`)}</option>)}
            </Select>
          )}
        </SettingRow>
        <SettingRow label={t("subtitleLook.position")} hint={t("subtitleLook.positionHint")}>
          {(id) => (
            <input id={id} type="range" min={0} max={30} step={2} value={d.subtitlePosition ?? 0}
              onChange={(e) => p.set("subtitlePosition", Number(e.target.value) || undefined)} className="w-full accent-[var(--color-text)]" />
          )}
        </SettingRow>
      </Card>
      <SaveBar p={p} />
    </form>
  );
}

/** Which rows the home page shows, and in what order. */
export function HomeSettings() {
  const { t } = useTranslation();
  const p = usePreferences();
  if (p.current.isPending) return <Spinner />;
  const chosen = (p.draft.homeSections ?? [...HOME_SECTIONS]) as HomeSection[];
  const order: HomeSection[] = [...chosen, ...HOME_SECTIONS.filter((s) => !chosen.includes(s))];
  const setOn = (s: HomeSection, on: boolean) => {
    const next = on ? order.filter((x) => chosen.includes(x) || x === s) : chosen.filter((x) => x !== s);
    p.set("homeSections", next);
  };
  const move = (i: number, dir: -1 | 1) => {
    const next = [...order];
    const j = i + dir;
    if (j < 0 || j >= next.length) return;
    [next[i], next[j]] = [next[j]!, next[i]!];
    p.set("homeSections", next.filter((x) => chosen.includes(x)));
  };
  return (
    <form className="flex flex-col gap-6" onSubmit={(e) => { e.preventDefault(); p.submit(); }}>
      <Card title={t("homePrefs.title")}>
        <p className="mb-4 text-sm text-muted">{t("homePrefs.intro")}</p>
        <ol className="flex flex-col">
          {order.map((s, i) => {
            const on = chosen.includes(s);
            return (
              <li key={s} className="flex items-center gap-3 border-t border-border py-3 first:border-t-0" data-testid="home-section">
                <span className="w-6 text-right text-sm text-text/40 tabular">{on ? chosen.indexOf(s) + 1 : ""}</span>
                <span className={`flex-1 text-[15px] ${on ? "text-text" : "text-text/45"}`}>{t(`homePrefs.section_${s}`)}</span>
                <button type="button" aria-label={t("homePrefs.moveUp")} disabled={!on || chosen.indexOf(s) === 0} onClick={() => move(i, -1)}
                  className="grid size-8 place-items-center rounded-[4px] text-text/70 hover:bg-white/10 disabled:opacity-25"><ArrowUp className="size-4" aria-hidden /></button>
                <button type="button" aria-label={t("homePrefs.moveDown")} disabled={!on || chosen.indexOf(s) === chosen.length - 1} onClick={() => move(i, 1)}
                  className="grid size-8 place-items-center rounded-[4px] text-text/70 hover:bg-white/10 disabled:opacity-25"><ArrowDown className="size-4" aria-hidden /></button>
                <Toggle checked={on} onChange={(v) => setOn(s, v)} label={t(`homePrefs.section_${s}`)} />
              </li>
            );
          })}
        </ol>
      </Card>
      <SaveBar p={p} />
    </form>
  );
}
