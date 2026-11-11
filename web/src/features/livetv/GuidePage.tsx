import { useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, CircleDot, Heart, Repeat } from "lucide-react";
import { ErrorText, Spinner, pressable } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { CATEGORIES, canManageLiveTv, useGuide, type Category, type Channel, type Program } from "./api";
import { ProgramDialog } from "./ProgramDialog";
import { blockGeometry, dayOf, isAiring, shiftWindow, ticks, timeOf, windowStart } from "./time";

export const GUIDE_HOURS = 3;
export const PX_PER_MIN = 6;
const CHANNEL_W = 168;
const ROW_H = 64;

const chip =
  `${pressable} h-8 rounded-full px-3.5 text-[13px] ring-1 ring-inset ring-border text-text/70 hover:bg-white/[0.07] hover:text-text aria-pressed:bg-text aria-pressed:text-bg aria-pressed:ring-text`;
const navBtn = `${pressable} h-9 rounded-[6px] bg-white/[0.07] px-3 text-sm text-text hover:bg-white/[0.13]`;

/** Ticks once a minute so the now-line and "on now" highlighting move. */
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}

/** Category chips and the favourites switch, shared by the guide and the channel list. */
export function GuideFilters({ category, setCategory, favorites, setFavorites }: {
  category?: Category; setCategory: (c?: Category) => void; favorites: boolean; setFavorites: (v: boolean) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t("livetv.filter")}>
      <button type="button" className={chip} aria-pressed={!category} onClick={() => setCategory(undefined)}>{t("livetv.category.all")}</button>
      {CATEGORIES.map((c) => (
        <button key={c} type="button" className={chip} aria-pressed={category === c} onClick={() => setCategory(category === c ? undefined : c)}>
          {t(`livetv.category.${c}`)}
        </button>
      ))}
      <button type="button" className={chip} aria-pressed={favorites} onClick={() => setFavorites(!favorites)}>
        <Heart className={`size-3.5 ${favorites ? "fill-current" : ""}`} aria-hidden />{t("livetv.favorites")}
      </button>
    </div>
  );
}

export function ChannelBadge({ channel, size = 40 }: { channel: Pick<Channel, "name" | "logoUrl" | "number">; size?: number }) {
  const [broken, setBroken] = useState(false);
  if (channel.logoUrl && !broken) {
    return <img src={channel.logoUrl} alt="" onError={() => setBroken(true)} style={{ width: size, height: size }}
      className="shrink-0 rounded-[6px] bg-white/[0.06] object-contain p-1" />;
  }
  return (
    <span aria-hidden style={{ width: size, height: size }}
      className="grid shrink-0 place-items-center rounded-[6px] bg-white/[0.07] text-[11px] font-bold text-text/70 tabular">
      {channel.number || channel.name.slice(0, 2).toUpperCase()}
    </span>
  );
}

/** The guide grid: channels down the side, time across, scrollable by window. */
export function GuidePage() {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage;
  const { data: me } = useMe();
  const manage = canManageLiveTv(me);
  const now = useNow();
  const [start, setStart] = useState(() => windowStart(new Date()));
  const [category, setCategory] = useState<Category | undefined>();
  const [favorites, setFavorites] = useState(false);
  const [open, setOpen] = useState<Program | null>(null);
  const startIso = start.toISOString();
  const guide = useGuide(startIso, GUIDE_HOURS, category, favorites);
  const winStart = start.getTime();
  const winEnd = shiftWindow(start, GUIDE_HOURS).getTime();
  const trackW = GUIDE_HOURS * 60 * PX_PER_MIN;
  const tickList = useMemo(() => ticks(winStart, winEnd), [winStart, winEnd]);
  const nowLeft = now >= winStart && now < winEnd ? ((now - winStart) / 60_000) * PX_PER_MIN : null;
  const rows = guide.data?.rows ?? [];

  return (
    <section className="flex flex-col gap-4" data-testid="guide-page">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex items-center gap-1.5">
          <button type="button" className={navBtn} aria-label={t("livetv.earlier")} onClick={() => setStart(shiftWindow(start, -GUIDE_HOURS))}>
            <ChevronLeft className="size-4" aria-hidden />
          </button>
          <button type="button" className={navBtn} onClick={() => setStart(windowStart(new Date()))}>{t("livetv.now")}</button>
          <button type="button" className={navBtn} aria-label={t("livetv.later")} onClick={() => setStart(shiftWindow(start, GUIDE_HOURS))}>
            <ChevronRight className="size-4" aria-hidden />
          </button>
        </div>
        <p className="text-sm font-medium text-text/80 tabular" data-testid="guide-window">
          {dayOf(winStart, lang)} · {timeOf(winStart, lang)}–{timeOf(winEnd, lang)}
        </p>
        {guide.isFetching && <Spinner className="size-4" />}
        <div className="w-full lg:ml-auto lg:w-auto">
          <GuideFilters category={category} setCategory={setCategory} favorites={favorites} setFavorites={setFavorites} />
        </div>
      </div>

      <ErrorText error={guide.error} />
      {guide.isPending ? (
        <div className="flex h-64 items-center justify-center"><Spinner /></div>
      ) : rows.length === 0 ? (
        <p className="rounded-card border border-border bg-surface p-6 text-muted" data-testid="guide-empty">
          {favorites ? t("livetv.noFavorites") : manage ? t("livetv.emptyAdmin") : t("livetv.empty")}
        </p>
      ) : (
        <div className="shelf-track relative overflow-x-auto rounded-card border border-border bg-surface/60" role="grid" aria-label={t("livetv.guide")}>
          <div style={{ width: CHANNEL_W + trackW }} className="relative">
            <div className="sticky top-0 z-20 flex h-10 border-b border-border bg-surface-2/95 backdrop-blur" role="row">
              <div style={{ width: CHANNEL_W }} className="sticky left-0 z-10 shrink-0 bg-surface-2" />
              <div className="relative" style={{ width: trackW }}>
                {tickList.map((tk) => (
                  <span key={tk} data-testid="guide-tick" className="absolute top-0 flex h-full items-center border-l border-border pl-2 text-xs font-semibold text-text/60 tabular"
                    style={{ left: ((tk - winStart) / 60_000) * PX_PER_MIN }}>{timeOf(tk, lang)}</span>
                ))}
              </div>
            </div>
            {rows.map(({ channel, programs }) => (
              <div key={channel.id} role="row" className="flex border-b border-border/70 last:border-b-0" style={{ height: ROW_H }} data-testid="guide-row">
                <Link to="/livetv/watch/$channelId" params={{ channelId: channel.id }} role="rowheader"
                  style={{ width: CHANNEL_W }} title={t("livetv.watchChannel", { name: channel.name })}
                  className="tv-focus sticky left-0 z-10 flex shrink-0 items-center gap-2.5 border-r border-border bg-surface px-3 transition-colors hover:bg-surface-2">
                  <ChannelBadge channel={channel} size={36} />
                  <span className="min-w-0">
                    <span className="block text-[11px] font-semibold text-text/50 tabular">{channel.number}</span>
                    <span className="block truncate text-sm font-medium">{channel.name}</span>
                  </span>
                  {channel.favorite && <Heart className="ml-auto size-3 shrink-0 fill-[var(--amb-a)] text-[var(--amb-a)]" aria-label={t("livetv.favorite")} />}
                </Link>
                <div className="relative" style={{ width: trackW }}>
                  {programs.map((p) => {
                    const g = blockGeometry(p, winStart, winEnd, PX_PER_MIN);
                    if (!g) return null;
                    const live = isAiring(p, now);
                    const past = new Date(p.end).getTime() <= now;
                    return (
                      <button key={p.id + p.start} type="button" data-testid="guide-program" role="gridcell"
                        onClick={() => setOpen({ ...p, channelName: p.channelName ?? channel.name, channelNumber: p.channelNumber ?? channel.number })}
                        style={{ left: g.left, width: Math.max(0, g.width - 2) }}
                        className={`tv-focus absolute top-1 bottom-1 flex flex-col justify-center overflow-hidden rounded-[5px] px-2.5 text-left transition-colors duration-150 ${
                          live ? "bg-white/[0.12] ring-1 ring-inset ring-[var(--amb-a)]/60 hover:bg-white/[0.17]"
                            : past ? "bg-white/[0.03] text-text/45 hover:bg-white/[0.07]" : "bg-white/[0.06] hover:bg-white/[0.11]"}`}>
                        <span className="flex items-center gap-1.5 truncate text-[13px] font-semibold">
                          {p.recordingStatus === "recording" || p.recordingStatus === "scheduled" ? (
                            <CircleDot className={`size-3 shrink-0 text-danger ${p.recordingStatus === "recording" ? "animate-pulse" : ""}`} aria-label={t(`livetv.status.${p.recordingStatus}`)} />
                          ) : null}
                          {p.seriesRuleId && <Repeat className="size-3 shrink-0 text-text/60" aria-label={t("livetv.seriesActive")} />}
                          <span className="truncate">{g.clippedStart ? "‹ " : ""}{p.title}</span>
                        </span>
                        <span className="truncate text-[11px] text-text/55 tabular">
                          {timeOf(p.start, lang)}–{timeOf(p.end, lang)}{p.subtitle ? ` · ${p.subtitle}` : ""}
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
            {nowLeft !== null && (
              <div aria-hidden data-testid="guide-now-line" className="pointer-events-none absolute top-0 bottom-0 z-[5] w-0.5 bg-[var(--amb-a)] shadow-[0_0_12px_var(--amb-a)]"
                style={{ left: CHANNEL_W + nowLeft }} />
            )}
          </div>
        </div>
      )}
      {open && <ProgramDialog program={open} onClose={() => setOpen(null)} />}
    </section>
  );
}
