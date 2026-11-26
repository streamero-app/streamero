import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronRight, EyeOff, Eye } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { ErrorText, Select, Spinner } from "@/components/ui";
import { Breadcrumbs, SearchField, useBlur } from "./AdultCommon";
import { GalleryCard, GalleryGrid } from "./AdultGalleries";
import { NamedTile, SceneCard, SceneGrid } from "./AdultPages";
import { adultImage, statusKey, useAdultStatus, type AdultGallery, type AdultScene, type AdultTag } from "./api";

function Row({ title, to, children, testId }: { title: string; to?: string; children: ReactNode; testId?: string }) {
  const { t } = useTranslation();
  return (
    <section data-testid={testId}>
      <div className="mb-3 flex items-baseline gap-3">
        <h2 className="font-head text-lg">{title}</h2>
        {to && <Link to={to} className="ml-auto inline-flex items-center gap-0.5 text-xs text-muted hover:text-text">{t("adult.seeAll")}<ChevronRight className="size-3.5" aria-hidden /></Link>}
      </div>
      {children}
    </section>
  );
}

const sceneRow = "grid grid-cols-[repeat(auto-fill,minmax(min(100%,15rem),1fr))] gap-4";
const galleryRow = "grid grid-cols-[repeat(auto-fill,minmax(min(100%,10.5rem),1fr))] gap-4";

function Scenes({ items }: { items: AdultScene[] }) {
  return <div className={sceneRow}>{items.slice(0, 8).map((s) => <SceneCard key={s.id} scene={s} />)}</div>;
}

function Galleries({ items }: { items: AdultGallery[] }) {
  return <div className={galleryRow}>{items.slice(0, 12).map((g) => <GalleryCard key={g.id} gallery={g} />)}</div>;
}

/** Start page of the section: continue watching (adult progress only), recently added and released, favourites, galleries. */
export function AdultHomePage() {
  const { t } = useTranslation();
  const home = useQuery({ queryKey: ["adult", "home"], queryFn: async () => unwrap(await api.GET("/adult/home")) });
  if (home.isPending) return <div className="py-16 text-center"><Spinner /></div>;
  if (home.error) return <ErrorText error={home.error} />;
  const h = home.data;
  const nothing = !h.continue.length && !h.recentlyAdded.length && !h.recentGalleries.length;
  return (
    <div className="flex flex-col gap-10" data-testid="adult-home">
      {nothing && <p className="py-16 text-center text-muted">{t("adult.nothing")}</p>}
      {h.continue.length > 0 && <Row title={t("adult.continue")} to="/adult/scenes?status=inProgress"><Scenes items={h.continue} /></Row>}
      {h.recentlyAdded.length > 0 && <Row title={t("adult.recentlyAdded")} to="/adult/scenes?sort=added"><Scenes items={h.recentlyAdded} /></Row>}
      {h.favoritePerformers.length > 0 && <Row title={t("adult.favoriteScenes")} to="/adult/performers"><Scenes items={h.favoritePerformers} /></Row>}
      {h.recentGalleries.length > 0 && (
        <Row title={t("adult.recentGalleries")} to="/adult/galleries?sort=added" testId="adult-home-galleries"><Galleries items={h.recentGalleries} /></Row>
      )}
      {h.recentlyReleased.length > 0 && <Row title={t("adult.recentlyReleased")} to="/adult/scenes"><Scenes items={h.recentlyReleased} /></Row>}
    </div>
  );
}

/** Saves the list of hidden tags. */
function useHiddenTags() {
  const qc = useQueryClient();
  const { data: status } = useAdultStatus();
  const hidden = status?.hiddenTags ?? [];
  const save = useMutation({
    mutationFn: async (tags: string[]) => unwrap(await api.PUT("/adult/preferences", { body: { hiddenTags: tags } })),
    onSuccess: (data) => {
      qc.setQueryData(statusKey, data);
      void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] === "adult" && q.queryKey[1] !== "status" });
    },
  });
  const isHidden = (tag: string) => hidden.some((h) => h.toLowerCase() === tag.toLowerCase());
  const toggle = (tag: string) => save.mutate(isHidden(tag) ? hidden.filter((h) => h.toLowerCase() !== tag.toLowerCase()) : [...hidden, tag]);
  return { hidden, isHidden, toggle, save };
}

function TagChip({ tag, onToggle, hidden }: { tag: AdultTag; onToggle: () => void; hidden: boolean }) {
  const { t } = useTranslation();
  return (
    <li className={`group flex items-center rounded-[6px] ring-1 ring-inset ring-border ${hidden ? "bg-transparent opacity-60" : "bg-white/[0.04]"}`} data-testid="adult-tag">
      <Link to="/adult/tag/$tag" params={{ tag: tag.name }} className="tv-focus flex min-w-0 flex-1 items-baseline gap-2 px-3 py-2 text-sm hover:text-text">
        <span className="truncate">{tag.name}</span>
        <span className="ml-auto shrink-0 text-xs text-muted tabular">{tag.sceneCount + tag.galleryCount}</span>
      </Link>
      <button type="button" onClick={onToggle} aria-label={hidden ? t("adult.showTag", { tag: tag.name }) : t("adult.hideTag", { tag: tag.name })}
        title={hidden ? t("adult.showTagShort") : t("adult.hideTagShort")} data-testid="adult-tag-toggle"
        className="tv-focus grid size-9 shrink-0 place-items-center rounded-[6px] text-text/50 hover:bg-white/10 hover:text-text">
        {hidden ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
      </button>
    </li>
  );
}

/** Tags from ThePornDB / StashDB with counts; the viewer can hide the ones they do not want to see. */
export function AdultTagsPage() {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<"count" | "name">("count");
  const [showHidden, setShowHidden] = useState(false);
  const { isHidden, toggle, hidden, save } = useHiddenTags();
  const tags = useQuery({
    queryKey: ["adult", "tags", q, sort, showHidden],
    queryFn: async () => unwrap(await api.GET("/adult/tags", { params: { query: { q: q || undefined, sort, includeHidden: showHidden || undefined, limit: 500 } } })),
  });
  return (
    <section className="flex flex-col gap-5">
      <div className="flex flex-wrap items-center gap-2">
        <SearchField value={q} onChange={setQ} label={t("adult.searchTags")} />
        <Select aria-label={t("adult.sort")} value={sort} onChange={(e) => setSort(e.target.value as "count" | "name")}>
          <option value="count">{t("adult.sortCount")}</option>
          <option value="name">{t("adult.sortName")}</option>
        </Select>
        {hidden.length > 0 && (
          <label className="inline-flex h-9 items-center gap-2 rounded-[6px] px-2 text-sm text-text/80">
            <input type="checkbox" className="accent-[var(--color-accent)]" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} />
            {t("adult.showHiddenTags", { count: hidden.length })}
          </label>
        )}
      </div>
      <p className="text-xs text-muted">{t("adult.tagsHint")}</p>
      <ErrorText error={save.error} />
      {tags.isPending ? <div className="py-16 text-center"><Spinner /></div> : <ErrorText error={tags.error} />}
      {tags.data && tags.data.items.length === 0 && <p className="py-16 text-center text-muted">{t("adult.noTags")}</p>}
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,13rem),1fr))] gap-2">
        {tags.data?.items.map((tag) => <TagChip key={tag.name} tag={tag} hidden={isHidden(tag.name)} onToggle={() => toggle(tag.name)} />)}
      </ul>
    </section>
  );
}

/** Everything with one tag: scenes, movies and galleries. */
export function AdultTagPage() {
  const { t } = useTranslation();
  const { tag } = useParams({ from: "/app/adult/tag/$tag" });
  const { isHidden, toggle, save } = useHiddenTags();
  const hidden = isHidden(tag);
  return (
    <div className="flex flex-col gap-6">
      <Breadcrumbs items={[{ label: t("adult.tags"), to: "/adult/tags" }, { label: tag }]} />
      <header className="flex flex-wrap items-center gap-3">
        <h2 className="font-display text-3xl">#{tag}</h2>
        <button type="button" onClick={() => toggle(tag)} disabled={save.isPending}
          className="inline-flex h-8 items-center gap-1.5 rounded-full bg-white/[0.07] px-3 text-sm ring-1 ring-inset ring-border hover:bg-white/[0.13]">
          {hidden ? <Eye className="size-4" aria-hidden /> : <EyeOff className="size-4" aria-hidden />}{hidden ? t("adult.showTagShort") : t("adult.hideTagShort")}
        </button>
      </header>
      <Row title={t("adult.scenesAndMovies")}><SceneGrid query={{ tag }} /></Row>
      <Row title={t("adult.galleries")}><GalleryGrid query={{ tag }} /></Row>
    </div>
  );
}

/** Search inside the adult section. */
export function AdultSearchPage() {
  const { t } = useTranslation();
  const { q = "" } = useSearch({ strict: false }) as { q?: string };
  const blur = useBlur();
  const result = useQuery({
    queryKey: ["adult", "search", q],
    queryFn: async () => unwrap(await api.GET("/adult/search", { params: { query: { q, limit: 24 } } })),
    enabled: q.trim() !== "",
  });
  const r = result.data;
  const none = r && !r.scenes.length && !r.galleries.length && !r.performers.length && !r.studios.length && !r.tags.length;
  return (
    <div className="flex flex-col gap-8" data-testid="adult-search-results">
      <Breadcrumbs items={[{ label: t("adult.searchResults", { q }) }]} />
      {result.isFetching && !r && <div className="py-16 text-center"><Spinner /></div>}
      <ErrorText error={result.error} />
      {none && <p className="py-16 text-center text-muted">{t("adult.nothing")}</p>}
      {r && r.performers.length > 0 && (
        <Row title={t("adult.performersTab")}>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,8.5rem),1fr))] gap-x-4 gap-y-6">
            {r.performers.map((p) => <NamedTile key={p.id} to="/adult/performer/$performerId" id={p.id} name={p.name} count={p.sceneCount}
              galleries={p.galleryCount} round blur={blur} favorite={p.favorite} image={p.hasImage ? adultImage("performer", p.id, "image") : undefined} />)}
          </div>
        </Row>
      )}
      {r && r.studios.length > 0 && (
        <Row title={t("adult.studios")}>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,12rem),1fr))] gap-x-4 gap-y-6">
            {r.studios.map((s) => <NamedTile key={s.id} to="/adult/studio/$studioId" id={s.id} name={s.name} count={s.sceneCount}
              galleries={s.galleryCount} blur={blur} image={s.hasImage ? adultImage("studio", s.id, "image") : undefined} />)}
          </div>
        </Row>
      )}
      {r && r.scenes.length > 0 && <Row title={t("adult.scenesAndMovies")}><div className={sceneRow}>{r.scenes.map((s) => <SceneCard key={s.id} scene={s} />)}</div></Row>}
      {r && r.galleries.length > 0 && <Row title={t("adult.galleries")}><div className={galleryRow}>{r.galleries.map((g) => <GalleryCard key={g.id} gallery={g} />)}</div></Row>}
      {r && r.tags.length > 0 && (
        <Row title={t("adult.tags")}>
          <ul className="flex flex-wrap gap-1.5">
            {r.tags.map((tag) => (
              <li key={tag.name}><Link to="/adult/tag/$tag" params={{ tag: tag.name }} className="inline-flex h-8 items-center rounded-[4px] bg-white/[0.05] px-2.5 text-sm text-text/80 hover:bg-white/[0.1]">#{tag.name}</Link></li>
            ))}
          </ul>
        </Row>
      )}
    </div>
  );
}
