import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ListMusic, Music, Pause, Play, Repeat, Repeat1, Shuffle, SkipBack, SkipForward, Volume2, VolumeX, X } from "lucide-react";
import { coverUrl, useMusic } from "./MusicPlayer";

function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

const btn = "tv-focus grid size-10 shrink-0 place-items-center rounded-[6px] text-text/80 transition-colors hover:bg-white/10 hover:text-text disabled:opacity-30";

/** The music bar: always reachable while browsing, above the phone tab bar. */
export function NowPlayingBar() {
  const { t } = useTranslation();
  const m = useMusic();
  const [queueOpen, setQueueOpen] = useState(false);
  if (!m.current) return null;
  const c = m.current;
  const cover = coverUrl(c);
  const pct = m.duration ? Math.min(100, (m.position / m.duration) * 100) : 0;
  return (
    <>
      {queueOpen && (
        <aside aria-label={t("music.queue")} data-testid="music-queue"
          className="fixed right-3 bottom-[9.5rem] z-50 flex max-h-[60vh] w-[min(94vw,420px)] flex-col overflow-hidden rounded-[8px] border border-border bg-surface/97 shadow-2xl backdrop-blur-xl md:right-6 md:bottom-28">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="font-head text-base">{t("music.queue")}</h2>
            <button type="button" onClick={() => setQueueOpen(false)} aria-label={t("common.close")} className={btn}><X className="size-4" aria-hidden /></button>
          </div>
          <ol className="overflow-y-auto py-1">
            {m.queue.map((q, i) => (
              <li key={`${q.id}-${i}`} className={`group flex items-center gap-3 px-4 py-2 ${i === m.index ? "bg-white/[0.06]" : "hover:bg-white/[0.04]"}`}>
                <button type="button" onClick={() => m.jump(i)} className="flex min-w-0 flex-1 items-center gap-3 text-left">
                  <span className="w-5 text-right text-xs text-text/40 tabular">{i === m.index && m.playing ? "▶" : i + 1}</span>
                  <span className="min-w-0">
                    <span className={`block truncate text-sm ${i === m.index ? "font-semibold text-text" : "text-text/85"}`}>{q.title}</span>
                    <span className="block truncate text-xs text-text/50">{q.artist}</span>
                  </span>
                </button>
                {i !== m.index && (
                  <button type="button" onClick={() => m.remove(i)} aria-label={t("music.removeFromQueue")} className="grid size-7 place-items-center rounded-[4px] text-text/40 opacity-0 hover:text-text group-hover:opacity-100">
                    <X className="size-3.5" aria-hidden />
                  </button>
                )}
              </li>
            ))}
          </ol>
        </aside>
      )}
      <div role="region" aria-label={t("music.nowPlaying")} data-testid="now-playing-bar"
        className="fixed inset-x-0 bottom-16 z-40 border-t border-border bg-bg/94 backdrop-blur-xl md:bottom-0">
        <div className="relative h-[3px] bg-white/10">
          <div className="absolute inset-y-0 left-0 bg-[var(--amb-a)]" style={{ width: `${pct}%` }} />
          <input type="range" min={0} max={m.duration || 1} value={m.position} step={1000} aria-label={t("player.seek")}
            onChange={(e) => m.seek(Number(e.target.value))} className="absolute inset-x-0 -top-2 h-5 w-full cursor-pointer opacity-0" />
        </div>
        <div className="flex h-[4.5rem] items-center gap-3 px-3 sm:px-6">
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <span className="relative grid size-12 shrink-0 place-items-center overflow-hidden rounded-[4px] bg-surface-2">
              {cover ? <img src={cover} alt="" className="absolute inset-0 size-full object-cover" /> : <Music className="size-5 text-text/40" aria-hidden />}
            </span>
            <span className="min-w-0">
              {c.parentId
                ? <Link to="/item/$itemId" params={{ itemId: c.parentId }} className="block truncate text-[15px] font-semibold hover:underline">{c.title}</Link>
                : <span className="block truncate text-[15px] font-semibold">{c.title}</span>}
              <span className="block truncate text-[13px] text-text/55">{[c.artist, c.album].filter(Boolean).join(" · ")}</span>
            </span>
          </div>
          <div className="flex items-center gap-1">
            <button type="button" onClick={() => m.setShuffle(!m.shuffle)} aria-pressed={m.shuffle} aria-label={t("music.shuffle")}
              className={`${btn} max-sm:hidden ${m.shuffle ? "!text-[var(--amb-a)]" : ""}`}><Shuffle className="size-4" aria-hidden /></button>
            <button type="button" onClick={m.prev} aria-label={t("music.previous")} className={btn}><SkipBack className="size-5 fill-current" aria-hidden /></button>
            <button type="button" onClick={m.toggle} aria-label={m.playing ? t("player.pause") : t("player.play")} data-testid="music-toggle"
              className="tv-focus grid size-11 place-items-center rounded-full bg-text text-bg transition-transform hover:scale-105">
              {m.playing ? <Pause className="size-5 fill-current" aria-hidden /> : <Play className="ml-0.5 size-5 fill-current" aria-hidden />}
            </button>
            <button type="button" onClick={m.next} disabled={m.index >= m.queue.length - 1 && m.repeat === "off"} aria-label={t("music.next")} className={btn}><SkipForward className="size-5 fill-current" aria-hidden /></button>
            <button type="button" onClick={() => m.setRepeat(m.repeat === "off" ? "all" : m.repeat === "all" ? "one" : "off")} aria-label={t(`music.repeat_${m.repeat}`)}
              className={`${btn} max-sm:hidden ${m.repeat !== "off" ? "!text-[var(--amb-a)]" : ""}`}>
              {m.repeat === "one" ? <Repeat1 className="size-4" aria-hidden /> : <Repeat className="size-4" aria-hidden />}
            </button>
          </div>
          <div className="flex flex-1 items-center justify-end gap-2">
            <span className="hidden text-xs text-text/50 tabular lg:inline">{clock(m.position)} / {clock(m.duration)}</span>
            <button type="button" onClick={() => m.setVolume(m.volume > 0 ? 0 : 1)} aria-label={m.volume > 0 ? t("player.mute") : t("player.unmute")} className={`${btn} max-md:hidden`}>
              {m.volume > 0 ? <Volume2 className="size-4" aria-hidden /> : <VolumeX className="size-4" aria-hidden />}
            </button>
            <input type="range" min={0} max={1} step={0.05} value={m.volume} onChange={(e) => m.setVolume(Number(e.target.value))} aria-label={t("player.volume")}
              className="hidden w-24 accent-[var(--color-text)] md:block" />
            <button type="button" onClick={() => setQueueOpen(!queueOpen)} aria-pressed={queueOpen} aria-label={t("music.queue")} className={btn}><ListMusic className="size-5" aria-hidden /></button>
            <button type="button" onClick={m.stop} aria-label={t("music.close")} className={`${btn} max-sm:hidden`}><X className="size-4" aria-hidden /></button>
          </div>
        </div>
      </div>
    </>
  );
}
