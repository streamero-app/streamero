import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type Hls from "hls.js";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { ErrorText, Spinner } from "@/components/ui";
import { ArrowLeft, Play, X } from "lucide-react";
import { imageUrl } from "@/features/library/format";
import { detectDeviceProfile } from "./deviceProfile";

type PlaybackInfo = components["schemas"]["PlaybackInfo"];
const SAVE_EVERY_MS = 10_000;

export function PlayerPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { itemId } = useParams({ from: "/app/watch/$itemId" });
  const search = useSearch({ from: "/app/watch/$itemId" });
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [info, setInfo] = useState<PlaybackInfo | null>(null);
  const [audioIndex, setAudioIndex] = useState<number | undefined>(undefined);
  const [subtitle, setSubtitle] = useState<number>(-1);
  const offsetRef = useRef(0); // HLS streams start at info.startMs; direct play starts at 0
  const lastSave = useRef(0);
  const firstStart = useRef(true);
  const navigate = useNavigate();
  const [remaining, setRemaining] = useState<number | null>(null);
  const [ended, setEnded] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const prefs = useQuery({ queryKey: ["preferences"], queryFn: async () => unwrap(await api.GET("/me/preferences")) });

  const detail = useQuery({
    queryKey: ["item", itemId],
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId } } })),
  });

  const start = useMutation({
    mutationFn: async (v: { audioIndex?: number; startMs?: number }) =>
      unwrap(await api.POST("/items/{itemId}/playback", {
        params: { path: { itemId } },
        body: { device: detectDeviceProfile(), audioIndex: v.audioIndex, startMs: v.startMs },
      })),
    onSuccess: (data) => {
      setInfo(data);
      // The viewer's subtitle preference applies when an item starts, not on audio switches.
      if (firstStart.current) {
        setSubtitle(data.subtitleDefault ?? -1);
        firstStart.current = false;
      }
    },
  });
  const isEpisode = detail.data?.item.kind === "episode";
  const next = useQuery({
    queryKey: ["next", itemId],
    enabled: isEpisode,
    retry: false,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/next", { params: { path: { itemId } } })),
  });

  const position = useCallback(() => {
    const v = videoRef.current;
    return v ? Math.round(offsetRef.current + v.currentTime * 1000) : 0;
  }, []);

  const save = useCallback((force = false) => {
    const now = Date.now();
    if (!force && now - lastSave.current < SAVE_EVERY_MS) return;
    lastSave.current = now;
    const pos = position();
    if (pos <= 0) return;
    void api.PUT("/items/{itemId}/progress", {
      params: { path: { itemId } },
      body: { positionMs: pos, durationMs: info?.durationMs },
    }).then(() => qc.invalidateQueries({ queryKey: ["continue"] }));
  }, [itemId, info?.durationMs, position, qc]);

  // Start (honours ?start= from "play from beginning"); again when autoplay moves to the next episode.
  useEffect(() => {
    firstStart.current = true;
    setInfo(null);
    setSubtitle(-1);
    setAudioIndex(undefined);
    setEnded(false);
    setDismissed(false);
    setRemaining(null);
    start.mutate({ startMs: search.start });
  }, [itemId]); // eslint-disable-line react-hooks/exhaustive-deps

  // Up next: count down over the end credits (or after the end) and move on.
  const autoplay = prefs.data?.autoplayNext ?? true;
  const showNext = !!next.data && !dismissed && (ended || (remaining !== null && remaining <= 20));
  const [countdown, setCountdown] = useState(10);
  useEffect(() => {
    if (!showNext) {
      setCountdown(10);
      return;
    }
    if (!autoplay) return;
    const timer = window.setInterval(() => setCountdown((c) => c - 1), 1000);
    return () => window.clearInterval(timer);
  }, [showNext, autoplay]);
  const playNext = useCallback(() => {
    if (!next.data) return;
    save(true);
    void navigate({ to: "/watch/$itemId", params: { itemId: next.data.id }, search: {}, replace: true });
  }, [next.data, navigate, save]);
  useEffect(() => {
    if (showNext && autoplay && countdown <= 0) playNext();
  }, [countdown, showNext, autoplay, playNext]);

  // Attach the stream to the <video> element.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !info) return;
    let cancelled = false;
    hlsRef.current?.destroy();
    hlsRef.current = null;
    if (info.method === "direct") {
      offsetRef.current = 0;
      video.src = info.url;
      const seek = () => {
        if (info.startMs > 0) video.currentTime = info.startMs / 1000;
      };
      video.addEventListener("loadedmetadata", seek, { once: true });
    } else {
      offsetRef.current = info.startMs;
      if (video.canPlayType("application/vnd.apple.mpegurl")) {
        video.src = info.url; // Safari / iOS play HLS natively
      } else {
        void import("hls.js").then(({ default: HlsCtor }) => {
          if (cancelled) return;
          const hls = new HlsCtor({ maxBufferLength: 30 });
          hls.loadSource(info.url);
          hls.attachMedia(video);
          hlsRef.current = hls;
        });
      }
    }
    void video.play().catch(() => undefined); // autoplay may need a click
    return () => {
      cancelled = true;
    };
  }, [info]);

  // Subtitles: show the chosen <track>, hide the others.
  useEffect(() => {
    const tracks = videoRef.current?.textTracks;
    if (!tracks) return;
    for (let i = 0; i < tracks.length; i++) tracks[i]!.mode = i === subtitle ? "showing" : "disabled";
  }, [subtitle, info]);

  // Save progress on leave.
  useEffect(() => () => {
    save(true);
    hlsRef.current?.destroy();
  }, [save]);

  const changeAudio = (idx: number) => {
    setAudioIndex(idx);
    save(true);
    start.mutate({ audioIndex: idx, startMs: position() });
  };

  const it = detail.data?.item;
  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-black" data-testid="player">
      <div className="flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent px-4 py-3 text-white">
        <Link to="/item/$itemId" params={{ itemId }} className="inline-flex h-9 items-center gap-1.5 rounded-full pr-3 pl-2 text-sm transition-colors hover:bg-white/10" onClick={() => save(true)}>
          <ArrowLeft className="size-4" aria-hidden />{t("player.back")}
        </Link>
        <span className="truncate font-display text-xl">{it?.title}</span>
        <div className="ml-auto flex items-center gap-3 text-sm">
          {info && info.audio.length > 1 && (
            <label className="flex items-center gap-1">
              {t("library.audio")}
              <select className="h-8 rounded-full bg-white/10 px-3 ring-1 ring-white/10" value={audioIndex ?? info.audioIndex ?? 0}
                onChange={(e) => changeAudio(Number(e.target.value))} aria-label={t("library.audio")}>
                {info.audio.map((a, i) => (
                  <option key={i} value={i} className="text-black">
                    {[a.language?.toUpperCase() ?? `#${i + 1}`, a.codec.toUpperCase(), a.title].filter(Boolean).join(" ")}
                  </option>
                ))}
              </select>
            </label>
          )}
          {info && info.subtitles.length > 0 && (
            <label className="flex items-center gap-1">
              {t("library.subtitles")}
              <select className="h-8 rounded-full bg-white/10 px-3 ring-1 ring-white/10" value={subtitle} onChange={(e) => setSubtitle(Number(e.target.value))}
                aria-label={t("library.subtitles")}>
                <option value={-1} className="text-black">{t("player.off")}</option>
                {info.subtitles.map((s, i) => (
                  <option key={s.index} value={i} className="text-black">
                    {(s.language?.toUpperCase() ?? `#${i + 1}`) + (s.forced ? ` (${t("library.forced")})` : "")}
                  </option>
                ))}
              </select>
            </label>
          )}
          {info && <span className="hidden text-xs text-white/60 sm:inline" data-testid="play-method">{t(`player.method_${info.method}`)}</span>}
        </div>
      </div>
      <div className="relative flex flex-1 items-center justify-center">
        <video ref={videoRef} className="h-full w-full" controls playsInline autoPlay data-testid="video"
          onTimeUpdate={(e) => {
            save();
            const v = e.currentTarget;
            const total = (info?.durationMs ?? 0) / 1000;
            if (total > 0) setRemaining(total - (offsetRef.current / 1000 + v.currentTime));
          }}
          onPause={() => save(true)} onEnded={() => { save(true); setEnded(true); }}>
          {info?.subtitles.map((s) => (
            <track key={s.index} kind="subtitles" src={s.url} srcLang={s.language ?? "und"} label={s.language ?? String(s.index)} />
          ))}
        </video>
        {start.isPending && <p role="status" className="absolute flex items-center gap-3 text-white/80"><Spinner />{t("player.preparing")}</p>}
        {start.error && <div className="absolute max-w-md rounded-card bg-surface p-4"><ErrorText error={start.error} /></div>}
        {showNext && next.data && (
          <div className="absolute right-4 bottom-24 z-10 flex w-[min(92vw,380px)] gap-3 rounded-lg bg-black/85 p-3 text-white shadow-2xl ring-1 ring-white/15 backdrop-blur [animation:rise_300ms_var(--ease-out)_both] sm:right-8"
            role="dialog" aria-label={t("player.upNext")} data-testid="up-next">
            <div className="relative aspect-video w-32 shrink-0 overflow-hidden rounded-md bg-white/10">
              {next.data.hasPoster && <img src={imageUrl(next.data.id, "still")} alt="" className="absolute inset-0 size-full object-cover" />}
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <p className="text-xs text-white/60">{autoplay ? t("player.nextIn", { seconds: Math.max(0, countdown) }) : t("player.upNext")}</p>
              <p className="truncate text-sm font-semibold">
                {next.data.seasonNumber !== undefined && next.data.episodeNumber !== undefined
                  ? `S${next.data.seasonNumber}:E${next.data.episodeNumber} · ` : ""}{next.data.title}
              </p>
              <div className="mt-auto flex gap-2">
                <button type="button" onClick={playNext} autoFocus
                  className="inline-flex h-8 items-center gap-1.5 rounded-md bg-white px-3 text-sm font-bold text-black hover:bg-white/85">
                  <Play className="size-3.5 fill-current" aria-hidden />{t("player.playNext")}
                </button>
                <button type="button" onClick={() => setDismissed(true)} aria-label={t("common.close")}
                  className="grid size-8 place-items-center rounded-md bg-white/15 hover:bg-white/25"><X className="size-4" aria-hidden /></button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
