import { Link, Navigate, Outlet, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Logo } from "@/components/Logo";
import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { Button } from "@/components/ui";
import { useSystemInfo } from "@/features/server/useSystemInfo";
import { useLogout, useMe } from "./useAuth";

const navLink = "rounded-md px-3 py-2 text-sm text-muted hover:text-text [&.active]:bg-surface-2 [&.active]:text-text";

/** Layout for signed-in pages; redirects to setup or sign-in when needed. */
export function AppShell() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: info } = useSystemInfo();
  const { data: me, isPending } = useMe();
  const logout = useLogout();

  if (info && !info.setupCompleted) return <Navigate to="/setup" />;
  if (isPending) {
    return <div role="status" className="flex h-full items-center justify-center text-muted">{t("server.checking")}</div>;
  }
  if (!me) return <Navigate to="/login" />;

  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-10 border-b border-border bg-bg/85 backdrop-blur">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-2 px-4 py-3">
          <Link to="/" className="mr-2"><Logo /></Link>
          <nav className="flex gap-1">
            <Link to="/" className={navLink} activeOptions={{ exact: true }}>{t("nav.home")}</Link>
            <Link to="/settings" className={navLink}>{t("nav.settings")}</Link>
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <LanguageSwitcher />
            <span className="hidden text-sm text-muted sm:inline" data-testid="current-user">{me.username}</span>
            <Button variant="secondary" onClick={() => logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) })}>
              {t("nav.logout")}
            </Button>
          </div>
        </div>
      </header>
      <div className="mx-auto w-full max-w-6xl flex-1 px-4 py-8">
        <Outlet />
      </div>
      <footer className="px-4 py-6 text-center text-xs text-muted">
        {info?.name} · {t("server.version", { version: info?.version })}{info?.commit ? ` (${info.commit})` : ""}
      </footer>
    </div>
  );
}
