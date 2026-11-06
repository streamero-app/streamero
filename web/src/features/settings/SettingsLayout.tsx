import { Link, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useMe } from "@/features/auth/useAuth";

const tab = "block rounded-md px-3 py-2 text-sm text-muted hover:text-text [&.active]:bg-surface-2 [&.active]:text-text";

export function SettingsLayout() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  return (
    <div className="flex flex-col gap-6 md:flex-row">
      <aside className="md:w-56 md:shrink-0">
        <h1 className="mb-4 text-2xl font-semibold">{t("nav.settings")}</h1>
        <nav className="flex gap-1 overflow-x-auto md:flex-col" aria-label={t("nav.settings")}>
          <Link to="/settings/account" className={tab}>{t("settings.account")}</Link>
          <Link to="/settings/devices" className={tab}>{t("settings.devices")}</Link>
          {me?.isAdmin && (
            <>
              <p className="hidden px-3 pt-4 pb-1 text-xs tracking-wide text-muted uppercase md:block">{t("settings.adminSection")}</p>
              <Link to="/settings/server" className={tab}>{t("settings.server")}</Link>
              <Link to="/settings/users" className={tab}>{t("settings.users")}</Link>
            </>
          )}
        </nav>
      </aside>
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}

/** Formats a timestamp in the UI language. */
export function useDateTime() {
  const { i18n } = useTranslation();
  const fmt = new Intl.DateTimeFormat(i18n.resolvedLanguage === "hr" ? "hr-HR" : "en-GB", { dateStyle: "medium", timeStyle: "short" });
  return (iso?: string) => (iso ? fmt.format(new Date(iso)) : "—");
}
