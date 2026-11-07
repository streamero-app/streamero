import type { ComponentType } from "react";
import { createRootRoute, createRoute, createRouter, Navigate, Outlet, redirect } from "@tanstack/react-router";
import { ServerGate } from "@/features/server/ServerGate";
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
import { ItemPage, LibraryPage } from "@/features/library/LibraryPages";
import { PlayerPage } from "@/features/player/PlayerPage";
import { SearchPage } from "@/features/library/SearchPage";
import { NotFoundPage } from "@/routes/NotFoundPage";

const rootRoute = createRootRoute({
  component: () => (
    <ServerGate>
      <Outlet />
    </ServerGate>
  ),
  notFoundComponent: NotFoundPage,
});

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });
const setupRoute = createRoute({ getParentRoute: () => rootRoute, path: "/setup", component: SetupPage });
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: "/invite/$token", component: InvitePage });

// Everything below requires a signed-in user (AppShell redirects otherwise).
const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "app", component: AppShell });
const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: HomePage });
const libraryRoute = createRoute({ getParentRoute: () => appRoute, path: "/library/$libraryId", component: LibraryPage });
const itemRoute = createRoute({ getParentRoute: () => appRoute, path: "/item/$itemId", component: ItemPage });
const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/search",
  component: SearchPage,
  validateSearch: (s: Record<string, unknown>): { q?: string } => (typeof s.q === "string" && s.q ? { q: s.q } : {}),
});
const watchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/watch/$itemId",
  component: PlayerPage,
  validateSearch: (s: Record<string, unknown>): { start?: number } =>
    s.start === undefined || Number.isNaN(Number(s.start)) ? {} : { start: Number(s.start) },
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
const profilesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "profiles", component: ProfilesSettings });
const devicesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "devices", component: DevicesSettings });
const quickConnectRoute = createRoute({ getParentRoute: () => settingsRoute, path: "quickconnect", component: QuickConnectSettings });
const librariesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "libraries", component: adminOnly(LibrariesSettings) });
const metadataRoute = createRoute({ getParentRoute: () => settingsRoute, path: "metadata", component: adminOnly(MetadataSettings) });
const serverRoute = createRoute({ getParentRoute: () => settingsRoute, path: "server", component: adminOnly(ServerSettingsPage) });
const apiKeysRoute = createRoute({ getParentRoute: () => settingsRoute, path: "apikeys", component: adminOnly(ApiKeysSettings) });
const invitesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "invites", component: adminOnly(InvitesSettings) });
const usersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "users", component: adminOnly(UsersSettings) });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  inviteRoute,
  appRoute.addChildren([
    homeRoute,
    libraryRoute,
    itemRoute,
    watchRoute,
    searchRoute,
    settingsRoute.addChildren([settingsIndex, accountRoute, profilesRoute, devicesRoute, quickConnectRoute, serverRoute, librariesRoute, metadataRoute, usersRoute, invitesRoute, apiKeysRoute]),
  ]),
]);
export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
