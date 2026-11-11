import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useParams, useRouter } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type Hls from "hls.js";
import { ArrowLeft, Maximize, Minimize, Pause, Play, RotateCcw, RotateCw, Volume2, VolumeX } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, ErrorText, Spinner } from "@/components/ui";
import { detectDeviceProfile } from "@/features/player/deviceProfile";
import { useMusic } from "@/features/music/MusicPlayer";
import type { Playback } from "./api";
import { progress, timeOf } from "./time";

/** Seconds behind the live edge below which the stream counts as live. */
export const LIVE_SLACK = 12;
const JUMP = 30;
const HIDE_AFTER_MS = 3200;
const iconBtn = "grid size-11 place-items-center rounded-[6px] text-white transition-[background-color,transform] duration-150 hover:bg-white/15 active:scale-95";

/** "−2:05" behind live. */
export function behindLabel(sec: number): string {
  const s = Math.max(0, Math.round(sec));
  return `−${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** Plays a channel as live HLS with a rolling time-shift window (pause, rewind, go live). */
export function LivePlayer() {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage;
  const { channelId } = useParams({ from: "/app/livetv/watch/$channelId" });
  const navigate = useNavigate();
  const router = useRouter();
  const music = useMusic();
  useEffect(() => { if (music.playing) music.toggle(); }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const rootRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [paused, setPaused] = useState(true);
  const [waiting, setWaiting] = useState(true);
  const [muted, setMuted] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [chrome, setChrome] = useState(true);
  const [win, setWin] = useState({ start: 0, end: 0, at: 0 });
  const hideTimer = useRef<number | undefined>(undefined);

  const channel = useQuery({
    queryKey: ["livetv", "channel", channelId],
    queryFn: async () => unwrap(await api.GET("/livetv/channels/{channelId}", { params: { path: { channelId } } })),
    refetchInterval: 60_000,
  });
  const tune = useMutation({
    mutationFn: async (): Promise<Playback> =>
      unwrap(await api.POST("/livetv/channels/{channelId}/play", { params: { path: { channelId } }, body: { device: detectDeviceProfile() } })),
  });
  useEffect(() => { tune.mutate(); }, [channelId]); // eslint-disable-line react-hooks/exhaustive-deps

  const info = tune.data;
  // Attach the live playlist: Safari plays HLS itself, elsewhere hls.js keeps the
  // whole server window seekable (back buffer = the time-shift length).
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !info) return;
    let cancelled = false;
    hlsRef.current?.destroy();
    hlsRef.current = null;
    if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = info.url;
      void video.play().catch(() => undefined);
    } else {
      void import("hls.js").then(({ default: HlsCtor }) => {
        if (cancelled) return;
        const hls = new HlsCtor({ liveDurationInfinity: true, backBufferLength: info.timeshiftSeconds, maxBufferLength: 30 });
        hls.loadSource(info.url);
        hls.attachMedia(video);
        hlsRef.current = hls;
        void video.play().catch(() => undefined);
      });
    }
    return () => { cancelled = true; };
  }, [info]);
  useEffect(() => () => hlsRef.current?.destroy(), []);

  const measure = useCallback(() => {
    const v = videoRef.current;
    if (!v || !v.seekable.length) return;
    setWin({ start: v.seekable.start(0), end: v.seekable.end(v.seekable.length - 1), at: v.currentTime });
  }, []);
  useEffect(() => {
    const id = window.setInterval(measure, 1000);
    return () => window.clearInterval(id);
  }, [measure]);

  const behind = Math.max(0, win.end - win.at);
  const atLive = behind < LIVE_SLACK;
  const seek = useCallback((sec: number) => {
    const v = videoRef.current;
    if (!v || !v.seekable.length) return;
    const s = v.seekable.start(0);
    const e = v.seekable.end(v.seekable.length - 1);
    v.currentTime = Math.min(e, Math.max(s, sec));
    measure();
  }, [measure]);
  const goLive = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    const edge = hlsRef.current?.liveSyncPosition ?? (v.seekable.length ? v.seekable.end(v.seekable.length - 1) - 3 : 0);
    seek(edge);
    void v.play().catch(() => undefined);
  }, [seek]);
  const togglePlay = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) void v.play().catch(() => undefined);
    else v.pause();
  }, []);
  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    else void rootRef.current?.requestFullscreen?.().catch(() => undefined);
  }, []);
  useEffect(() => {
    const onFs = () => setFullscreen(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFs);
    return () => document.removeEventListener("fullscreenchange", onFs);
  }, []);

  const poke = useCallback(() => {
    setChrome(true);
    window.clearTimeout(hideTimer.current);
    hideTimer.current = window.setTimeout(() => { if (!videoRef.current?.paused) setChrome(false); }, HIDE_AFTER_MS);
  }, []);
  useEffect(() => () => window.clearTimeout(hideTimer.current), []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return;
      if (e.key === " " || e.key === "k") { e.preventDefault(); togglePlay(); }
      else if (e.key === "ArrowLeft") seek((videoRef.current?.currentTime ?? 0) - 10);
      else if (e.key === "ArrowRight") seek((videoRef.current?.currentTime ?? 0) + 10);
      else if (e.key === "f") toggleFullscreen();
      else if (e.key === "m" && videoRef.current) { videoRef.current.muted = !videoRef.current.muted; }
      else return;
      poke();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [togglePlay, seek, toggleFullscreen, poke]);

  const back = () => {
    if (router.history.length > 1) router.history.back();
    else void navigate({ to: "/livetv" });
  };
  const ch = channel.data;
  const now = info?.now ?? ch?.now;
  const span = win.end - win.start;
  const pct = span > 0 ? ((win.at - win.start) / span) * 100 : 100;

  return (
    <div ref={rootRef} className="fixed inset-0 z-50 bg-black text-white" data-testid="live-player" onPointerMove={poke} onClick={poke}
      style={{ cursor: chrome ? "default" : "none" }}>
      <video ref={videoRef} className="size-full object-contain" playsInline aria-label={ch?.name ?? t("livetv.title")}
        onPlay={() => setPaused(false)} onPause={() => { setPaused(true); setChrome(true); }}
        onWaiting={() => setWaiting(true)} onPlaying={() => setWaiting(false)} onCanPlay={() => setWaiting(false)}
        onVolumeChange={(e) => setMuted(e.currentTarget.muted)} onTimeUpdate={measure}
        onDoubleClick={toggleFullscreen} />

      {(tune.isPending || (waiting && !tune.isError)) && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center"><Spinner className="size-10" /></div>
      )}
      {tune.isError && (
        <div className="absolute inset-0 grid place-items-center p-6">
          <div className="flex max-w-md flex-col items-center gap-4 text-center">
            <ErrorText error={tune.error} />
            <div className="flex gap-3">
              <Button onClick={() => tune.mutate()}>{t("livetv.retry")}</Button>
              <Button variant="secondary" onClick={back}>{t("livetv.back")}</Button>
            </div>
          </div>
        </div>
      )}

      <div className={`absolute inset-0 flex flex-col justify-between transition-opacity duration-300 ${chrome ? "opacity-100" : "pointer-events-none opacity-0"}`}>
        <div className="flex items-start gap-3 bg-gradient-to-b from-black/80 to-transparent p-4 sm:p-6">
          <button type="button" className={iconBtn} aria-label={t("livetv.back")} onClick={back}><ArrowLeft className="size-6" aria-hidden /></button>
          <div className="min-w-0 pt-1">
            <p className="text-xs font-semibold tracking-[0.08em] text-white/60 uppercase">{[ch?.number, ch?.name].filter(Boolean).join(" · ")}</p>
            {now && (
              <p className="truncate text-lg font-semibold sm:text-xl" data-testid="live-now">
                {now.title}<span className="ml-2 text-sm font-normal text-white/60 tabular">{timeOf(now.start, lang)}–{timeOf(now.end, lang)}</span>
              </p>
            )}
            {ch?.next && <p className="truncate text-sm text-white/60">{t("livetv.nextShort")} · {timeOf(ch.next.start, lang)} {ch.next.title}</p>}
          </div>
        </div>

        <div className="flex flex-col gap-3 bg-gradient-to-t from-black/85 to-transparent p-4 pt-16 sm:p-6 sm:pt-20">
          {now && (
            <div className="h-[3px] w-full max-w-md bg-white/20" aria-hidden title={t("livetv.programProgress")}>
              <div className="h-full bg-[var(--amb-a)]" style={{ width: `${progress(now, Date.now()) * 100}%` }} />
            </div>
          )}
          <div className="flex items-center gap-3">
            <input type="range" min={0} max={100} step={0.1} value={Math.min(100, Math.max(0, pct))} aria-label={t("livetv.timeshift")}
              aria-valuetext={atLive ? t("livetv.live") : behindLabel(behind)} disabled={span <= 0}
              onChange={(e) => seek(win.start + (Number(e.target.value) / 100) * span)}
              className="h-1 flex-1 cursor-pointer accent-[var(--amb-a)]" />
            <span className="w-16 text-right text-sm text-white/75 tabular" data-testid="live-behind">{atLive ? "" : behindLabel(behind)}</span>
          </div>
          <div className="flex items-center gap-1">
            <button type="button" className={iconBtn} aria-label={paused ? t("player.play") : t("player.pause")} onClick={togglePlay}>
              {paused ? <Play className="size-6 fill-current" aria-hidden /> : <Pause className="size-6 fill-current" aria-hidden />}
            </button>
            <button type="button" className={iconBtn} aria-label={t("livetv.rewind", { n: JUMP })} onClick={() => seek(win.at - JUMP)}><RotateCcw className="size-5" aria-hidden /></button>
            <button type="button" className={iconBtn} aria-label={t("livetv.forward", { n: JUMP })} disabled={atLive} onClick={() => seek(win.at + JUMP)}><RotateCw className="size-5" aria-hidden /></button>
            <button type="button" onClick={goLive} disabled={atLive && !paused} data-testid="go-live"
              className={`ml-2 inline-flex h-8 items-center gap-2 rounded-[4px] px-3 text-xs font-bold tracking-[0.1em] uppercase transition-colors ${
                atLive && !paused ? "bg-danger/90 text-white" : "bg-white/15 text-white hover:bg-white/25"}`}>
              <span className={`size-2 rounded-full ${atLive && !paused ? "animate-pulse bg-white" : "bg-white/60"}`} aria-hidden />
              {atLive && !paused ? t("livetv.live") : t("livetv.goLive")}
            </button>
            {info && (
              <span className="ml-3 hidden text-xs text-white/50 sm:inline">
                {t("livetv.timeshiftInfo", { minutes: Math.round(info.timeshiftSeconds / 60) })}
                {info.method === "transcode" ? ` · ${t("livetv.transcoding")}` : ""}
              </span>
            )}
            <div className="ml-auto flex items-center gap-1">
              <button type="button" className={iconBtn} aria-label={muted ? t("player.unmute") : t("player.mute")}
                onClick={() => { if (videoRef.current) videoRef.current.muted = !videoRef.current.muted; }}>
                {muted ? <VolumeX className="size-5" aria-hidden /> : <Volume2 className="size-5" aria-hidden />}
              </button>
              <button type="button" className={iconBtn} aria-label={fullscreen ? t("player.exitFullscreen") : t("player.fullscreen")} onClick={toggleFullscreen}>
                {fullscreen ? <Minimize className="size-5" aria-hidden /> : <Maximize className="size-5" aria-hidden />}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
