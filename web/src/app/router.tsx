import { createRootRoute, createRoute, createRouter, Outlet, redirect } from "@tanstack/react-router";
import { ServerGate } from "@/features/server/ServerGate";
import { LoginPage } from "@/routes/LoginPage";
import { NotFoundPage } from "@/routes/NotFoundPage";

const rootRoute = createRootRoute({
  component: () => (
    <ServerGate>
      <Outlet />
    </ServerGate>
  ),
  notFoundComponent: NotFoundPage,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    // No session handling until P1: always go to login.
    throw redirect({ to: "/login" });
  },
});

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });

export const routeTree = rootRoute.addChildren([indexRoute, loginRoute]);
export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
