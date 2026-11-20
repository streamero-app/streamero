import { useEffect, useState } from "react";
import type { Channel } from "./api";

/** Ticks once a minute so the now-line and "on now" highlighting move. */
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}

/** A channel's logo (from its playlist, so another host: no referrer), else its number. */
export function ChannelBadge({ channel, size = 40 }: { channel: Pick<Channel, "name" | "logoUrl" | "number">; size?: number }) {
  const [broken, setBroken] = useState(false);
  if (channel.logoUrl && !broken) {
    return <img src={channel.logoUrl} alt="" loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setBroken(true)} style={{ width: size, height: size }}
      className="shrink-0 rounded-[6px] bg-white/[0.06] object-contain p-1" />;
  }
  return (
    <span aria-hidden style={{ width: size, height: size }}
      className="grid shrink-0 place-items-center rounded-[6px] bg-white/[0.07] text-[11px] font-bold text-text/70 tabular">
      {channel.number || channel.name.slice(0, 2).toUpperCase()}
    </span>
  );
}
