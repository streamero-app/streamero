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
import { HomePage } from "@/routes/HomePage";
import { LoginPage } from "@/routes/LoginPage";
import { SetupPage } from "@/routes/SetupPage";
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

// Everything below requires a signed-in user (AppShell redirects otherwise).
const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "app", component: AppShell });
const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: HomePage });

function adminOnly(Page: ComponentType) {
  return function AdminPage() {
    const { data: me } = useMe();
    return me?.isAdmin ? <Page /> : <Navigate to="/settings/account" />;
  };
}

const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings", component: SettingsLayout });
const settingsIndex = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/settings/account" });
  },
});
const accountRoute = createRoute({ getParentRoute: () => settingsRoute, path: "account", component: AccountSettings });
const devicesRoute = createRoute({ getParentRoute: () => settingsRoute, path: "devices", component: DevicesSettings });
const serverRoute = createRoute({ getParentRoute: () => settingsRoute, path: "server", component: adminOnly(ServerSettingsPage) });
const usersRoute = createRoute({ getParentRoute: () => settingsRoute, path: "users", component: adminOnly(UsersSettings) });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  appRoute.addChildren([
    homeRoute,
    settingsRoute.addChildren([settingsIndex, accountRoute, devicesRoute, serverRoute, usersRoute]),
  ]),
]);
export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
