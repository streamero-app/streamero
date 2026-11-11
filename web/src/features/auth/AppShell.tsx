import { useEffect, useRef, useState } from "react";
import { Link, Navigate, Outlet, useNavigate, useRouter, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bookmark, Clapperboard, Compass, Film, House, LogOut, Music, Radio, Search, Settings, Tv, Users, type LucideIcon } from "lucide-react";
import { Logo } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { Spinner } from "@/components/ui";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { useLibraries } from "@/features/library/LibraryPages";
import { Avatar, MAIN_COLOR, avatarUrl, profilePicked, markProfilePicked, useProfiles } from "@/features/profiles/ProfileSwitcher";
import { installSpatialNav } from "@/features/tv/spatialNav";
import { useLogout, useMe } from "./useAuth";
import { setLanguage } from "@/i18n";
import { MusicProvider } from "@/features/music/MusicPlayer";
import { NowPlayingBar } from "@/features/music/NowPlayingBar";
import { SyncPlayProvider } from "@/features/syncplay/SyncPlay";
import { canWatchLiveTv } from "@/features/livetv/api";

export const libraryIcon: Record<string, LucideIcon> = { movies: Film, shows: Tv, mixed: Clapperboard, music: Music };

const topLink = "relative inline-flex h-10 items-center px-1 text-[15px] font-medium text-text/65 transition-colors duration-200 hover:text-text after:absolute after:inset-x-1 after:bottom-1 after:h-[2px] after:origin-left after:scale-x-0 after:bg-[var(--amb-a)] after:transition-transform after:duration-300 [&.active]:text-text [&.active]:after:scale-x-100";

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
      <div className={`flex h-10 items-center overflow-hidden rounded-[6px] transition-[width,background-color] duration-300 ease-out ${open ? "w-64 bg-white/[0.1] ring-1 ring-inset ring-border" : "w-10"}`}>
        <button type="button" aria-label={t("search.open")} onClick={() => setOpen(true)} className="grid size-10 shrink-0 place-items-center text-text/85 hover:text-text">
          <Search className="size-5" aria-hidden />
        </button>
        <input ref={input} name="q" type="search" placeholder={t("search.placeholder")} aria-label={t("search.title")}
          tabIndex={open ? 0 : -1} onBlur={(e) => { if (!e.currentTarget.value) setOpen(false); }}
          className="h-full w-full bg-transparent pr-3 text-sm text-text outline-none placeholder:text-text/50 focus:shadow-none" />
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
  const item = "flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-text/85 transition-colors hover:bg-white/[0.07] hover:text-text focus-visible:bg-white/[0.07]";
  return (
    <div className="relative" ref={ref}>
      <button type="button" aria-haspopup="menu" aria-expanded={open} aria-label={t("nav.profileMenu")} data-testid="account-menu"
        onClick={() => setOpen(!open)} className="flex items-center gap-2 rounded-md">
        <Avatar name={current?.name ?? me.displayName ?? me.username} color={current?.color ?? MAIN_COLOR} size={32}
          image={current ? (current.hasAvatar ? avatarUrl(current.id) : null) : me.hasAvatar ? avatarUrl(me.id) : null} />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-50 mt-3 w-64 origin-top-right overflow-hidden rounded-[8px] border border-border bg-surface/95 py-2 shadow-2xl backdrop-blur-xl [animation:pop_160ms_var(--ease-out)_both]">
          <style>{"@keyframes pop{from{opacity:0;transform:translateY(-4px)}to{opacity:1;transform:none}}"}</style>
          <p className="truncate px-4 pt-1 pb-2 text-sm font-semibold">{current?.name ?? me.displayName ?? me.username}</p>
          {(profiles.data?.length ?? 0) > 0 && (
            <Link to="/who" role="menuitem" className={item} onClick={() => { markProfilePicked(false); setOpen(false); }}>
              <Users className="size-4 text-text/55" aria-hidden />{t("nav.switchProfile")}
            </Link>
          )}
          {canWatchLiveTv(me) && (
            <Link to="/livetv" role="menuitem" className={item} onClick={() => setOpen(false)} data-testid="livetv-menu">
              <Radio className="size-4 text-text/55" aria-hidden />{t("livetv.title")}
            </Link>
          )}
          {(me.isAdmin || me.permissions.syncPlay) && (
            <Link to="/together" role="menuitem" className={item} onClick={() => setOpen(false)} data-testid="together-menu">
              <Users className="size-4 text-text/55" aria-hidden />{t("together.groups")}
            </Link>
          )}
          {!kid && (
            <Link to="/settings" role="menuitem" className={item} onClick={() => setOpen(false)}>
              <Settings className="size-4 text-text/55" aria-hidden />{t("nav.settings")}
            </Link>
          )}
          <div className="px-4 py-2"><LanguageSwitcher /></div>
          <div className="my-1 border-t border-border" />
          <button type="button" role="menuitem" className={item}
            onClick={() => { markProfilePicked(false); logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) }); }}>
            <LogOut className="size-4 text-text/55" aria-hidden />{t("nav.logout")}
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
  // The account's language follows the user to every device.
  const { i18n } = useTranslation();
  useEffect(() => {
    if (me?.language && me.language !== i18n.resolvedLanguage) setLanguage(me.language);
  }, [me?.language]); // eslint-disable-line react-hooks/exhaustive-deps

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (isPending) {
    return <div role="status" aria-label={t("server.checking")} className="flex h-full items-center justify-center"><Spinner /></div>;
  }
  if (!me) return <Navigate to="/login" />;
  // Like a streaming service: choose who is watching once per browser session.
  if ((profiles.data?.length ?? 0) > 0 && !me.currentProfile && !profilePicked()) return <Navigate to="/who" />;

  const watching = path.startsWith("/watch/") || path.startsWith("/livetv/watch/");
  const libs = Array.isArray(libraries.data) ? libraries.data : [];

  return (
    <MusicProvider>
    <SyncPlayProvider>
    <div className="min-h-full">
      {!watching && (
        <header data-scrolled={scrolled}
          className="fixed inset-x-0 top-0 z-40 flex h-16 items-center gap-8 bg-gradient-to-b from-black/75 to-transparent px-5 transition-[background-color] duration-300 data-[scrolled=true]:bg-bg/92 data-[scrolled=true]:backdrop-blur-xl sm:px-10 md:h-[4.5rem] lg:px-16">
          <Link to="/" aria-label={t("nav.home")} className="tv-focus shrink-0 rounded-[4px]">
            <Logo size={26} />
          </Link>
          <nav className="hidden h-full items-center gap-6 md:flex" aria-label={t("nav.menu")}>
            <Link to="/" className={topLink} activeOptions={{ exact: true }}>{t("nav.home")}</Link>
            {libs.map((l) => (
              <Link key={l.id} to="/library/$libraryId" params={{ libraryId: l.id }} className={topLink}>{l.name}</Link>
            ))}
            <Link to="/collections" className={topLink}>{t("collections.title")}</Link>
            <Link to="/my-list" className={topLink}>{t("nav.myList")}</Link>
            <Link to="/playlists" className={topLink}>{t("playlists.title")}</Link>
            {canWatchLiveTv(me) && <Link to="/livetv" className={topLink} data-testid="livetv-link">{t("livetv.title")}</Link>}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <div className="hidden sm:block"><SearchBox /></div>
            <Link to="/discover" aria-label={t("discover.title")} title={t("discover.title")} data-testid="discover-link"
              className="tv-focus grid size-10 place-items-center rounded-[6px] text-text/85 transition-colors hover:bg-white/10 hover:text-text [&.active]:text-[var(--amb-a)]">
              <Compass className="size-5" aria-hidden />
            </Link>
            <AccountMenu />
          </div>
        </header>
      )}

      <main className={watching ? "" : "pb-24 md:pb-20"}>
        <Outlet />
      </main>

      {!watching && (
        <nav aria-label={t("nav.libraries")}
          className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-border bg-bg/92 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl md:hidden">
          {[
            { to: "/", icon: House, label: t("nav.home"), exact: true },
            { to: "/search", icon: Search, label: t("nav.search") },
            { to: "/discover", icon: Compass, label: t("discover.short") },
            { to: "/my-list", icon: Bookmark, label: t("nav.myList") },
            { to: "/libraries", icon: Clapperboard, label: t("nav.libraries") },
          ].map(({ to, icon: Icon, label, exact }) => (
            <Link key={to} to={to} activeOptions={{ exact: !!exact }}
              className="relative flex h-16 flex-col items-center justify-center gap-1 text-[11px] font-medium text-text/50 transition-colors before:absolute before:inset-x-6 before:top-0 before:h-[2px] before:scale-x-0 before:bg-[var(--amb-a)] before:transition-transform [&.active]:text-text [&.active]:before:scale-x-100">
              <Icon className="size-[22px]" aria-hidden />{label}
            </Link>
          ))}
        </nav>
      )}
      {!watching && <NowPlayingBar />}
    </div>
    </SyncPlayProvider>
    </MusicProvider>
  );
}
