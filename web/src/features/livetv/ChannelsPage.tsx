import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Heart, Play } from "lucide-react";
import { ErrorText, Spinner } from "@/components/ui";
import { ProgressBar } from "@/features/library/cards";
import { useMe } from "@/features/auth/useAuth";
import { canManageLiveTv, useChannels, useFavorite, type Category, type Program } from "./api";
import { ChannelBadge, GuideFilters, useNow } from "./GuidePage";
import { ProgramDialog } from "./ProgramDialog";
import { progress, timeOf } from "./time";

/** Channel list with what is on now (and how far in) and what is next. */
export function ChannelsPage() {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage;
  const { data: me } = useMe();
  const now = useNow();
  const [category, setCategory] = useState<Category | undefined>();
  const [favorites, setFavorites] = useState(false);
  const [open, setOpen] = useState<Program | null>(null);
  const channels = useChannels(category, favorites);
  const fav = useFavorite();
  const list = channels.data ?? [];

  return (
    <section className="flex flex-col gap-4" data-testid="channels-page">
      <GuideFilters category={category} setCategory={setCategory} favorites={favorites} setFavorites={setFavorites} />
      <ErrorText error={channels.error ?? fav.error} />
      {channels.isPending ? <Spinner /> : list.length === 0 ? (
        <p className="rounded-card border border-border bg-surface p-6 text-muted">
          {favorites ? t("livetv.noFavorites") : canManageLiveTv(me) ? t("livetv.emptyAdmin") : t("livetv.empty")}
        </p>
      ) : (
        <ul className="flex flex-col divide-y divide-border overflow-hidden rounded-card border border-border bg-surface">
          {list.map((ch) => (
            <li key={ch.id} className="flex items-center gap-4 px-4 py-3 sm:px-5" data-testid="channel-row">
              <Link to="/livetv/watch/$channelId" params={{ channelId: ch.id }} aria-label={t("livetv.watchChannel", { name: ch.name })}
                className="tv-focus group relative shrink-0 rounded-[6px]">
                <ChannelBadge channel={ch} size={52} />
                <span className="absolute inset-0 grid place-items-center rounded-[6px] bg-black/60 opacity-0 transition-opacity group-hover:opacity-100">
                  <Play className="size-5 fill-current" aria-hidden />
                </span>
              </Link>
              <div className="min-w-0 flex-1">
                <p className="flex items-baseline gap-2 truncate">
                  <span className="text-xs font-semibold text-text/50 tabular">{ch.number}</span>
                  <span className="truncate font-medium">{ch.name}</span>
                </p>
                {ch.now ? (
                  <button type="button" className="mt-1 block w-full max-w-xl text-left" onClick={() => setOpen({ ...ch.now!, channelName: ch.name, channelNumber: ch.number })}>
                    <span className="flex items-baseline gap-2 text-sm">
                      <span className="text-[11px] font-bold tracking-wide text-[var(--amb-a)] uppercase">{t("livetv.nowShort")}</span>
                      <span className="truncate text-text/90">{ch.now.title}</span>
                      <span className="ml-auto shrink-0 text-xs text-text/50 tabular">{timeOf(ch.now.start, lang)}–{timeOf(ch.now.end, lang)}</span>
                    </span>
                    <ProgressBar value={progress(ch.now, now) * 100} className="mt-1.5" />
                  </button>
                ) : (
                  <p className="mt-1 text-sm text-text/45">{t("livetv.noGuide")}</p>
                )}
                {ch.next && (
                  <button type="button" className="mt-1 flex w-full max-w-xl items-baseline gap-2 text-left text-[13px] text-text/55"
                    onClick={() => setOpen({ ...ch.next!, channelName: ch.name, channelNumber: ch.number })}>
                    <span className="text-[11px] font-semibold uppercase">{t("livetv.nextShort")}</span>
                    <span className="truncate">{ch.next.title}</span>
                    <span className="ml-auto shrink-0 tabular">{timeOf(ch.next.start, lang)}</span>
                  </button>
                )}
              </div>
              <button type="button" aria-pressed={ch.favorite} disabled={fav.isPending}
                aria-label={ch.favorite ? t("livetv.unfavorite", { name: ch.name }) : t("livetv.makeFavorite", { name: ch.name })}
                onClick={() => fav.mutate({ channelId: ch.id, on: !ch.favorite })}
                className="grid size-10 shrink-0 place-items-center rounded-[6px] text-text/60 transition-colors hover:bg-white/[0.07] hover:text-text">
                <Heart className={`size-5 ${ch.favorite ? "fill-[var(--amb-a)] text-[var(--amb-a)]" : ""}`} aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      )}
      {open && <ProgramDialog program={open} onClose={() => setOpen(null)} />}
    </section>
  );
}
