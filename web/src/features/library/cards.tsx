import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, ChevronLeft, ChevronRight, Play, Plus, Star } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Art } from "@/components/ui";
import { formatDuration, imageUrl, placeholderHue, type ItemSummary } from "./format";

export function progressOf(it: ItemSummary): number {
  return it.positionMs && it.durationMs ? Math.min(100, (it.positionMs / it.durationMs) * 100) : 0;
}

export function isPlayable(it: ItemSummary): boolean {
  return it.kind === "movie" || it.kind === "episode";
}

/** Artwork stand-in: the title set large on a muted, title-tinted ground. */
export function ArtFallback({ item, wide = false }: { item: ItemSummary; wide?: boolean }) {
  const hue = placeholderHue(item.title);
  return (
    <div className="absolute inset-0 flex flex-col justify-end p-3"
      style={{ background: `radial-gradient(120% 100% at 15% 0%, hsl(${hue} 18% 26%) 0%, hsl(${(hue + 30) % 360} 12% 13%) 55%, #0e0e0e 100%)` }}>
      <span className={`font-display leading-[0.95] text-white ${wide ? "line-clamp-2 text-[22px]" : "line-clamp-4 text-2xl"}`}>{item.title}</span>
      {item.year && !wide && <span className="mt-1 text-xs text-white/55 tabular">{item.year}</span>}
    </div>
  );
}

function landscapeSrc(it: ItemSummary): string | null {
  if (it.hasBackdrop) return imageUrl(it.id, "backdrop");
  if (it.kind === "episode" && it.hasPoster) return imageUrl(it.id, "still");
  return null;
}

export function ProgressBar({ value, className = "" }: { value: number; className?: string }) {
  if (!value) return null;
  return (
    <div className={`h-[3px] overflow-hidden bg-white/25 ${className}`} aria-hidden>
      <div className="h-full bg-accent" style={{ width: `${value}%` }} />
    </div>
  );
}

/** Landscape art of an item, with the title over it when the art has none. */
function Landscape({ item, showTitle = true }: { item: ItemSummary; showTitle?: boolean }) {
  const [broken, setBroken] = useState(false);
  const src = landscapeSrc(item);
  return (
    <>
      {src && !broken ? <Art src={src} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" /> : <ArtFallback item={item} wide />}
      {src && !broken && showTitle && (
        <>
          <div className="absolute inset-0 bg-gradient-to-t from-black/75 via-black/5 to-transparent" />
          <span className="absolute inset-x-3 bottom-2.5 line-clamp-2 text-[15px] leading-tight font-bold tracking-tight text-white drop-shadow">{item.title}</span>
        </>
      )}
    </>
  );
}

function useUserData(item: ItemSummary) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { favorite?: boolean; played?: boolean }) =>
      unwrap(await api.PUT("/items/{itemId}/user-data", { params: { path: { itemId: item.id } }, body })),
    onSuccess: () => {
      for (const key of [["item"], ["library-items"], ["favorites"], ["continue"], ["latest"], ["row"]]) void qc.invalidateQueries({ queryKey: key });
    },
  });
}

const roundBtn = "grid size-9 shrink-0 place-items-center rounded-full border-2 border-white/45 bg-black/40 text-white transition-[border-color,transform] duration-150 hover:border-white active:scale-95";

/** The enlarged card shown over a hovered title (desktop): art, quick actions and facts. */
function Preview({ item, anchor, onClose }: { item: ItemSummary; anchor: DOMRect; onClose: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const update = useUserData(item);
  const scale = 1.45;
  const width = anchor.width * scale;
  // Keep the preview on screen at the row edges.
  const left = Math.min(Math.max(12, anchor.left + anchor.width / 2 - width / 2), window.innerWidth - width - 12);
  const top = anchor.top + window.scrollY - (anchor.height * scale - anchor.height) / 2;
  const origin = anchor.left - left < 20 ? "left" : left + width > window.innerWidth - 30 ? "right" : "center";
  return createPortal(
    <div onMouseLeave={onClose} role="dialog" aria-label={item.title}
      className="absolute z-50 overflow-hidden rounded-md bg-surface shadow-[0_20px_50px_rgb(0_0_0/0.75)] [animation:grow_220ms_var(--ease-out)_both]"
      style={{ left, top, width, transformOrigin: `${origin} center` }}>
      <style>{"@keyframes grow{from{opacity:.4;transform:scale(.69)}to{opacity:1;transform:none}}"}</style>
      <Link to="/item/$itemId" params={{ itemId: item.id }} className="relative block aspect-video">
        <Landscape item={item} />
      </Link>
      <div className="flex flex-col gap-2.5 p-3.5">
        <div className="flex items-center gap-2">
          {isPlayable(item) && (
            <button type="button" aria-label={t("player.play")} className="grid size-9 place-items-center rounded-full bg-white text-black transition-transform hover:bg-white/85 active:scale-95"
              onClick={() => void navigate({ to: "/watch/$itemId", params: { itemId: item.id } })}>
              <Play className="ml-0.5 size-4 fill-current" aria-hidden />
            </button>
          )}
          <button type="button" className={roundBtn} aria-label={item.favorite ? t("library.removeFromList") : t("library.addToList")}
            aria-pressed={!!item.favorite} onClick={() => update.mutate({ favorite: !item.favorite })}>
            {item.favorite ? <Check className="size-4" aria-hidden /> : <Plus className="size-4" aria-hidden />}
          </button>
          <button type="button" className={`${roundBtn} ${item.played ? "!border-accent text-accent" : ""}`} aria-pressed={!!item.played}
            aria-label={item.played ? t("library.markUnplayed") : t("library.markPlayed")} onClick={() => update.mutate({ played: !item.played })}>
            <Check className="size-4" strokeWidth={item.played ? 3 : 2} aria-hidden />
          </button>
          <Link to="/item/$itemId" params={{ itemId: item.id }} className={`${roundBtn} ml-auto`} aria-label={t("library.details")}>
            <ChevronDown className="size-4" aria-hidden />
          </Link>
        </div>
        <p className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[13px] text-white/80 tabular">
          {item.rating ? <span className="flex items-center gap-1 font-semibold text-white"><Star className="size-3.5 fill-accent text-accent" aria-hidden />{item.rating.toFixed(1)}</span> : null}
          {item.year && <span>{item.year}</span>}
          {item.durationMs ? <span>{formatDuration(item.durationMs)}</span> : null}
          <span className="rounded-sm border border-white/35 px-1 text-[11px] leading-4 text-white/70">{t(`library.kind_${item.kind}`)}</span>
        </p>
        <p className="truncate text-sm font-semibold">{item.title}</p>
        <ProgressBar value={progressOf(item)} className="rounded-full" />
      </div>
    </div>,
    document.body,
  );
}

/** Hover intent on a pointer device: open after a short rest, like streaming apps. */
function useHoverPreview() {
  const [rect, setRect] = useState<DOMRect | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const fine = typeof window !== "undefined" && window.matchMedia?.("(hover: hover) and (pointer: fine)").matches;
  const enter = (el: HTMLElement) => {
    if (!fine) return;
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setRect(el.getBoundingClientRect()), 420);
  };
  const leave = () => window.clearTimeout(timer.current);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  useEffect(() => {
    if (!rect) return;
    const close = () => setRect(null);
    window.addEventListener("scroll", close, { passive: true });
    return () => window.removeEventListener("scroll", close);
  }, [rect]);
  return { rect, enter, leave, close: () => setRect(null) };
}

/** 16:9 title card used in rows. Continue-watching cards play directly. */
export function TitleCard({ item, className = "", play = false }: { item: ItemSummary; className?: string; play?: boolean }) {
  const hover = useHoverPreview();
  const ref = useRef<HTMLAnchorElement>(null);
  const progress = progressOf(item);
  const link = play && isPlayable(item)
    ? { to: "/watch/$itemId" as const, params: { itemId: item.id } }
    : { to: "/item/$itemId" as const, params: { itemId: item.id } };
  return (
    <>
      <Link {...link} ref={ref} data-testid="item-card"
        onMouseEnter={(e) => hover.enter(e.currentTarget)} onMouseLeave={hover.leave}
        className={`tv-focus group relative block shrink-0 transition-transform duration-200 ease-out ${className}`}>
        <div className="relative aspect-video overflow-hidden rounded-md bg-surface-2">
          <Landscape item={item} />
          {progress > 0 && <ProgressBar value={progress} className="absolute inset-x-2.5 bottom-1.5 rounded-full" />}
        </div>
      </Link>
      {hover.rect && <Preview item={item} anchor={hover.rect} onClose={hover.close} />}
    </>
  );
}

/** Portrait poster card (grids, search). */
export function ItemCard({ item, className = "" }: { item: ItemSummary; className?: string }) {
  const { t } = useTranslation();
  const [broken, setBroken] = useState(false);
  const art = item.hasPoster && !broken;
  return (
    <Link to="/item/$itemId" params={{ itemId: item.id }} data-testid="item-card"
      className={`tv-focus group flex flex-col gap-2 transition-transform duration-200 ease-out ${className}`}>
      <div className="relative aspect-[2/3] overflow-hidden rounded-md bg-surface-2 transition-[transform,box-shadow] duration-300 ease-out group-hover:scale-[1.04] group-hover:shadow-[0_16px_40px_rgb(0_0_0/0.6)]">
        {art ? (
          <Art src={imageUrl(item.id, item.kind === "episode" ? "still" : "poster")} onError={() => setBroken(true)} className="absolute inset-0 size-full object-cover" />
        ) : <ArtFallback item={item} />}
        {item.played && (
          <span className="absolute top-2 right-2 grid size-6 place-items-center rounded-full bg-black/70 text-accent">
            <Check className="size-3.5" strokeWidth={3} aria-label={t("library.watched")} />
          </span>
        )}
        <ProgressBar value={progressOf(item)} className="absolute inset-x-0 bottom-0" />
      </div>
      <div className="min-w-0 px-0.5">
        <p className="truncate text-[13px] font-medium text-white/85 group-hover:text-white">{item.title}</p>
        <p className="flex items-center gap-1.5 text-xs text-muted tabular">
          {item.year}
          {item.rating ? <>{item.year && <span aria-hidden className="text-faint">·</span>}<Star className="size-3 fill-accent text-accent" aria-hidden />{item.rating.toFixed(1)}</> : null}
        </p>
      </div>
    </Link>
  );
}

/** A titled horizontal row with edge paddles on hover (desktop) and native swipe (touch). */
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
  const page = (dir: 1 | -1) => track.current?.scrollBy({ left: dir * track.current.clientWidth * 0.9, behavior: "smooth" });
  const paddle = "absolute inset-y-0 z-10 hidden w-10 place-items-center bg-black/45 text-white opacity-0 transition-opacity duration-200 group-hover/row:opacity-100 hover:bg-black/65 focus-visible:opacity-100 sm:grid lg:w-14";
  return (
    <section className="group/row relative flex flex-col gap-2.5" data-testid={testId}>
      <div className="flex items-baseline gap-4 px-4 sm:px-8 lg:px-14">
        <h2 className="text-[17px] font-bold tracking-tight sm:text-xl">{title}</h2>
        {action}
      </div>
      <div className="relative">
        {edges.left && (
          <button type="button" aria-label={t("home.scrollLeft")} onClick={() => page(-1)} className={`${paddle} left-0 rounded-r-md`}>
            <ChevronLeft className="size-8" aria-hidden />
          </button>
        )}
        <div ref={track} onScroll={measure}
          className="shelf-track flex gap-2 overflow-x-auto scroll-px-4 px-4 py-2 sm:scroll-px-8 sm:px-8 lg:scroll-px-14 lg:px-14">
          {children}
        </div>
        {edges.right && (
          <button type="button" aria-label={t("home.scrollRight")} onClick={() => page(1)} className={`${paddle} right-0 rounded-l-md`}>
            <ChevronRight className="size-8" aria-hidden />
          </button>
        )}
      </div>
    </section>
  );
}

/** Width of a landscape card so that a whole number of cards fills the row at each breakpoint. */
export const rowCard = "w-[44vw] sm:w-[30vw] md:w-[23vw] lg:w-[18.6vw] 2xl:w-[15.5vw]";
