import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, ChevronLeft, ChevronRight, Play, Star } from "lucide-react";
import { Art } from "@/components/ui";
import { useAmbient } from "@/features/ambient/Ambient";
import { imageUrl, placeholderHue, type ItemSummary } from "./format";

export function progressOf(it: ItemSummary): number {
  return it.positionMs && it.durationMs ? Math.min(100, (it.positionMs / it.durationMs) * 100) : 0;
}

export function isPlayable(it: ItemSummary): boolean {
  return it.kind === "movie" || it.kind === "episode";
}

/** The card's own light (its first palette colour) for the glow. */
function cardLight(it: ItemSummary): CSSProperties | undefined {
  return it.colors?.[0] ? ({ "--card-a": it.colors[0] } as CSSProperties) : undefined;
}

/** Artwork stand-in: the title set large on a muted, title-tinted ground. */
export function ArtFallback({ item, wide = false }: { item: ItemSummary; wide?: boolean }) {
  const hue = placeholderHue(item.title);
  return (
    <div className="absolute inset-0 flex flex-col justify-end p-[8cqw] [container-type:inline-size]"
      style={{ background: `radial-gradient(120% 100% at 15% 0%, hsl(${hue} 22% 24%) 0%, hsl(${(hue + 30) % 360} 14% 12%) 55%, #0b0b0e 100%)` }}>
      <span className={`font-display leading-[0.95] break-words text-white ${wide ? "line-clamp-2 text-[9cqw]" : "line-clamp-4 text-[15cqw]"}`}>{item.title}</span>
      {item.year && !wide && <span className="mt-[2cqw] text-[8cqw] text-white/55 tabular">{item.year}</span>}
    </div>
  );
}

/** The title as its logo artwork when there is one, else set in type. */
export function TitleArt({ item, className = "", textClass = "" }: { item: ItemSummary; className?: string; textClass?: string }) {
  const [broken, setBroken] = useState(false);
  if (item.hasLogo && !broken) {
    return (
      <img src={imageUrl(item.id, "logo")} alt={item.title} draggable={false} onError={() => setBroken(true)}
        className={`object-contain object-left-bottom drop-shadow-[0_2px_12px_rgb(0_0_0/0.55)] ${className}`} />
    );
  }
  return <span className={textClass}>{item.title}</span>;
}

export function ProgressBar({ value, className = "" }: { value: number; className?: string }) {
  if (!value) return null;
  return (
    <div className={`h-[3px] overflow-hidden rounded-full bg-white/20 ${className}`} aria-hidden>
      <div className="h-full rounded-full bg-[var(--card-a,var(--amb-a))]" style={{ width: `${value}%` }} />
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
        <div className="absolute inset-0 flex items-end bg-gradient-to-t from-black/85 via-black/10 to-transparent p-[8cqw] [container-type:inline-size]">
          <span className="line-clamp-3 font-display text-[14cqw] leading-[0.95] break-words text-white">{item.title}</span>
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

/** Hovering or focusing a card lights the room with the card's colours. */
function useCardLight(item: ItemSummary) {
  const { lightSoon } = useAmbient();
  return { onMouseEnter: () => lightSoon(item.colors), onFocus: () => lightSoon(item.colors) };
}

/**
 * Landscape card (continue watching, up next, my list). The picture glows in its own
 * colour on focus; continue-watching cards play directly.
 */
export function TitleCard({ item, className = "", play = false }: { item: ItemSummary; className?: string; play?: boolean }) {
  const [broken, setBroken] = useState(false);
  const light = useCardLight(item);
  const src = landscapeSrc(item);
  const progress = progressOf(item);
  const link = play && isPlayable(item)
    ? { to: "/watch/$itemId" as const, params: { itemId: item.id } }
    : { to: "/item/$itemId" as const, params: { itemId: item.id } };
  return (
    <Link {...link} data-testid="item-card" {...light} style={cardLight(item)}
      className={`group flex shrink-0 flex-col gap-2.5 rounded-2xl outline-none ${className}`}>
      <div className="glow relative aspect-video overflow-hidden rounded-2xl bg-surface-2 group-hover:-translate-y-1 group-focus-visible:-translate-y-1 group-focus-visible:scale-[1.03]">
        {src && !broken ? <Art src={src} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" /> : <ArtFallback item={item} wide />}
        {play && isPlayable(item) && (
          <span className="absolute inset-0 grid place-items-center opacity-0 transition-opacity duration-200 group-hover:opacity-100 group-focus-visible:opacity-100">
            <span className="grid size-12 place-items-center rounded-full bg-[var(--card-a,var(--amb-a))] text-[var(--amb-ink)] shadow-xl">
              <Play className="ml-0.5 size-5 fill-current" aria-hidden />
            </span>
          </span>
        )}
        {progress > 0 && <ProgressBar value={progress} className="absolute inset-x-3 bottom-2.5" />}
      </div>
      <div className="min-w-0 px-1">
        <p className="truncate text-[14px] font-semibold text-white/90 group-hover:text-white">{item.seriesTitle ?? item.title}</p>
        <p className="truncate text-xs text-white/50 tabular">
          {item.seriesTitle ? `S${item.seasonNumber}:E${item.episodeNumber} · ${item.title}` : item.year}
        </p>
      </div>
    </Link>
  );
}

/** Portrait poster card: rounded, glowing in its own colour, lights the room on focus. */
export function ItemCard({ item, className = "" }: { item: ItemSummary; className?: string }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  const light = useCardLight(item);
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="item-card" {...light} style={cardLight(item)}
      className={`group flex flex-col gap-2.5 rounded-2xl outline-none ${className}`}>
      <div className="glow relative aspect-[2/3] overflow-hidden rounded-2xl bg-surface-2 group-hover:-translate-y-1.5 group-focus-visible:-translate-y-1.5 group-focus-visible:scale-[1.04]">
        <PortraitArt item={item} broken={broken} onError={() => setBroken(true)} />
        {item.played && (
          <span className="absolute top-2.5 right-2.5 grid size-7 place-items-center rounded-full bg-black/60 text-white backdrop-blur">
            <Check className="size-4" strokeWidth={3} aria-label={t("library.watched")} />
          </span>
        )}
        <ProgressBar value={progressOf(item)} className="absolute inset-x-3 bottom-2.5" />
      </div>
      <div className="min-w-0 px-1">
        <p className="truncate text-[13px] font-semibold text-white/85 group-hover:text-white">{item.title}</p>
        <p className="flex items-center gap-1.5 text-xs text-white/50 tabular">
          {item.year}
          {item.rating ? <>{item.year && <span aria-hidden>·</span>}<Star className="size-3 fill-current text-[var(--card-a,var(--amb-a))]" aria-hidden />{item.rating.toFixed(1)}</> : null}
        </p>
      </div>
    </Link>
  );
}

/** A section: a large title, then a horizontally scrolling strip (swipe on touch, arrows on desktop). */
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
  const arrow = "grid size-10 place-items-center rounded-full bg-white/[0.07] text-white ring-1 ring-white/10 backdrop-blur transition-colors hover:bg-white/15 disabled:opacity-25";
  return (
    <section className="flex flex-col gap-4" data-testid={testId}>
      <div className="flex items-end gap-4 px-5 sm:px-10 lg:px-16">
        <h2 className="font-display text-2xl leading-none sm:text-[32px]">{title}</h2>
        {action}
        <div className="ml-auto hidden gap-2 sm:flex">
          <button type="button" aria-label={t("home.scrollLeft")} onClick={() => page(-1)} disabled={!edges.left} className={arrow}><ChevronLeft className="size-5" aria-hidden /></button>
          <button type="button" aria-label={t("home.scrollRight")} onClick={() => page(1)} disabled={!edges.right} className={arrow}><ChevronRight className="size-5" aria-hidden /></button>
        </div>
      </div>
      <div ref={track} onScroll={measure}
        className="shelf-track flex gap-5 overflow-x-auto scroll-px-5 px-5 pt-2 pb-8 sm:scroll-px-10 sm:px-10 lg:scroll-px-16 lg:px-16">
        {children}
      </div>
    </section>
  );
}

/** Landscape card width: a whole number of cards per row at each breakpoint. */
export const rowCard = "w-[72vw] sm:w-[40vw] md:w-[30vw] lg:w-[23vw] 2xl:w-[19vw]";
/** Poster card width in strips. */
export const posterCard = "w-[40vw] shrink-0 sm:w-[26vw] md:w-[19vw] lg:w-[14vw] 2xl:w-[11.5vw]";

/** Ranked poster: the rank set in outlined display type beside the poster. */
export function RankCard({ item, rank }: { item: ItemSummary; rank: number }) {
  const [broken, setBroken] = useState(false);
  const light = useCardLight(item);
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="item-card" aria-label={`${rank}. ${item.title}`} {...light} style={cardLight(item)}
      className="group relative flex w-[52vw] shrink-0 items-end rounded-2xl outline-none sm:w-[32vw] md:w-[24vw] lg:w-[18vw] 2xl:w-[15vw]">
      <span aria-hidden className="pointer-events-none -mr-[8%] w-[48%] text-right font-display text-[clamp(6rem,11vw,12rem)] leading-[0.78] text-transparent [-webkit-text-stroke:clamp(2px,0.22vw,4px)_var(--card-a,var(--amb-a))] select-none">
        {rank}
      </span>
      <div className="glow relative aspect-[2/3] w-[56%] overflow-hidden rounded-2xl bg-surface-2 group-hover:-translate-y-1.5 group-focus-visible:-translate-y-1.5">
        <PortraitArt item={item} broken={broken} onError={() => setBroken(true)} />
      </div>
    </Link>
  );
}

/** Placeholder row while titles load, shaped like the real one. */
export function RowSkeleton() {
  return (
    <div aria-hidden className="flex flex-col gap-4">
      <div className="mx-5 h-7 w-56 animate-pulse rounded-lg bg-white/[0.06] sm:mx-10 lg:mx-16" />
      <div className="flex gap-5 overflow-hidden px-5 py-2 sm:px-10 lg:px-16">
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className={`${posterCard} aspect-[2/3] animate-pulse rounded-2xl bg-white/[0.05]`} style={{ animationDelay: `${i * 90}ms` }} />
        ))}
      </div>
    </div>
  );
}
