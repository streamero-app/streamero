import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronRight, Eye, Search } from "lucide-react";
import { Art } from "@/components/ui";
import { placeholderHue } from "@/features/library/format";
import { useAdultStatus } from "./api";

/**
 * Artwork that stays blurred until it is clicked (when the viewer keeps blurring
 * on). The first click on a blurred picture only reveals it.
 */
export function BlurArt({ src, blur, className = "", fallback }: { src?: string; blur: boolean; className?: string; fallback: ReactNode }) {
  const { t } = useTranslation();
  const [shown, setShown] = useState(false);
  const [broken, setBroken] = useState(false);
  const hidden = blur && !shown;
  if (!src || broken) return <>{fallback}</>;
  const reveal = (e: MouseEvent) => {
    if (!hidden) return;
    e.preventDefault();
    e.stopPropagation();
    setShown(true);
  };
  return (
    <div className={`absolute inset-0 overflow-hidden ${className}`} onClickCapture={reveal}>
      <Art src={src} onError={() => setBroken(true)}
        className={`absolute inset-0 size-full object-cover transition-[filter,transform] duration-500 ease-out ${hidden ? "scale-110 blur-2xl" : ""}`} />
      {hidden && (
        <button type="button" onClick={reveal} data-testid="adult-reveal"
          className="absolute inset-0 grid place-items-center bg-black/25 text-text/85 transition-colors hover:bg-black/10">
          <span className="flex items-center gap-1.5 rounded-full bg-black/55 px-3 py-1.5 text-xs font-semibold backdrop-blur-md">
            <Eye className="size-3.5" aria-hidden />{t("adult.reveal")}
          </span>
        </button>
      )}
    </div>
  );
}

export function Monogram({ name, round = false }: { name: string; round?: boolean }) {
  const hue = placeholderHue(name);
  return (
    <div className={`absolute inset-0 grid place-items-center ${round ? "rounded-full" : ""}`}
      style={{ background: `linear-gradient(150deg, hsl(${hue} 22% 22%) 0%, hsl(${(hue + 30) % 360} 14% 10%) 80%)` }}>
      <span className="font-display text-[clamp(1.5rem,4vw,2.6rem)] text-text/80">{name.slice(0, 1).toUpperCase()}</span>
    </div>
  );
}

export function useBlur(): boolean {
  const { data } = useAdultStatus();
  return data?.blur ?? true;
}

export function SearchField({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  const [text, setText] = useState(value);
  useEffect(() => setText(value), [value]);
  useEffect(() => {
    const id = window.setTimeout(() => text !== value && onChange(text.trim()), 300);
    return () => window.clearTimeout(id);
  }, [text]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <label className="relative flex min-w-0 flex-1 basis-56 items-center">
      <Search className="pointer-events-none absolute left-3 size-4 text-muted" aria-hidden />
      <input type="search" value={text} onChange={(e) => setText(e.target.value)} aria-label={label} placeholder={label}
        className="h-9 w-full rounded-[6px] border border-border bg-white/[0.05] pr-3 pl-9 text-sm text-text placeholder:text-faint focus:border-accent/70 focus-visible:outline-none" />
    </label>
  );
}


/** Where the page sits in the adult section; every step but the last is a link. */
export function Breadcrumbs({ items }: { items: { label: string; to?: string }[] }) {
  const { t } = useTranslation();
  const all = [{ label: t("adult.title"), to: "/adult" }, ...items];
  return (
    <nav aria-label={t("adult.breadcrumbs")} data-testid="adult-breadcrumbs">
      <ol className="flex min-w-0 flex-wrap items-center gap-1 text-xs text-muted">
        {all.map((it, i) => (
          <li key={i} className="flex min-w-0 items-center gap-1">
            {i > 0 && <ChevronRight className="size-3.5 shrink-0 text-faint" aria-hidden />}
            {it.to && i < all.length - 1
              ? <Link to={it.to} className="truncate hover:text-text">{it.label}</Link>
              : <span aria-current="page" className="truncate text-text/80">{it.label}</span>}
          </li>
        ))}
      </ol>
    </nav>
  );
}
