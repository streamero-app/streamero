import type { ComponentType } from "react";
import { createRootRoute, createRoute, createRouter, Navigate, Outlet, redirect } from "@tanstack/react-router";
import { ServerGate } from "@/features/server/ServerGate";
import { AmbientProvider } from "@/features/ambient/Ambient";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { AppShell } from "@/features/auth/AppShell";
import { useMe } from "@/features/auth/useAuth";
import { SettingsLayout } from "@/features/settings/SettingsLayout";
import { AccountSettings } from "@/features/settings/AccountSettings";
import { DevicesSettings } from "@/features/settings/DevicesSettings";
import { ServerSettingsPage } from "@/features/settings/ServerSettingsPage";
import { UsersSettings } from "@/features/settings/UsersSettings";
import { ApiKeysSettings } from "@/features/settings/ApiKeysSettings";
import { QuickConnectSettings } from "@/features/settings/QuickConnectSettings";
import { HomePage } from "@/routes/HomePage";
import { LoginPage } from "@/routes/LoginPage";
import { SetupPage } from "@/routes/SetupPage";
import { InvitePage } from "@/routes/InvitePage";
import { InvitesSettings } from "@/features/settings/InvitesSettings";
import { ProfilesSettings } from "@/features/settings/ProfilesSettings";
import { LibrariesSettings } from "@/features/settings/LibrariesSettings";
import { MetadataSettings } from "@/features/settings/MetadataSettings";
import { IntegrationsSettings } from "@/features/settings/IntegrationsSettings";
import { DashboardSettings, TasksSettings } from "@/features/settings/DashboardSettings";
import { NotificationsSettings } from "@/features/settings/NotificationsSettings";
import { HomeSettings, PlaybackSettings, SubtitleSettings } from "@/features/settings/PlaybackSettings";
import { ActivitySettings, BrandingSettings, LogsSettings, TranscodingSettings } from "@/features/settings/ServerExtraSettings";
import { ItemPage, LibrariesPage, LibraryPage, MyListPage } from "@/features/library/LibraryPages";
import { ProfileGatePage } from "@/features/profiles/ProfileSwitcher";
import { CollectionPage, CollectionsPage, PlaylistPage, PlaylistsPage } from "@/features/library/CollectionsPages";
import { PlayerPage } from "@/features/player/PlayerPage";
import { SearchPage } from "@/features/library/SearchPage";
import { DiscoverPage } from "@/features/discover/DiscoverPage";
import { RequestsSettings } from "@/features/settings/RequestsSettings";
import { ImportSettings } from "@/features/settings/ImportSettings";
import { EnhancementsSettings } from "@/features/settings/EnhancementsSettings";
import { BackupSettings } from "@/features/settings/BackupSettings";
import { SsoSettings } from "@/features/settings/SsoSettings";
import { NotFoundPage } from "@/routes/NotFoundPage";
import { TogetherInvitePage, TogetherListPage } from "@/features/syncplay/WatchTogether";

/** The administrator's extra CSS (Settings → Branding), on every page. */
function CustomCss() {
  const { data } = useSystemInfo();
  return data?.customCss ? <style data-testid="custom-css">{data.customCss}</style> : null;
}

const rootRoute = createRootRoute({
  component: () => (
    <AmbientProvider>
      <CustomCss />
      <ServerGate>
        <Outlet />
      </ServerGate>
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

function adminOnly(Page: ComponentType) {
  return function AdminPage() {
    const { data: me } = useMe();
    return me?.isAdmin ? <Page /> : <Navigate to="/settings/account" />;
  };
}

function AdultsOnlySettings() {
  const { data: me } = useMe();
  return me?.currentProfile?.isKid ? <Navigate to="/" /> : <SettingsLayout />;
}
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
    togetherRoute,
    togetherInviteRoute,
    searchRoute,
    discoverRoute,
    settingsRoute.addChildren([settingsIndex, playbackRoute, dashboardRoute, tasksRoute, backupsRoute, notificationsRoute, accountRoute, profilesRoute, devicesRoute, quickConnectRoute, serverRoute, librariesRoute, metadataRoute, integrationsRoute, usersRoute, invitesRoute, apiKeysRoute,
      subtitleLookRoute, homePrefsRoute, transcodingRoute, logsRoute, activityRoute, brandingRoute, requestsRoute, enhancementsRoute, importRoute, ssoRoute]),
  ]),
]);
export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
