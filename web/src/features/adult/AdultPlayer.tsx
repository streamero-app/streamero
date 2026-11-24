import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type Hls from "hls.js";
import { ArrowLeft } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, ErrorText, Spinner } from "@/components/ui";
import { detectDeviceProfile, SAFE_PROFILE } from "@/features/player/deviceProfile";
import { nativeHls, tryPlay } from "@/features/player/playback";
import type { components } from "@/api/schema.gen";

type PlaybackInfo = components["schemas"]["PlaybackInfo"];
const SAVE_EVERY_MS = 10_000;

/**
 * Player of the adult section (D-053): the browser's own controls over the same
 * signed streams as the main player, without watch together, remote control,
 * "up next" or anything reported to the activity dashboard.
 */
export function AdultPlayer() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { sceneId } = useParams({ from: "/app/adult/watch/$sceneId" });
  const search = useSearch({ from: "/app/adult/watch/$sceneId" });
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const offset = useRef(0);
  const duration = useRef<number | undefined>(undefined);
  const lastSave = useRef(0);
  const safe = useRef(false);
  const [info, setInfo] = useState<PlaybackInfo | null>(null);
  const [blocked, setBlocked] = useState(false);
  const [failed, setFailed] = useState(false);

  const start = useMutation({
    mutationFn: async (v: { startMs?: number; safe?: boolean }) =>
      unwrap(await api.POST("/adult/scenes/{sceneId}/playback", {
        params: { path: { sceneId } },
        body: { device: v.safe ? SAFE_PROFILE : detectDeviceProfile(), ...(v.startMs !== undefined ? { startMs: v.startMs } : {}) },
      })),
    onSuccess: (data) => {
      duration.current = data.durationMs;
      setInfo(data);
    },
  });

  const position = useCallback(() => {
    const v = videoRef.current;
    return v ? Math.round(offset.current + v.currentTime * 1000) : 0;
  }, []);

  const save = useCallback((force: boolean) => {
    const now = Date.now();
    if (!force && now - lastSave.current < SAVE_EVERY_MS) return;
    lastSave.current = now;
    const pos = position();
    if (pos <= 0) return;
    void api.PUT("/adult/scenes/{sceneId}/progress", { params: { path: { sceneId } }, body: { positionMs: pos, durationMs: duration.current } })
      .then(() => qc.invalidateQueries({ queryKey: ["adult"] }));
  }, [position, qc, sceneId]);
  const saveRef = useRef(save);
  saveRef.current = save;

  useEffect(() => {
    start.mutate({ startMs: search.start });
  }, [sceneId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !info) return;
    let cancelled = false;
    hlsRef.current?.destroy();
    hlsRef.current = null;
    const fail = () => {
      if (cancelled) return;
      cancelled = true;
      if (safe.current) {
        setFailed(true);
        return;
      }
      safe.current = true;
      start.mutate({ startMs: position() || info.startMs, safe: true });
    };
    const begin = () => {
      if (!cancelled) void tryPlay(video).then((refused) => refused && setBlocked(true));
    };
    video.addEventListener("error", fail);
    if (info.method === "direct") {
      offset.current = 0;
      video.src = info.url;
      video.addEventListener("loadedmetadata", () => { if (info.startMs > 0) video.currentTime = info.startMs / 1000; }, { once: true });
      video.addEventListener("canplay", begin, { once: true });
    } else {
      offset.current = info.startMs;
      if (nativeHls(video)) {
        video.src = info.url;
        video.addEventListener("canplay", begin, { once: true });
      } else {
        void import("hls.js").then(({ default: HlsCtor }) => {
          if (cancelled) return;
          const hls = new HlsCtor({ maxBufferLength: 30 });
          hls.on(HlsCtor.Events.MANIFEST_PARSED, begin);
          hls.on(HlsCtor.Events.ERROR, (_e, data) => { if (data.fatal) fail(); });
          hls.loadSource(info.url);
          hls.attachMedia(video);
          hlsRef.current = hls;
        });
      }
    }
    return () => {
      cancelled = true;
      video.removeEventListener("error", fail);
      video.removeEventListener("canplay", begin);
    };
  }, [info]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => () => {
    saveRef.current(true);
    hlsRef.current?.destroy();
  }, []);

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-black" data-testid="adult-player">
      <div className="absolute inset-x-0 top-0 z-10 flex items-center gap-2 bg-gradient-to-b from-black/80 to-transparent p-3">
        <Link to="/adult/scene/$sceneId" params={{ sceneId }} onClick={() => save(true)}
          className="inline-flex h-10 items-center gap-1.5 rounded-[6px] pr-4 pl-2.5 text-sm font-medium text-white hover:bg-white/10">
          <ArrowLeft className="size-5" aria-hidden />{t("player.back")}
        </Link>
      </div>
      <video ref={videoRef} className="size-full" controls playsInline
        onTimeUpdate={() => save(false)} onPause={() => save(true)} onEnded={() => save(true)} />
      {start.isPending && !info && <div className="absolute inset-0 grid place-items-center"><Spinner /></div>}
      {(start.error || failed) && (
        <div className="absolute inset-0 grid place-items-center p-6 text-center">
          <div className="flex flex-col items-center gap-3">
            {failed ? <p className="text-sm text-danger">{t("player.playbackFailed")}</p> : <ErrorText error={start.error} />}
            <Button variant="secondary" onClick={() => { safe.current = false; setFailed(false); start.mutate({ startMs: position() || undefined }); }}>
              {t("player.retry")}
            </Button>
          </div>
        </div>
      )}
      {blocked && (
        <button type="button" className="absolute inset-0 grid place-items-center bg-black/40 text-white"
          onClick={() => { setBlocked(false); void videoRef.current?.play(); }}>
          <span className="rounded-full bg-white/15 px-6 py-3 text-lg font-semibold backdrop-blur">{t("adult.play")}</span>
        </button>
      )}
    </div>
  );
}
