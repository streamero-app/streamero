import { Link, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CalendarClock, CircleDot, LayoutGrid, ListVideo, Repeat, type LucideIcon } from "lucide-react";
import { Page } from "@/components/Page";
import { useMe } from "@/features/auth/useAuth";
import { canWatchLiveTv } from "./api";

const tab =
  "group inline-flex h-9 shrink-0 items-center gap-2 rounded-[6px] px-3 text-sm font-medium whitespace-nowrap text-text/60 transition-colors duration-150 hover:bg-white/[0.06] hover:text-text [&.active]:bg-white/[0.1] [&.active]:text-text";

function Tab({ to, icon: Icon, label, exact }: { to: string; icon: LucideIcon; label: string; exact?: boolean }) {
  return (
    <Link to={to} className={tab} activeOptions={{ exact: !!exact }}>
      <Icon className="size-4 text-text/40 transition-colors group-[.active]:text-[var(--amb-a)]" aria-hidden />
      {label}
    </Link>
  );
}

/** Live TV: guide, channels, recordings, schedule and series rules under one header. */
export function LiveTvLayout() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  if (!canWatchLiveTv(me)) {
    return (
      <Page>
        <h1 className="font-display text-4xl">{t("livetv.title")}</h1>
        <p className="mt-4 max-w-prose text-muted" data-testid="livetv-denied">{t("livetv.noPermission")}</p>
      </Page>
    );
  }
  return (
    <Page className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end gap-x-8 gap-y-4">
        <h1 className="font-display text-4xl sm:text-5xl">{t("livetv.title")}</h1>
        <nav aria-label={t("livetv.sections")} className="shelf-track -mx-1 flex gap-1 overflow-x-auto px-1">
          <Tab to="/live" icon={LayoutGrid} label={t("livetv.guide")} exact />
          <Tab to="/live/channels" icon={ListVideo} label={t("livetv.channels")} />
          <Tab to="/live/recordings" icon={CircleDot} label={t("livetv.recordings")} />
          <Tab to="/live/schedule" icon={CalendarClock} label={t("livetv.schedule")} />
          <Tab to="/live/series" icon={Repeat} label={t("livetv.series")} />
        </nav>
      </div>
      <Outlet />
    </Page>
  );
}
