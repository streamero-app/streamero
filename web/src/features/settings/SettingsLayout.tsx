import { Link, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Bell, Captions, Clapperboard, Cable, Cpu, FolderOpen, Gauge, History, House, KeyRound, ListChecks, Mail, MonitorSmartphone, Palette, ScrollText, Server, Sparkles, UserRound, Users, UsersRound, Zap, type LucideIcon } from "lucide-react";
import { useMe } from "@/features/auth/useAuth";
import { Page } from "@/components/Page";

const tab =
  "group flex h-9 shrink-0 items-center gap-2.5 rounded-[6px] px-3 text-sm whitespace-nowrap text-muted transition-colors duration-150 hover:bg-white/[0.05] hover:text-text [&.active]:bg-white/[0.08] [&.active]:text-text";

function Tab({ to, icon: Icon, label }: { to: string; icon: LucideIcon; label: string }) {
  return (
    <Link to={to} className={tab}>
      <Icon className="size-4 text-faint transition-colors group-hover:text-text group-[.active]:text-text" aria-hidden />
      {label}
    </Link>
  );
}

export function SettingsLayout() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  return (
    <Page className="flex flex-col gap-8">
      <h1 className="font-display text-4xl sm:text-5xl">{t("nav.settings")}</h1>
      <div className="flex flex-col gap-8 md:flex-row">
      <aside className="md:w-56 md:shrink-0">
        <nav className="shelf-track -mx-4 flex gap-1 overflow-x-auto px-4 md:mx-0 md:flex-col md:px-0" aria-label={t("nav.settings")}>
          <Tab to="/settings/account" icon={UserRound} label={t("settings.account")} />
          <Tab to="/settings/playback" icon={Clapperboard} label={t("settings.playback")} />
          <Tab to="/settings/subtitles" icon={Captions} label={t("settings.subtitleLook")} />
          <Tab to="/settings/home" icon={House} label={t("settings.home")} />
          <Tab to="/settings/profiles" icon={UsersRound} label={t("settings.profiles")} />
          <Tab to="/settings/devices" icon={MonitorSmartphone} label={t("settings.devices")} />
          <Tab to="/settings/quickconnect" icon={Zap} label={t("settings.quickConnect")} />
          {me?.isAdmin && (
            <>
              <p className="hidden px-3 pt-5 pb-1.5 text-[11px] font-medium tracking-[0.08em] text-faint uppercase md:block">{t("settings.adminSection")}</p>
              <Tab to="/settings/dashboard" icon={Gauge} label={t("settings.dashboard")} />
              <Tab to="/settings/server" icon={Server} label={t("settings.server")} />
              <Tab to="/settings/libraries" icon={FolderOpen} label={t("settings.libraries")} />
              <Tab to="/settings/metadata" icon={Sparkles} label={t("settings.metadata")} />
              <Tab to="/settings/transcoding" icon={Cpu} label={t("settings.transcoding")} />
              <Tab to="/settings/users" icon={Users} label={t("settings.users")} />
              <Tab to="/settings/invites" icon={Mail} label={t("settings.invites")} />
              <Tab to="/settings/integrations" icon={Cable} label={t("settings.integrations")} />
              <Tab to="/settings/notifications" icon={Bell} label={t("settings.notifications")} />
              <Tab to="/settings/tasks" icon={ListChecks} label={t("settings.tasks")} />
              <Tab to="/settings/activity" icon={History} label={t("settings.activityLog")} />
              <Tab to="/settings/logs" icon={ScrollText} label={t("settings.logs")} />
              <Tab to="/settings/branding" icon={Palette} label={t("settings.branding")} />
              <Tab to="/settings/apikeys" icon={KeyRound} label={t("settings.apiKeys")} />
            </>
          )}
        </nav>
      </aside>
      <div className="enter flex min-w-0 max-w-3xl flex-1 flex-col gap-6">
        <Outlet />
      </div>
      </div>
    </Page>
  );
}

/** Formats a timestamp in the UI language. */
export function useDateTime() {
  const { i18n } = useTranslation();
  const fmt = new Intl.DateTimeFormat(i18n.resolvedLanguage === "hr" ? "hr-HR" : "en-GB", { dateStyle: "medium", timeStyle: "short" });
  return (iso?: string) => (iso ? fmt.format(new Date(iso)) : "—");
}
