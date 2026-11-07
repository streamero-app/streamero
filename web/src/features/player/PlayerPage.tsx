import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type Hls from "hls.js";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { ErrorText } from "@/components/ui";
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
    onSuccess: (data) => setInfo(data),
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

  // Start once (honours ?start= from "play from beginning").
  useEffect(() => {
    start.mutate({ startMs: search.start });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

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
        <Link to="/item/$itemId" params={{ itemId }} className="rounded-md px-2 py-1 text-sm hover:bg-white/10" onClick={() => save(true)}>
          ← {t("player.back")}
        </Link>
        <span className="truncate font-medium">{it?.title}</span>
        <div className="ml-auto flex items-center gap-3 text-sm">
          {info && info.audio.length > 1 && (
            <label className="flex items-center gap-1">
              {t("library.audio")}
              <select className="rounded bg-white/10 px-2 py-1" value={audioIndex ?? info.audioIndex ?? 0}
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
              <select className="rounded bg-white/10 px-2 py-1" value={subtitle} onChange={(e) => setSubtitle(Number(e.target.value))}
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
          onTimeUpdate={() => save()} onPause={() => save(true)} onEnded={() => save(true)}>
          {info?.subtitles.map((s) => (
            <track key={s.index} kind="subtitles" src={s.url} srcLang={s.language ?? "und"} label={s.language ?? String(s.index)} />
          ))}
        </video>
        {start.isPending && <p role="status" className="absolute text-white/80">{t("player.preparing")}</p>}
        {start.error && <div className="absolute max-w-md rounded-card bg-surface p-4"><ErrorText error={start.error} /></div>}
      </div>
    </div>
  );
}
