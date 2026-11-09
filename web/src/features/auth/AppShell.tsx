import { useEffect, useRef, useState } from "react";
import { Link, Navigate, Outlet, useNavigate, useRouter, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bookmark, Clapperboard, Film, House, LogOut, Music, Search, Settings, Tv, Users, type LucideIcon } from "lucide-react";
import { Logo } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { Spinner } from "@/components/ui";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { useLibraries } from "@/features/library/LibraryPages";
import { Avatar, MAIN_COLOR, profilePicked, markProfilePicked, useProfiles } from "@/features/profiles/ProfileSwitcher";
import { installSpatialNav } from "@/features/tv/spatialNav";
import { useLogout, useMe } from "./useAuth";

export const libraryIcon: Record<string, LucideIcon> = { movies: Film, shows: Tv, mixed: Clapperboard, music: Music };

const topLink = "inline-flex h-9 items-center rounded-full px-4 text-[14px] font-medium text-white/70 transition-colors duration-200 hover:text-white [&.active]:bg-white/[0.12] [&.active]:text-white [&.active]:shadow-[inset_0_-2px_0_var(--amb-a)]";

/** Search box that opens from an icon, like streaming apps. */
function SearchBox() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { if (open) input.current?.focus(); }, [open]);
  return (
    <form role="search" className="flex items-center" onSubmit={(e) => {
      e.preventDefault();
      const q = new FormData(e.currentTarget).get("q")?.toString().trim();
      if (q) void navigate({ to: "/search", search: { q } });
    }}>
      <div className={`flex h-9 items-center overflow-hidden rounded-full transition-[width,background-color] duration-300 ease-out ${open ? "w-64 bg-white/[0.1]" : "w-9"}`}>
        <button type="button" aria-label={t("search.open")} onClick={() => setOpen(true)} className="grid size-9 shrink-0 place-items-center text-white">
          <Search className="size-5" aria-hidden />
        </button>
        <input ref={input} name="q" type="search" placeholder={t("search.placeholder")} aria-label={t("search.title")}
          tabIndex={open ? 0 : -1} onBlur={(e) => { if (!e.currentTarget.value) setOpen(false); }}
          className="h-full w-full bg-transparent pr-3 text-sm text-white outline-none placeholder:text-white/50 focus:shadow-none" />
      </div>
    </form>
  );
}

function AccountMenu() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const profiles = useProfiles();
  const logout = useLogout();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => { document.removeEventListener("mousedown", close); document.removeEventListener("keydown", esc); };
  }, [open]);
  if (!me) return null;
  const current = me.currentProfile;
  const kid = !!current?.isKid;
  const item = "flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-white/85 transition-colors hover:bg-white/[0.07] hover:text-white focus-visible:bg-white/[0.07]";
  return (
    <div className="relative" ref={ref}>
      <button type="button" aria-haspopup="menu" aria-expanded={open} aria-label={t("nav.profileMenu")} data-testid="account-menu"
        onClick={() => setOpen(!open)} className="flex items-center gap-2 rounded-md">
        <Avatar name={current?.name ?? me.username} color={current?.color ?? MAIN_COLOR} size={32} />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-50 mt-3 w-60 origin-top-right overflow-hidden rounded-md border border-white/10 bg-black/95 py-2 shadow-2xl backdrop-blur-xl [animation:pop_160ms_var(--ease-out)_both]">
          <style>{"@keyframes pop{from{opacity:0;transform:translateY(-4px)}to{opacity:1;transform:none}}"}</style>
          <p className="truncate px-4 pt-1 pb-2 text-sm font-semibold">{current?.name ?? me.username}</p>
          {(profiles.data?.length ?? 0) > 0 && (
            <Link to="/who" role="menuitem" className={item} onClick={() => { markProfilePicked(false); setOpen(false); }}>
              <Users className="size-4 text-white/60" aria-hidden />{t("nav.switchProfile")}
            </Link>
          )}
          {!kid && (
            <Link to="/settings" role="menuitem" className={item} onClick={() => setOpen(false)}>
              <Settings className="size-4 text-white/60" aria-hidden />{t("nav.settings")}
            </Link>
          )}
          <div className="px-4 py-2"><LanguageSwitcher /></div>
          <div className="my-1 border-t border-white/10" />
          <button type="button" role="menuitem" className={item}
            onClick={() => { markProfilePicked(false); logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) }); }}>
            <LogOut className="size-4 text-white/60" aria-hidden />{t("nav.logout")}
          </button>
        </div>
      )}
    </div>
  );
}

/** Layout for signed-in pages; redirects to setup, sign-in or the profile picker when needed. */
export function AppShell() {
  const { t } = useTranslation();
  const router = useRouter();
  const { data: info } = useSystemInfo();
  const { data: me, isPending } = useMe();
  const profiles = useProfiles();
  const libraries = useLibraries();
  const [scrolled, setScrolled] = useState(false);
  const path = useRouterState({ select: (s) => s.location.pathname });

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 10);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);
  useEffect(() => installSpatialNav(() => router.history.back()), [router]);

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (isPending) {
    return <div role="status" aria-label={t("server.checking")} className="flex h-full items-center justify-center"><Spinner /></div>;
  }
  if (!me) return <Navigate to="/login" />;
  // Like a streaming service: choose who is watching once per browser session.
  if ((profiles.data?.length ?? 0) > 0 && !me.currentProfile && !profilePicked()) return <Navigate to="/who" />;

  const watching = path.startsWith("/watch/");
  const libs = Array.isArray(libraries.data) ? libraries.data : [];

  return (
    <div className="min-h-full">
      {!watching && (
        <header className="pointer-events-none fixed inset-x-0 top-0 z-40 flex items-center gap-3 px-3 pt-3 sm:px-6 sm:pt-4">
          <Link to="/" aria-label={t("nav.home")} data-scrolled={scrolled}
            className="pointer-events-auto flex h-12 items-center gap-2 rounded-full bg-black/30 pr-4 pl-2 ring-1 ring-white/10 backdrop-blur-2xl transition-colors data-[scrolled=true]:bg-black/55">
            <Logo size={30} />
          </Link>
          <nav className="pointer-events-auto mx-auto hidden h-12 items-center gap-1 rounded-full bg-black/30 px-1.5 ring-1 ring-white/10 backdrop-blur-2xl md:flex" aria-label={t("nav.menu")}>
            <Link to="/" className={topLink} activeOptions={{ exact: true }}>{t("nav.home")}</Link>
            {libs.map((l) => (
              <Link key={l.id} to="/library/$libraryId" params={{ libraryId: l.id }} className={topLink}>{l.name}</Link>
            ))}
            <Link to="/collections" className={topLink}>{t("collections.title")}</Link>
            <Link to="/my-list" className={topLink}>{t("nav.myList")}</Link>
            <Link to="/playlists" className={topLink}>{t("playlists.title")}</Link>
          </nav>
          <div className="pointer-events-auto ml-auto flex h-12 items-center gap-1 rounded-full bg-black/30 px-1.5 ring-1 ring-white/10 backdrop-blur-2xl md:ml-0">
            <div className="hidden sm:block"><SearchBox /></div>
            <AccountMenu />
          </div>
        </header>
      )}

      <main className={watching ? "" : "pb-24 md:pb-0"}>
        <Outlet />
      </main>
      {!watching && (
        <footer className="px-4 pt-6 pb-24 text-center text-xs text-faint sm:px-8 md:pb-10 lg:px-14">
          {info?.name} · {t("server.version", { version: info?.version })}{info?.commit ? ` (${info.commit})` : ""}
        </footer>
      )}

      {!watching && (
        <nav aria-label={t("nav.libraries")}
          className="fixed inset-x-3 bottom-[max(0.75rem,env(safe-area-inset-bottom))] z-40 grid grid-cols-4 rounded-full bg-black/55 ring-1 ring-white/10 backdrop-blur-2xl md:hidden">
          {[
            { to: "/", icon: House, label: t("nav.home"), exact: true },
            { to: "/search", icon: Search, label: t("nav.search") },
            { to: "/my-list", icon: Bookmark, label: t("nav.myList") },
            { to: "/libraries", icon: Clapperboard, label: t("nav.libraries") },
          ].map(({ to, icon: Icon, label, exact }) => (
            <Link key={to} to={to} activeOptions={{ exact: !!exact }}
              className="flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] text-white/55 transition-colors [&.active]:text-[var(--amb-a)]">
              <Icon className="size-5" aria-hidden />{label}
            </Link>
          ))}
        </nav>
      )}
    </div>
  );
}
