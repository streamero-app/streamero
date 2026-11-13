import type { FunctionComponent } from "react";
import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Navigate, Outlet, redirect } from "@tanstack/react-router";
import { ServerGate } from "@/features/server/ServerGate";
import { DialogProvider } from "@/components/Dialog";
import { AmbientProvider } from "@/features/ambient/Ambient";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { AppShell } from "@/features/auth/AppShell";
import { canAdminister, useMe } from "@/features/auth/useAuth";
import { HomePage } from "@/routes/HomePage";
import { LoginPage } from "@/routes/LoginPage";
import { SetupPage } from "@/routes/SetupPage";
import { InvitePage } from "@/routes/InvitePage";
import { ItemPage, LibrariesPage, LibraryPage, MyListPage } from "@/features/library/LibraryPages";
import { ProfileGatePage } from "@/features/profiles/ProfileSwitcher";
import { SearchPage } from "@/features/library/SearchPage";
import { NotFoundPage } from "@/routes/NotFoundPage";
import { canManageLiveTv } from "@/features/livetv/api";

// Pages outside the everyday path load on first visit (their own chunks): settings,
// Live TV, the players, watch together, discover and collections.
const SettingsLayout = lazyRouteComponent(() => import("@/features/settings/SettingsLayout"), "SettingsLayout");
const AccountSettings = lazyRouteComponent(() => import("@/features/settings/AccountSettings"), "AccountSettings");
const DevicesSettings = lazyRouteComponent(() => import("@/features/settings/DevicesSettings"), "DevicesSettings");
const ServerSettingsPage = lazyRouteComponent(() => import("@/features/settings/ServerSettingsPage"), "ServerSettingsPage");
const UsersSettings = lazyRouteComponent(() => import("@/features/settings/UsersSettings"), "UsersSettings");
const ApiKeysSettings = lazyRouteComponent(() => import("@/features/settings/ApiKeysSettings"), "ApiKeysSettings");
const QuickConnectSettings = lazyRouteComponent(() => import("@/features/settings/QuickConnectSettings"), "QuickConnectSettings");
const InvitesSettings = lazyRouteComponent(() => import("@/features/settings/InvitesSettings"), "InvitesSettings");
const ProfilesSettings = lazyRouteComponent(() => import("@/features/settings/ProfilesSettings"), "ProfilesSettings");
const LibrariesSettings = lazyRouteComponent(() => import("@/features/settings/LibrariesSettings"), "LibrariesSettings");
const MetadataSettings = lazyRouteComponent(() => import("@/features/settings/MetadataSettings"), "MetadataSettings");
const IntegrationsSettings = lazyRouteComponent(() => import("@/features/settings/IntegrationsSettings"), "IntegrationsSettings");
const DashboardSettings = lazyRouteComponent(() => import("@/features/settings/DashboardSettings"), "DashboardSettings");
const TasksSettings = lazyRouteComponent(() => import("@/features/settings/DashboardSettings"), "TasksSettings");
const NotificationsSettings = lazyRouteComponent(() => import("@/features/settings/NotificationsSettings"), "NotificationsSettings");
const HomeSettings = lazyRouteComponent(() => import("@/features/settings/PlaybackSettings"), "HomeSettings");
const PlaybackSettings = lazyRouteComponent(() => import("@/features/settings/PlaybackSettings"), "PlaybackSettings");
const SubtitleSettings = lazyRouteComponent(() => import("@/features/settings/PlaybackSettings"), "SubtitleSettings");
const ActivitySettings = lazyRouteComponent(() => import("@/features/settings/ServerExtraSettings"), "ActivitySettings");
const BrandingSettings = lazyRouteComponent(() => import("@/features/settings/ServerExtraSettings"), "BrandingSettings");
const LogsSettings = lazyRouteComponent(() => import("@/features/settings/ServerExtraSettings"), "LogsSettings");
const TranscodingSettings = lazyRouteComponent(() => import("@/features/settings/ServerExtraSettings"), "TranscodingSettings");
const CollectionPage = lazyRouteComponent(() => import("@/features/library/CollectionsPages"), "CollectionPage");
const CollectionsPage = lazyRouteComponent(() => import("@/features/library/CollectionsPages"), "CollectionsPage");
const PlaylistPage = lazyRouteComponent(() => import("@/features/library/CollectionsPages"), "PlaylistPage");
const PlaylistsPage = lazyRouteComponent(() => import("@/features/library/CollectionsPages"), "PlaylistsPage");
const PlayerPage = lazyRouteComponent(() => import("@/features/player/PlayerPage"), "PlayerPage");
const DiscoverPage = lazyRouteComponent(() => import("@/features/discover/DiscoverPage"), "DiscoverPage");
const RequestsSettings = lazyRouteComponent(() => import("@/features/settings/RequestsSettings"), "RequestsSettings");
const ImportSettings = lazyRouteComponent(() => import("@/features/settings/ImportSettings"), "ImportSettings");
const EnhancementsSettings = lazyRouteComponent(() => import("@/features/settings/EnhancementsSettings"), "EnhancementsSettings");
const BackupSettings = lazyRouteComponent(() => import("@/features/settings/BackupSettings"), "BackupSettings");
const SsoSettings = lazyRouteComponent(() => import("@/features/settings/SsoSettings"), "SsoSettings");
const TogetherInvitePage = lazyRouteComponent(() => import("@/features/syncplay/WatchTogether"), "TogetherInvitePage");
const TogetherListPage = lazyRouteComponent(() => import("@/features/syncplay/WatchTogether"), "TogetherListPage");
const LiveTvLayout = lazyRouteComponent(() => import("@/features/livetv/LiveTvLayout"), "LiveTvLayout");
const GuidePage = lazyRouteComponent(() => import("@/features/livetv/GuidePage"), "GuidePage");
const ChannelsPage = lazyRouteComponent(() => import("@/features/livetv/ChannelsPage"), "ChannelsPage");
const RecordingsPage = lazyRouteComponent(() => import("@/features/livetv/RecordingsPage"), "RecordingsPage");
const SchedulePage = lazyRouteComponent(() => import("@/features/livetv/RecordingsPage"), "SchedulePage");
const SeriesPage = lazyRouteComponent(() => import("@/features/livetv/SeriesPage"), "SeriesPage");
const LivePlayer = lazyRouteComponent(() => import("@/features/livetv/LivePlayer"), "LivePlayer");
const LiveTvSettings = lazyRouteComponent(() => import("@/features/settings/LiveTvSettings"), "LiveTvSettings");

/** The administrator's extra CSS (Settings → Branding), on every page. */
function CustomCss() {
  const { data } = useSystemInfo();
  return data?.customCss ? <style data-testid="custom-css">{data.customCss}</style> : null;
}

const rootRoute = createRootRoute({
  component: () => (
    <AmbientProvider>
      <CustomCss />
      <DialogProvider>
        <ServerGate>
          <Outlet />
        </ServerGate>
      </DialogProvider>
    </AmbientProvider>
  ),
  notFoundComponent: NotFoundPage,
});

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });
const setupRoute = createRoute({ getParentRoute: () => rootRoute, path: "/setup", component: SetupPage });
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: "/invite/$token", component: InvitePage });
const whoRoute = createRoute({ getParentRoute: () => rootRoute, path: "/who", component: ProfileGatePage });

// Everything below requires a signed-in user (AppShell redirects otherwise).
const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "app", component: AppShell });
const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: HomePage });
const libraryRoute = createRoute({ getParentRoute: () => appRoute, path: "/library/$libraryId", component: LibraryPage });
const itemRoute = createRoute({ getParentRoute: () => appRoute, path: "/item/$itemId", component: ItemPage });
const collectionsRoute = createRoute({ getParentRoute: () => appRoute, path: "/collections", component: CollectionsPage });
const collectionRoute = createRoute({ getParentRoute: () => appRoute, path: "/collection/$collectionId", component: CollectionPage });
const playlistsRoute = createRoute({ getParentRoute: () => appRoute, path: "/playlists", component: PlaylistsPage });
const playlistRoute = createRoute({ getParentRoute: () => appRoute, path: "/playlist/$playlistId", component: PlaylistPage });
const myListRoute = createRoute({ getParentRoute: () => appRoute, path: "/my-list", component: MyListPage });
const librariesRoute2 = createRoute({ getParentRoute: () => appRoute, path: "/libraries", component: LibrariesPage });
const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/search",
  component: SearchPage,
  validateSearch: (s: Record<string, unknown>): { q?: string } => (typeof s.q === "string" && s.q ? { q: s.q } : {}),
});
const discoverRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/discover",
  component: DiscoverPage,
  validateSearch: (s: Record<string, unknown>): { q?: string } => (typeof s.q === "string" && s.q ? { q: s.q } : {}),
});
const togetherRoute = createRoute({ getParentRoute: () => appRoute, path: "/together", component: TogetherListPage });
const togetherInviteRoute = createRoute({ getParentRoute: () => appRoute, path: "/together/$groupId", component: TogetherInvitePage });
// Live TV (D-041): the player sits beside the section so it gets the whole screen.
const liveTvRoute = createRoute({ getParentRoute: () => appRoute, path: "/live", component: LiveTvLayout });
const liveGuideRoute = createRoute({ getParentRoute: () => liveTvRoute, path: "/", component: GuidePage });
const liveChannelsRoute = createRoute({ getParentRoute: () => liveTvRoute, path: "channels", component: ChannelsPage });
const liveRecordingsRoute = createRoute({ getParentRoute: () => liveTvRoute, path: "recordings", component: RecordingsPage });
const liveScheduleRoute = createRoute({ getParentRoute: () => liveTvRoute, path: "schedule", component: SchedulePage });
const liveSeriesRoute = createRoute({ getParentRoute: () => liveTvRoute, path: "series", component: SeriesPage });
const liveWatchRoute = createRoute({ getParentRoute: () => appRoute, path: "/live/watch/$channelId", component: LivePlayer });
const watchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/watch/$itemId",
  component: PlayerPage,
  validateSearch: (s: Record<string, unknown>): { start?: number; playlist?: string; file?: string; audio?: number; sub?: number; together?: number } => {
    const num = (v: unknown) => (v === undefined || v === "" || Number.isNaN(Number(v)) ? undefined : Number(v));
    const out: { start?: number; playlist?: string; file?: string; audio?: number; sub?: number; together?: number } = {};
    if (num(s.start) !== undefined) out.start = num(s.start);
    if (typeof s.playlist === "string" && s.playlist) out.playlist = s.playlist;
    if (typeof s.file === "string" && /^[0-9a-f-]{36}$/i.test(s.file)) out.file = s.file;
    if (num(s.audio) !== undefined) out.audio = num(s.audio);
    if (num(s.sub) !== undefined) out.sub = num(s.sub);
    if (num(s.together) === 1) out.together = 1;
    return out;
  },
});

type Lazy = FunctionComponent & { preload?: () => Promise<void> | undefined };

/**
 * A gate in front of a lazily loaded page. It carries the page's preload so the
 * router fetches the chunk while matching, and the page never suspends mid-render.
 */
function gated(Page: Lazy, allowed: (me: ReturnType<typeof useMe>["data"]) => boolean, to: "/" | "/settings/account"): Lazy {
  const Gate: Lazy = () => {
    const { data: me } = useMe();
    return allowed(me) ? <Page /> : <Navigate to={to} />;
  };
  Gate.preload = Page.preload;
  return Gate;
}

/** Administrator pages (not from a kid profile, where the server refuses admin calls). */
function adminOnly(Page: Lazy) {
  return gated(Page, canAdminister, "/settings/account");
}

/** Live TV settings: administrators and users allowed to manage Live TV. */
const LiveTvSettingsPage = gated(LiveTvSettings, canManageLiveTv, "/settings/account");

const AdultsOnlySettings = gated(SettingsLayout, (me) => !me?.currentProfile?.isKid, "/");
const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings", component: AdultsOnlySettings });
const settingsIndex = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/settings/account" });
  },
});
const accountRoute = createRoute({ getParentRoute: () => settingsRoute, path: "account", component: AccountSettings });
const dashboardRoute = createRoute({ getParentRoute: () => settingsRoute, path: "dashboard", component: adminOnly(DashboardSettings) });
const tasksRoute = createRoute({ getParentRoute: () => settingsRoute, path: "tasks", component: adminOnly(TasksSettings) });
const backupsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "backups", component: adminOnly(BackupSettings) });
const notificationsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "notifications", component: adminOnly(NotificationsSettings) });
const playbackRoute = createRoute({ getParentRoute: () => settingsRoute, path: "playback", component: PlaybackSettings });
const profilesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "profiles", component: ProfilesSettings });
const devicesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "devices", component: DevicesSettings });
const quickConnectRoute = createRoute({ getParentRoute: () => settingsRoute, path: "quickconnect", component: QuickConnectSettings });
const librariesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "libraries", component: adminOnly(LibrariesSettings) });
const metadataRoute = createRoute({ getParentRoute: () => settingsRoute, path: "metadata", component: adminOnly(MetadataSettings) });
const integrationsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "integrations", component: adminOnly(IntegrationsSettings) });
const serverRoute = createRoute({ getParentRoute: () => settingsRoute, path: "server", component: adminOnly(ServerSettingsPage) });
const apiKeysRoute = createRoute({ getParentRoute: () => settingsRoute, path: "apikeys", component: adminOnly(ApiKeysSettings) });
const invitesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "invites", component: adminOnly(InvitesSettings) });
const usersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "users", component: adminOnly(UsersSettings) });
const subtitleLookRoute = createRoute({ getParentRoute: () => settingsRoute, path: "subtitles", component: SubtitleSettings });
const homePrefsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "home", component: HomeSettings });
const transcodingRoute = createRoute({ getParentRoute: () => settingsRoute, path: "transcoding", component: adminOnly(TranscodingSettings) });
const logsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "logs", component: adminOnly(LogsSettings) });
const activityRoute = createRoute({ getParentRoute: () => settingsRoute, path: "activity", component: adminOnly(ActivitySettings) });
const requestsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "requests", component: adminOnly(RequestsSettings) });
const enhancementsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "enhancements", component: adminOnly(EnhancementsSettings) });
const importRoute = createRoute({ getParentRoute: () => settingsRoute, path: "import", component: adminOnly(ImportSettings) });
const ssoRoute = createRoute({ getParentRoute: () => settingsRoute, path: "sso", component: adminOnly(SsoSettings) });
const liveTvSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: "livetv", component: LiveTvSettingsPage });
const brandingRoute = createRoute({ getParentRoute: () => settingsRoute, path: "branding", component: adminOnly(BrandingSettings) });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  inviteRoute,
  whoRoute,
  appRoute.addChildren([
    homeRoute,
    libraryRoute,
    itemRoute,
    myListRoute,
    collectionsRoute,
    collectionRoute,
    playlistsRoute,
    playlistRoute,
    librariesRoute2,
    watchRoute,
    liveTvRoute.addChildren([liveGuideRoute, liveChannelsRoute, liveRecordingsRoute, liveScheduleRoute, liveSeriesRoute]),
    liveWatchRoute,
    togetherRoute,
    togetherInviteRoute,
    searchRoute,
    discoverRoute,
    settingsRoute.addChildren([settingsIndex, playbackRoute, dashboardRoute, tasksRoute, backupsRoute, notificationsRoute, accountRoute, profilesRoute, devicesRoute, quickConnectRoute, serverRoute, librariesRoute, metadataRoute, integrationsRoute, usersRoute, invitesRoute, apiKeysRoute,
      subtitleLookRoute, homePrefsRoute, transcodingRoute, logsRoute, activityRoute, brandingRoute, requestsRoute, enhancementsRoute, importRoute, ssoRoute, liveTvSettingsRoute]),
  ]),
]);
export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
