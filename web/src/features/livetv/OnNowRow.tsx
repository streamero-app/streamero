import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Row, rowCard } from "@/features/library/cards";
import { useMe } from "@/features/auth/useAuth";
import { canWatchLiveTv, liveKeys, type Channel } from "./api";
import { ChannelBadge, useNow } from "./GuidePage";
import { progress, timeOf } from "./time";

function OnNowCard({ channel, now }: { channel: Channel; now: number }) {
  const { t, i18n } = useTranslation();
  const p = channel.now!;
  return (
    <Link to="/live/watch/$channelId" params={{ channelId: channel.id }} data-testid="on-now-card"
      className={`tv-focus group relative flex shrink-0 flex-col overflow-hidden rounded-[6px] bg-surface-2 ${rowCard}`}>
      <div className="relative aspect-video w-full overflow-hidden bg-gradient-to-br from-surface-3 to-surface">
        {p.imageUrl
          ? <img src={p.imageUrl} alt="" loading="lazy" className="size-full object-cover transition-transform duration-500 group-hover:scale-[1.03]" />
          : <div className="grid size-full place-items-center"><ChannelBadge channel={channel} size={72} /></div>}
        <div className="absolute inset-x-0 bottom-0 h-1/2 bg-gradient-to-t from-black/85 to-transparent" />
        <div className="absolute bottom-3 left-3 flex items-center gap-2">
          {p.imageUrl && <ChannelBadge channel={channel} size={32} />}
          <span className="inline-flex h-5 items-center gap-1.5 rounded-[3px] bg-danger/90 px-1.5 text-[10px] font-bold tracking-[0.1em] text-white uppercase">
            <span className="size-1.5 rounded-full bg-white" aria-hidden />{t("livetv.live")}
          </span>
        </div>
        <div className="absolute inset-x-0 bottom-0 h-[3px] bg-white/20" aria-hidden>
          <div className="h-full bg-[var(--amb-a)]" style={{ width: `${progress(p, now) * 100}%` }} />
        </div>
      </div>
      <div className="flex flex-col gap-0.5 px-3 py-2.5">
        <p className="truncate text-[15px] font-semibold">{p.title}</p>
        <p className="truncate text-xs text-text/55 tabular">
          {channel.name} · {timeOf(p.start, i18n.resolvedLanguage)}–{timeOf(p.end, i18n.resolvedLanguage)}
        </p>
      </div>
    </Link>
  );
}

/** Home: what is on now on Live TV (favourite channels first); hidden when there is nothing. */
export function OnNowRow() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const enabled = canWatchLiveTv(me);
  const now = useNow();
  const q = useQuery({
    queryKey: liveKeys.onNow,
    enabled,
    queryFn: async () => unwrap(await api.GET("/livetv/on-now", { params: { query: { limit: 20 } } })),
    refetchInterval: 120_000,
  });
  const list = (Array.isArray(q.data) ? q.data : []).filter((c) => c.now);
  if (!enabled) return null;
  return (
    <Row title={t("livetv.onNow")} testId="on-now" count={list.length}
      action={<Link to="/live" className="pb-0.5 text-sm font-semibold text-text/55 hover:text-text">{t("livetv.openGuide")}</Link>}>
      {list.map((c) => <OnNowCard key={c.id} channel={c} now={now} />)}
    </Row>
  );
}
