import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, ChevronLeft, ChevronRight, Play, X } from "lucide-react";
import { Art } from "@/components/ui";
import { useAmbient } from "@/features/ambient/Ambient";
import { qualityTags, useEnhancements } from "@/features/enhancements/useEnhancements";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import { imageUrl, placeholderHue, type ItemSummary } from "./format";

export function progressOf(it: ItemSummary): number {
  return it.positionMs && it.durationMs ? Math.min(100, (it.positionMs / it.durationMs) * 100) : 0;
}

export function isPlayable(it: ItemSummary): boolean {
  return it.kind === "movie" || it.kind === "episode";
}

/** The card's own light (its first palette colour) for its progress line. */
function cardLight(it: ItemSummary): CSSProperties | undefined {
  return it.colors?.[0] ? ({ "--card-a": it.colors[0] } as CSSProperties) : undefined;
}

/** Artwork stand-in: the title set as a title card on a dark ground tinted by the title. */
export function ArtFallback({ item, wide = false }: { item: ItemSummary; wide?: boolean }) {
  const hue = placeholderHue(item.title);
  return (
    <div className="absolute inset-0 flex flex-col justify-end p-[8cqw] [container-type:inline-size]"
      style={{ background: `linear-gradient(160deg, hsl(${hue} 18% 20%) 0%, hsl(${(hue + 25) % 360} 12% 10%) 70%)` }}>
      <span className={`font-display break-words text-text ${wide ? "line-clamp-2 text-[8cqw]" : "line-clamp-4 text-[13cqw]"}`}>{item.title}</span>
      {item.year && !wide && <span className="mt-[3cqw] text-[7cqw] font-semibold text-text/55 tabular">{item.year}</span>}
    </div>
  );
}

/** The title as its logo artwork when there is one, else set as a title card. */
export function TitleArt({ item, className = "", textClass = "" }: { item: ItemSummary; className?: string; textClass?: string }) {
  const [broken, setBroken] = useState(false);
  if (item.hasLogo && !broken) {
    return (
      <img src={imageUrl(item.id, "logo")} alt={item.title} draggable={false} onError={() => setBroken(true)}
        className={`object-contain object-left-bottom drop-shadow-[0_2px_16px_rgb(0_0_0/0.6)] ${className}`} />
    );
  }
  return <span className={textClass}>{item.title}</span>;
}

/** A flat line of watched time in the film's colour. */
export function ProgressBar({ value, className = "" }: { value: number; className?: string }) {
  if (!value) return null;
  return (
    <div className={`h-[3px] bg-white/25 ${className}`} aria-hidden>
      <div className="h-full bg-[var(--card-a,var(--amb-a))]" style={{ width: `${value}%` }} />
    </div>
  );
}

/** Portrait artwork: the poster, else a centred crop of the backdrop with the title, else type. */
export function PortraitArt({ item, broken = false, onError }: { item: ItemSummary; broken?: boolean; onError?: () => void }) {
  if (item.hasPoster && !broken) {
    return <Art src={imageUrl(item.id, item.kind === "episode" ? "still" : "poster")} onError={onError} className="absolute inset-0 size-full object-cover" />;
  }
  if (item.hasBackdrop && !broken) {
    return (
      <>
        <Art src={imageUrl(item.id, "backdrop")} onError={onError} className="absolute inset-0 size-full object-cover" />
        <div className="absolute inset-0 flex items-end bg-gradient-to-t from-black/90 via-black/20 to-transparent p-[8cqw] [container-type:inline-size]">
          <span className="line-clamp-3 font-display text-[12cqw] break-words text-text">{item.title}</span>
        </div>
      </>
    );
  }
  return <ArtFallback item={item} />;
}

function landscapeSrc(it: ItemSummary): string | null {
  if (it.hasBackdrop) return imageUrl(it.id, "backdrop");
  if (it.kind === "episode" && it.hasPoster) return imageUrl(it.id, "still");
  return null;
}

/** Hovering or focusing a card brings its colour into the interface. */
function useCardLight(item: ItemSummary) {
  const { lightSoon } = useAmbient();
  return { onMouseEnter: () => lightSoon(item.colors), onFocus: () => lightSoon(item.colors) };
}

/**
 * Landscape card (continue watching, up next, my list): a frame from the film with the
 * title over it and the watched time underneath; continue-watching cards play directly.
 */
export function TitleCard({ item, className = "", play = false }: { item: ItemSummary; className?: string; play?: boolean }) {
  const { t } = useTranslation();
  const enh = useEnhancements();
  const qc = useQueryClient();
  const removable = play && enh.removeFromContinue && progressOf(item) > 0;
  const forget = async () => {
    await api.PUT("/items/{itemId}/progress", { params: { path: { itemId: item.id } }, body: { positionMs: 0 } });
    void qc.invalidateQueries({ queryKey: ["continue"] });
  };
  const [broken, setBroken] = useState(false);
  const light = useCardLight(item);
  const src = landscapeSrc(item);
  const progress = progressOf(item);
  const playable = play && isPlayable(item);
  const link = playable
    ? { to: "/watch/$itemId" as const, params: { itemId: item.id } }
    : { to: "/item/$itemId" as const, params: { itemId: item.id } };
  const left = progress > 0 && item.durationMs && item.positionMs ? Math.max(1, Math.round((item.durationMs - item.positionMs) / 60000)) : 0;
  return (
    <div className={`group/card relative shrink-0 ${className}`}>
    <Link {...link} data-testid="item-card" {...light} style={cardLight(item)}
      className="tv-focus group flex flex-col outline-none">
      <div className="poster-lift relative aspect-video overflow-hidden rounded-[6px] bg-surface-2">
        {src && !broken ? <Art src={src} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" /> : <ArtFallback item={item} wide />}
        <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/10 to-transparent" />
        {playable && (
          <span className="absolute top-3 right-3 grid size-10 place-items-center rounded-[6px] bg-text text-bg opacity-0 transition-opacity duration-200 group-hover:opacity-100 group-focus-visible:opacity-100">
            <Play className="ml-0.5 size-4 fill-current" aria-hidden />
          </span>
        )}
        <div className="absolute inset-x-0 bottom-0 flex flex-col gap-0.5 p-3.5 pb-3">
          <p className="truncate text-[15px] font-semibold text-text">{item.seriesTitle ?? item.title}</p>
          <p className="truncate text-[13px] text-text/65 tabular">
            {item.seriesTitle ? `${t("common.episodeCode", { season: item.seasonNumber, episode: item.episodeNumber })} · ${item.title}` : left ? t("home.minutesLeft", { count: left }) : item.year}
          </p>
        </div>
        {progress > 0 && <ProgressBar value={progress} className="absolute inset-x-0 bottom-0" />}
      </div>
    </Link>
    {removable && (
      <button type="button" onClick={() => void forget()} aria-label={t("home.removeFromContinue", { title: item.seriesTitle ?? item.title })}
        title={t("home.removeFromContinue", { title: item.seriesTitle ?? item.title })}
        className="tv-focus absolute top-3 left-3 grid size-8 place-items-center rounded-[6px] bg-black/70 text-text opacity-0 backdrop-blur transition-opacity group-hover/card:opacity-100 focus-visible:opacity-100 max-md:opacity-100">
        <X className="size-4" aria-hidden />
      </button>
    )}
    </div>
  );
}

/** Portrait poster card: the poster carries everything; the name sits quietly below. */
export function ItemCard({ item, className = "" }: { item: ItemSummary; className?: string }) {
  const { t } = useTranslation();
  const enh = useEnhancements();
  const tags = qualityTags(item.videoHeight, item.hdr);
  const [broken, setBroken] = useState(false);
  const light = useCardLight(item);
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="item-card" {...light} style={cardLight(item)}
      className={`tv-focus group flex flex-col gap-2.5 outline-none ${className}`}>
      <div className="poster-lift relative aspect-[2/3] overflow-hidden rounded-[6px] bg-surface-2 shadow-[0_12px_30px_-16px_rgb(0_0_0/0.9)]">
        <PortraitArt item={item} broken={broken} onError={() => setBroken(true)} />
        {item.played && (
          <span className="absolute top-2 right-2 grid size-7 place-items-center rounded-[4px] bg-black/70 text-text backdrop-blur">
            <Check className="size-4" strokeWidth={3} aria-label={t("library.watched")} />
          </span>
        )}
        {enh.qualityTags && tags.length > 0 && (
          <span className="absolute bottom-2 left-2 flex gap-1" data-testid="quality-tags">
            {tags.map((q) => <span key={q} className="rounded-[3px] bg-black/75 px-1.5 py-px text-[10px] font-bold tracking-wide text-text backdrop-blur">{q}</span>)}
          </span>
        )}
        <ProgressBar value={progressOf(item)} className="absolute inset-x-0 bottom-0" />
      </div>
      <div className="min-w-0">
        <p className="truncate text-[14px] font-semibold text-text/90 group-hover:text-text">{item.title}</p>
        <p className="text-[13px] text-text/50 tabular">
          {item.year}{item.rating ? <>{item.year && " · "}★ {item.rating.toFixed(1)}</> : null}
        </p>
      </div>
    </Link>
  );
}

/** A section: a wide caps title, then a horizontally scrolling strip (swipe on touch, arrows on desktop). */
export function Row({ title, testId, count, children, action }: { title: string; testId: string; count: number; children: ReactNode; action?: ReactNode }) {
  const { t } = useTranslation();
  const track = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ left: false, right: false });
  const measure = () => {
    const el = track.current;
    if (!el) return;
    setEdges({ left: el.scrollLeft > 4, right: el.scrollLeft + el.clientWidth < el.scrollWidth - 4 });
  };
  useLayoutEffect(() => {
    const el = track.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [count]);
  if (!count) return null;
  const page = (dir: 1 | -1) => track.current?.scrollBy({ left: dir * track.current.clientWidth * 0.85, behavior: "smooth" });
  const arrow = "grid size-9 place-items-center rounded-[6px] text-text/80 transition-colors hover:bg-white/10 hover:text-text disabled:opacity-20";
  return (
    <section className="flex flex-col gap-4" data-testid={testId}>
      <div className="flex items-end gap-4 px-5 sm:px-10 lg:px-16">
        <h2 className="font-head text-xl leading-none sm:text-[26px]">{title}</h2>
        {action}
        {(edges.left || edges.right) && (
          <div className="ml-auto hidden gap-1 sm:flex">
            <button type="button" aria-label={t("home.scrollLeft")} onClick={() => page(-1)} disabled={!edges.left} className={arrow}><ChevronLeft className="size-5" aria-hidden /></button>
            <button type="button" aria-label={t("home.scrollRight")} onClick={() => page(1)} disabled={!edges.right} className={arrow}><ChevronRight className="size-5" aria-hidden /></button>
          </div>
        )}
      </div>
      <div ref={track} onScroll={measure}
        className="shelf-track flex gap-4 overflow-x-auto scroll-px-5 px-5 pt-2 pb-6 sm:scroll-px-10 sm:px-10 lg:scroll-px-16 lg:gap-5 lg:px-16">
        {children}
      </div>
    </section>
  );
}

/** Landscape card width: a whole number of cards per row at each breakpoint. */
export const rowCard = "w-[78vw] sm:w-[44vw] md:w-[31vw] lg:w-[23.5vw] 2xl:w-[19vw]";
/** Poster card width in strips. */
export const posterCard = "w-[38vw] shrink-0 sm:w-[25vw] md:w-[18vw] lg:w-[13.5vw] 2xl:w-[11vw]";

/** Ranked poster: the rank set huge in the display face, the poster overlapping it. */
export function RankCard({ item, rank }: { item: ItemSummary; rank: number }) {
  const [broken, setBroken] = useState(false);
  const light = useCardLight(item);
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="item-card" aria-label={`${rank}. ${item.title}`} {...light} style={cardLight(item)}
      className="tv-focus group relative flex w-[50vw] shrink-0 items-end outline-none sm:w-[31vw] md:w-[23vw] lg:w-[17vw] 2xl:w-[14vw]">
      <span aria-hidden className="pointer-events-none -mr-[6%] w-[46%] text-right font-display text-[clamp(5.5rem,10vw,11rem)] leading-[0.74] text-transparent [-webkit-text-stroke:2px_rgb(244_239_233/0.55)] select-none">
        {rank}
      </span>
      <div className="poster-lift relative aspect-[2/3] w-[58%] overflow-hidden rounded-[6px] bg-surface-2">
        <PortraitArt item={item} broken={broken} onError={() => setBroken(true)} />
      </div>
    </Link>
  );
}

/** Placeholder row while titles load, shaped like the real one. */
export function RowSkeleton() {
  return (
    <div aria-hidden className="flex flex-col gap-4">
      <div className="mx-5 h-6 w-48 animate-pulse rounded-[4px] bg-white/[0.06] sm:mx-10 lg:mx-16" />
      <div className="flex gap-5 overflow-hidden px-5 py-2 sm:px-10 lg:px-16">
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className={`${posterCard} aspect-[2/3] animate-pulse rounded-[6px] bg-white/[0.05]`} style={{ animationDelay: `${i * 90}ms` }} />
        ))}
      </div>
    </div>
  );
}
