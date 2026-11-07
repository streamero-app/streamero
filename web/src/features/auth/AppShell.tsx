import { useEffect, useState } from "react";
import { Link, Navigate, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Clapperboard, Film, House, LogOut, Menu, Music, Search, Settings, Tv, X, type LucideIcon } from "lucide-react";
import { Logo } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { Spinner } from "@/components/ui";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { useLibraries } from "@/features/library/LibraryPages";
import { useLogout, useMe } from "./useAuth";
import { ProfileSwitcher } from "@/features/profiles/ProfileSwitcher";

export const libraryIcon: Record<string, LucideIcon> = { movies: Film, shows: Tv, mixed: Clapperboard, music: Music };

const navItem =
  "group flex h-10 items-center gap-3 rounded-[10px] px-3 text-[14px] text-muted transition-colors duration-150 hover:bg-white/[0.05] hover:text-text [&.active]:bg-white/[0.08] [&.active]:text-text";
const navIcon = "size-[18px] shrink-0 text-faint transition-colors group-hover:text-text group-[.active]:text-accent";

function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const libraries = useLibraries();
  const logout = useLogout();
  const kid = !!me?.currentProfile?.isKid;
  return (
    <div className="flex h-full flex-col gap-6 px-3 py-5">
      <Link to="/" className="px-2" onClick={onNavigate} aria-label={t("nav.home")}><Logo /></Link>
      <nav className="flex flex-col gap-0.5" aria-label={t("nav.menu")}>
        <Link to="/" className={navItem} activeOptions={{ exact: true }} onClick={onNavigate}><House className={navIcon} />{t("nav.home")}</Link>
        <Link to="/search" className={navItem} onClick={onNavigate}><Search className={navIcon} />{t("nav.search")}</Link>
      </nav>
      {(libraries.data?.length ?? 0) > 0 && (
        <nav className="flex flex-col gap-0.5" aria-label={t("nav.libraries")}>
          <p className="px-3 pb-1.5 text-[11px] font-medium tracking-[0.08em] text-faint uppercase">{t("nav.libraries")}</p>
          {libraries.data?.map((l) => {
            const Icon = libraryIcon[l.kind] ?? Clapperboard;
            return (
              <Link key={l.id} to="/library/$libraryId" params={{ libraryId: l.id }} className={navItem} onClick={onNavigate}>
                <Icon className={navIcon} />
                <span className="truncate">{l.name}</span>
              </Link>
            );
          })}
        </nav>
      )}
      <div className="mt-auto flex flex-col gap-0.5">
        {!kid && <Link to="/settings" className={navItem} onClick={onNavigate}><Settings className={navIcon} />{t("nav.settings")}</Link>}
        <button type="button" className={navItem} onClick={() => logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) })}>
          <LogOut className={navIcon} />{t("nav.logout")}
        </button>
      </div>
    </div>
  );
}

/** Layout for signed-in pages; redirects to setup or sign-in when needed. */
export function AppShell() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: info } = useSystemInfo();
  const { data: me, isPending } = useMe();
  const [drawer, setDrawer] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const path = useRouterState({ select: (s) => s.location.pathname });

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);
  useEffect(() => setDrawer(false), [path]);

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (isPending) {
    return <div role="status" aria-label={t("server.checking")} className="flex h-full items-center justify-center"><Spinner /></div>;
  }
  if (!me) return <Navigate to="/login" />;

  return (
    <div className="min-h-full lg:pl-60">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 border-r border-border bg-[#0d0b09] lg:block">
        <SidebarNav />
      </aside>

      {drawer && (
        <div className="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true" aria-label={t("nav.menu")}>
          <button type="button" aria-label={t("common.close", { defaultValue: "Close" })} className="absolute inset-0 bg-black/60 backdrop-blur-sm enter" onClick={() => setDrawer(false)} />
          <div className="absolute inset-y-0 left-0 w-72 max-w-[85vw] border-r border-border bg-[#0d0b09] shadow-2xl [animation:drawer_280ms_var(--ease-out)_both]">
            <button type="button" aria-label={t("common.close", { defaultValue: "Close" })} onClick={() => setDrawer(false)}
              className="absolute top-5 right-3 grid size-9 place-items-center rounded-full text-muted hover:bg-white/[0.06] hover:text-text">
              <X className="size-5" />
            </button>
            <SidebarNav onNavigate={() => setDrawer(false)} />
          </div>
          <style>{"@keyframes drawer{from{transform:translateX(-100%)}to{transform:none}}"}</style>
        </div>
      )}

      <header data-scrolled={scrolled}
        className="sticky top-0 z-20 flex h-16 items-center gap-3 px-4 transition-[background-color,border-color,backdrop-filter] duration-300 data-[scrolled=true]:border-b data-[scrolled=true]:border-border data-[scrolled=true]:bg-bg/80 data-[scrolled=true]:backdrop-blur-xl sm:px-6 lg:px-10">
        <button type="button" onClick={() => setDrawer(true)} aria-label={t("nav.menu")}
          className="-ml-1 grid size-10 place-items-center rounded-full text-text hover:bg-white/[0.06] lg:hidden">
          <Menu className="size-5" />
        </button>
        <Link to="/" className="lg:hidden" aria-label={t("nav.home")}><Logo size={26} /></Link>
        {path === "/search" ? <div className="ml-auto lg:ml-0" /> : <form className="ml-auto lg:ml-0" role="search" onSubmit={(e) => {
          e.preventDefault();
          const q = new FormData(e.currentTarget).get("q")?.toString().trim();
          if (q) void navigate({ to: "/search", search: { q } });
        }}>
          <label className="group relative hidden items-center sm:flex">
            <Search className="pointer-events-none absolute left-3 z-10 size-4 text-faint transition-colors group-focus-within:text-accent" />
            <input name="q" type="search" placeholder={t("search.placeholder")} aria-label={t("search.title")}
              className="h-10 w-56 rounded-full border border-border bg-black/35 pr-4 pl-9 text-sm text-text backdrop-blur-md transition-[width,border-color,background-color] duration-300 ease-out placeholder:text-faint hover:border-border-strong focus:w-80 focus:border-accent/60 focus:bg-black/55" />
          </label>
          <Link to="/search" className="grid size-10 place-items-center rounded-full text-text hover:bg-white/[0.06] sm:hidden">
            <Search className="size-5" aria-hidden /><span className="sr-only">{t("search.title")}</span>
          </Link>
        </form>}
        <div className="flex items-center gap-2 lg:ml-auto">
          <div className="hidden md:block"><LanguageSwitcher /></div>
          <ProfileSwitcher />
        </div>
      </header>

      <main className="-mt-16 min-h-[calc(100vh-4rem)]">
        <Outlet />
      </main>
      <footer className="px-4 py-8 text-center text-xs text-faint sm:px-6 lg:px-10">
        {info?.name} · {t("server.version", { version: info?.version })}{info?.commit ? ` (${info.commit})` : ""}
      </footer>
    </div>
  );
}

