import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Check, Lock, MoreHorizontal, Pencil, RefreshCw, Search, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Button, ErrorText, Spinner, SuccessText, TextField } from "@/components/ui";

type ItemDetail = components["schemas"]["ItemDetail"];

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d || d.open) return;
    try {
      d.showModal();
    } catch {
      d.setAttribute("open", ""); // environments without the modal API
    }
  }, []);
  return (
    <dialog ref={ref} onClose={onClose} onCancel={onClose} aria-label={title}
      className="m-auto w-[min(94vw,720px)] max-h-[88vh] overflow-y-auto rounded-lg border border-white/10 bg-surface p-0 text-text shadow-2xl backdrop:bg-black/70 backdrop:backdrop-blur-sm">
      <div className="sticky top-0 z-10 flex items-center gap-3 border-b border-white/10 bg-surface px-5 py-4">
        <h2 className="text-lg font-semibold">{title}</h2>
        <button type="button" onClick={onClose} aria-label={t("common.close")} className="ml-auto grid size-8 place-items-center rounded-full hover:bg-white/10">
          <X className="size-4" aria-hidden />
        </button>
      </div>
      <div className="p-5">{children}</div>
    </dialog>
  );
}

function EditDialog({ d, onClose }: { d: ItemDetail; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const it = d.item;
  const [form, setForm] = useState({
    title: it.title, originalTitle: d.originalTitle ?? "", year: it.year ? String(it.year) : "", overview: d.overview ?? "",
    tagline: d.tagline ?? "", genres: d.genres.join(", "), officialRating: d.officialRating ?? "", rating: it.rating ? it.rating.toFixed(1) : "",
    locked: true,
  });
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/items/{itemId}/metadata", {
      params: { path: { itemId: it.id } },
      body: {
        title: form.title, originalTitle: form.originalTitle || undefined, overview: form.overview || undefined, tagline: form.tagline || undefined,
        officialRating: form.officialRating || undefined, genres: form.genres.split(",").map((g) => g.trim()).filter(Boolean),
        year: form.year ? Number(form.year) : undefined, rating: form.rating ? Number(form.rating.replace(",", ".")) : undefined, locked: form.locked,
      },
    })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["item"] }); void qc.invalidateQueries({ queryKey: ["library-items"] }); onClose(); },
  });
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  return (
    <Dialog title={t("metadataEdit.title")} onClose={onClose}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
        <TextField label={t("metadataEdit.name")} value={form.title} onChange={set("title")} required maxLength={300} />
        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <TextField label={t("metadataEdit.originalTitle")} value={form.originalTitle} onChange={set("originalTitle")} maxLength={300} />
          <TextField label={t("metadataEdit.year")} value={form.year} onChange={set("year")} inputMode="numeric" pattern="[0-9]{4}" />
        </div>
        <label className="flex flex-col gap-1.5 text-sm">
          <span className="font-medium text-text/90">{t("metadataEdit.overview")}</span>
          <textarea value={form.overview} onChange={set("overview")} rows={5} maxLength={10000}
            className="rounded-[10px] border border-border bg-black/30 px-3 py-2 text-[15px] leading-relaxed text-text outline-none focus:border-accent/70" />
        </label>
        <TextField label={t("metadataEdit.tagline")} value={form.tagline} onChange={set("tagline")} maxLength={500} />
        <TextField label={t("metadataEdit.genres")} value={form.genres} onChange={set("genres")} hint={t("metadataEdit.genresHint")} />
        <div className="grid gap-4 sm:grid-cols-2">
          <TextField label={t("metadataEdit.officialRating")} value={form.officialRating} onChange={set("officialRating")} maxLength={20} placeholder="12" />
          <TextField label={t("metadataEdit.rating")} value={form.rating} onChange={set("rating")} inputMode="decimal" placeholder="7.5" />
        </div>
        <label className="flex items-start gap-3 text-sm">
          <input type="checkbox" className="mt-0.5 accent-[var(--color-accent)]" checked={form.locked} onChange={(e) => setForm({ ...form, locked: e.target.checked })} />
          <span><span className="font-medium">{t("metadataEdit.lock")}</span><span className="block text-xs text-muted">{t("metadataEdit.lockHint")}</span></span>
        </label>
        <ErrorText error={save.error} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
          <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
        </div>
      </form>
    </Dialog>
  );
}

function IdentifyDialog({ d, onClose }: { d: ItemDetail; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [query, setQuery] = useState(d.originalTitle || d.item.title);
  const [year, setYear] = useState(d.item.year ? String(d.item.year) : "");
  const [submitted, setSubmitted] = useState({ query, year });
  const results = useQuery({
    queryKey: ["identify", d.item.id, submitted],
    retry: false,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/identify", {
      params: { path: { itemId: d.item.id }, query: { query: submitted.query, year: submitted.year ? Number(submitted.year) : undefined } },
    })),
  });
  const apply = useMutation({
    mutationFn: async (tmdbId: number) => unwrap(await api.POST("/items/{itemId}/identify", { params: { path: { itemId: d.item.id } }, body: { tmdbId } })),
    onSuccess: () => { void qc.invalidateQueries(); onClose(); },
  });
  return (
    <Dialog title={t("identify.title")} onClose={onClose}>
      <form className="mb-5 flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); setSubmitted({ query, year }); }}>
        <div className="min-w-48 flex-1"><TextField label={t("identify.query")} value={query} onChange={(e) => setQuery(e.target.value)} /></div>
        <div className="w-24"><TextField label={t("metadataEdit.year")} value={year} onChange={(e) => setYear(e.target.value)} inputMode="numeric" /></div>
        <Button type="submit" variant="secondary"><Search className="size-4" aria-hidden />{t("search.title")}</Button>
      </form>
      {results.isFetching && <Spinner />}
      <ErrorText error={results.error ?? apply.error} />
      {results.data?.length === 0 && <p className="text-sm text-muted">{t("search.none")}</p>}
      <ul className="flex flex-col gap-2">
        {results.data?.map((r) => (
          <li key={r.tmdbId}>
            <button type="button" disabled={apply.isPending} onClick={() => apply.mutate(r.tmdbId)} data-testid="identify-result"
              className="flex w-full gap-3 rounded-md p-2 text-left transition-colors hover:bg-white/[0.06] disabled:opacity-50">
              <div className="aspect-[2/3] w-14 shrink-0 overflow-hidden rounded bg-surface-2">
                {r.posterUrl && <img src={r.posterUrl} alt="" referrerPolicy="no-referrer" className="size-full object-cover" loading="lazy" />}
              </div>
              <span className="min-w-0">
                <span className="block font-medium">{r.title}{r.year ? ` (${r.year})` : ""}</span>
                {r.originalTitle && <span className="block text-xs text-muted">{r.originalTitle}</span>}
                {r.overview && <span className="mt-1 line-clamp-2 block text-xs text-muted">{r.overview}</span>}
                <span className="mt-1 block text-[11px] text-faint">TMDb {r.tmdbId}</span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </Dialog>
  );
}

/** Admin actions on a title page: edit, identify, refresh. */
export function MetadataMenu({ d }: { d: ItemDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [dialog, setDialog] = useState<"edit" | "identify" | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const refresh = useMutation({
    mutationFn: async () => unwrap(await api.POST("/items/{itemId}/refresh", { params: { path: { itemId: d.item.id } } })),
    onSuccess: () => void qc.invalidateQueries(),
  });
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  const row = "flex w-full items-center gap-2.5 px-4 py-2 text-left text-sm text-white/85 hover:bg-white/[0.07] hover:text-white";
  const identifiable = d.item.kind === "movie" || d.item.kind === "series";
  return (
    <div className="relative" ref={ref}>
      <button type="button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)} aria-label={t("metadataEdit.more")} title={t("metadataEdit.more")}
        className="tv-focus grid size-11 place-items-center rounded-full border-2 border-white/50 bg-black/40 text-white backdrop-blur hover:border-white sm:size-12">
        <MoreHorizontal className="size-5" aria-hidden />
      </button>
      {open && (
        <div role="menu" className="absolute bottom-full left-0 z-50 mb-2 w-60 rounded-md border border-white/10 bg-black/95 py-2 shadow-2xl backdrop-blur-xl">
          <button type="button" role="menuitem" className={row} onClick={() => { setDialog("edit"); setOpen(false); }}>
            <Pencil className="size-4 text-white/60" aria-hidden />{t("metadataEdit.title")}
          </button>
          {identifiable && (
            <button type="button" role="menuitem" className={row} onClick={() => { setDialog("identify"); setOpen(false); }}>
              <Search className="size-4 text-white/60" aria-hidden />{t("identify.title")}
            </button>
          )}
          <button type="button" role="menuitem" className={row} disabled={refresh.isPending} onClick={() => refresh.mutate()}>
            <RefreshCw className={`size-4 text-white/60 ${refresh.isPending ? "animate-spin" : ""}`} aria-hidden />{t("metadataEdit.refresh")}
          </button>
          {d.locked && <p className="flex items-center gap-2 px-4 pt-2 text-xs text-muted"><Lock className="size-3" aria-hidden />{t("metadataEdit.locked")}</p>}
          <div className="px-4">
            {refresh.isSuccess && <SuccessText><Check className="inline size-3" aria-hidden /> {t("metadataEdit.refreshed")}</SuccessText>}
            <ErrorText error={refresh.error} />
          </div>
        </div>
      )}
      {dialog === "edit" && <EditDialog d={d} onClose={() => setDialog(null)} />}
      {dialog === "identify" && <IdentifyDialog d={d} onClose={() => setDialog(null)} />}
    </div>
  );
}
