import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type Hls from "hls.js";
import {
  ArrowLeft, Captions, Gauge, Maximize, MessageSquare, Minimize, Pause, PictureInPicture2, Play, RotateCcw, RotateCw, SkipForward, Volume1, Volume2, VolumeX, X,
} from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useEnhancements } from "@/features/enhancements/useEnhancements";
import { formatDuration } from "@/features/library/format";
import { useMusic } from "@/features/music/MusicPlayer";
import type { components } from "@/api/schema.gen";
import { ErrorText, Spinner } from "@/components/ui";
import { imageUrl } from "@/features/library/format";
import { detectDeviceProfile } from "./deviceProfile";

type PlaybackInfo = components["schemas"]["PlaybackInfo"];
type Segment = components["schemas"]["MediaSegment"];
const SAVE_EVERY_MS = 10_000;

const SUB_SIZE = { small: "2.6vh", normal: "3.4vh", large: "4.4vh", xlarge: "5.6vh" } as const;
const SUB_COLOR = { white: "#ffffff", yellow: "#ffe14d", cyan: "#7ff3ff" } as const;

/** Short names of bitmap subtitle formats (ffprobe codec names or sidecar extensions). */
export function bitmapName(codec: string): string {
  const c = codec.toLowerCase();
  if (c.includes("pgs") || c === "sup") return "PGS";
  if (c.includes("dvb")) return "DVB";
  if (c.includes("dvd") || c === "sub" || c === "idx" || c === "vobsub") return "VobSub";
  return c.toUpperCase();
}

/** The viewer's subtitle look, as ::cue rules for the player's text tracks. */
export function subtitleCss(p?: { subtitleSize?: keyof typeof SUB_SIZE; subtitleColor?: keyof typeof SUB_COLOR; subtitleBackground?: "none" | "shadow" | "box"; subtitlePosition?: number }): string {
  const bg = p?.subtitleBackground ?? "shadow";
  const rules = [
    `font-size:${SUB_SIZE[p?.subtitleSize ?? "normal"]}`,
    `color:${SUB_COLOR[p?.subtitleColor ?? "white"]}`,
    `font-family:var(--font-sans)`,
    bg === "box" ? "background-color:rgb(0 0 0 / 0.75)" : "background-color:transparent",
    bg === "shadow" ? "text-shadow:0 0 4px #000,0 2px 6px #000,0 0 1px #000" : "text-shadow:none",
  ];
  // Lifting cues: the video's own line placement cannot be set from CSS, so shift the whole track area.
  const lift = p?.subtitlePosition ? `video::-webkit-media-text-track-container{transform:translateY(-${p.subtitlePosition}vh)}` : "";
  return `video::cue{${rules.join(";")}}${lift}`;
}
const HIDE_AFTER_MS = 3200;
const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2];

export function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

/** Seek bar with buffered range, chapter ticks and a trickplay preview on hover. */
function SeekBar({ positionMs, durationMs, bufferedMs, info, onSeek }: {
  positionMs: number; durationMs: number; bufferedMs: number; info: PlaybackInfo | null; onSeek: (ms: number) => void;
}) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<number | null>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const at = (e: { clientX: number }) => {
    const r = ref.current!.getBoundingClientRect();
    return r.width ? Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)) * durationMs : 0;
  };
  const shown = drag ?? positionMs;
  const pct = durationMs ? (shown / durationMs) * 100 : 0;
  const tp = info?.trickplay;
  const chapter = hover !== null ? [...(info?.chapters ?? [])].reverse().find((c) => c.startMs <= hover) : undefined;
  let thumb: { url: string; x: number; y: number } | null = null;
  if (tp && hover !== null) {
    const idx = Math.min(tp.count - 1, Math.floor(hover / tp.intervalMs));
    const perSheet = tp.columns * tp.rows;
    const sheet = Math.floor(idx / perSheet);
    const cell = idx % perSheet;
    if (tp.sheetUrls[sheet]) thumb = { url: tp.sheetUrls[sheet]!, x: (cell % tp.columns) * tp.width, y: Math.floor(cell / tp.columns) * tp.height };
  }
  const hoverPct = hover !== null && durationMs ? (hover / durationMs) * 100 : 0;
  return (
    <div className="group/seek relative flex h-6 cursor-pointer touch-none items-center" ref={ref}
      role="slider" tabIndex={0} aria-label={t("player.seek")} aria-valuemin={0} aria-valuemax={Math.round(durationMs / 1000)}
      aria-valuenow={Math.round(shown / 1000)} aria-valuetext={clock(shown)}
      onPointerMove={(e) => { setHover(at(e)); if (drag !== null) setDrag(at(e)); }}
      onPointerLeave={() => { if (drag === null) setHover(null); }}
      onPointerDown={(e: ReactPointerEvent) => { e.currentTarget.setPointerCapture?.(e.pointerId); setDrag(at(e)); }}
      onPointerUp={(e) => { if (drag !== null) onSeek(at(e)); setDrag(null); setHover(null); }}>
      <div className="relative h-1 w-full rounded-full bg-white/25 transition-[height] duration-150 group-hover/seek:h-1.5">
        <div className="absolute inset-y-0 left-0 rounded-full bg-white/35" style={{ width: `${durationMs ? Math.min(100, (bufferedMs / durationMs) * 100) : 0}%` }} />
        <div className="absolute inset-y-0 left-0 rounded-full bg-accent" style={{ width: `${pct}%` }} />
        {durationMs > 0 && info?.chapters?.filter((c) => c.startMs > 0).map((c) => (
          <span key={c.startMs} className="absolute inset-y-0 w-0.5 bg-black/70" style={{ left: `${(c.startMs / durationMs) * 100}%` }} aria-hidden />
        ))}
        <span className="absolute top-1/2 size-3.5 -translate-x-1/2 -translate-y-1/2 scale-0 rounded-full bg-accent shadow transition-transform duration-150 group-hover/seek:scale-100"
          style={{ left: `${pct}%` }} aria-hidden />
      </div>
      {hover !== null && (
        <div className="pointer-events-none absolute bottom-7 flex -translate-x-1/2 flex-col items-center gap-1.5"
          style={{ left: `clamp(${(tp?.width ?? 80) / 2}px, ${hoverPct}%, calc(100% - ${(tp?.width ?? 80) / 2}px))` }} data-testid="seek-preview">
          {thumb && tp && (
            <div className="overflow-hidden rounded-md shadow-2xl ring-2 ring-white/80"
              style={{ width: tp.width, height: tp.height, backgroundImage: `url(${thumb.url})`, backgroundPosition: `-${thumb.x}px -${thumb.y}px` }} />
          )}
          <span className="rounded bg-black/80 px-2 py-0.5 text-xs font-semibold text-white tabular">
            {chapter?.title ? `${chapter.title} · ` : ""}{clock(hover)}
          </span>
        </div>
      )}
    </div>
  );
}

const iconBtn = "grid size-11 place-items-center rounded-[6px] text-white transition-[background-color,transform] duration-150 hover:bg-white/15 active:scale-95";

export function PlayerPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { itemId } = useParams({ from: "/app/watch/$itemId" });
  const search = useSearch({ from: "/app/watch/$itemId" });
  // A film and a song never play at once.
  const music = useMusic();
  useEffect(() => { if (music.playing) music.toggle(); }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const rootRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [info, setInfo] = useState<PlaybackInfo | null>(null);
  const [audioIndex, setAudioIndex] = useState<number | undefined>(undefined);
  const [subtitle, setSubtitle] = useState<number>(-1);
  const offsetRef = useRef(0); // HLS streams start at info.startMs; direct play starts at 0
  const lastSave = useRef(0);
  const firstStart = useRef(true);
  const [position, setPosition] = useState(0);
  const [buffered, setBuffered] = useState(0);
  const [paused, setPaused] = useState(true);
  const helpAllowed = useRef(false);
  const helpOpen = useRef(false);
  const [waiting, setWaiting] = useState(false);
  const [volume, setVolume] = useState(1);
  const [muted, setMuted] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [fullscreen, setFullscreen] = useState(false);
  const [chrome, setChrome] = useState(true);
  const [panel, setPanel] = useState<"tracks" | "speed" | null>(null);
  const [ended, setEnded] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [skipped, setSkipped] = useState<string | null>(null);
  const hideTimer = useRef<number | undefined>(undefined);

  const detail = useQuery({
    queryKey: ["item", itemId],
    queryFn: async () => unwrap(await api.GET("/items/{itemId}", { params: { path: { itemId } } })),
  });
  const prefs = useQuery({ queryKey: ["preferences"], queryFn: async () => unwrap(await api.GET("/me/preferences")) });

  const start = useMutation({
    mutationFn: async (v: { audioIndex?: number; startMs?: number; subtitleIndex?: number }) =>
      unwrap(await api.POST("/items/{itemId}/playback", {
        params: { path: { itemId } },
        // The version chosen on the title page stays for audio switches and seeks.
        body: {
          device: detectDeviceProfile(), audioIndex: v.audioIndex, startMs: v.startMs === undefined ? undefined : Math.round(v.startMs),
          fileId: search.file, subtitleIndex: v.subtitleIndex,
        },
      })),
    onSuccess: (data) => {
      setInfo(data);
      // The viewer's subtitle preference (or the choice made on the title page) applies
      // when an item starts, not on audio switches or seeks.
      if (firstStart.current) {
        const chosen = search.sub === undefined ? undefined : search.sub < 0 ? -1 : data.subtitles.findIndex((x) => x.index === search.sub);
        setSubtitle(chosen !== undefined ? chosen : data.subtitleDefault ?? -1);
        firstStart.current = false;
      }
    },
  });
  // Messages and stop requests from an administrator (Settings → Dashboard).
  const [notice, setNotice] = useState<string | null>(null);
  const commands = useQuery({
    queryKey: ["my-commands", itemId],
    queryFn: async () => unwrap(await api.GET("/me/commands")),
    refetchInterval: 4000,
    enabled: !!info,
  });
  useEffect(() => {
    for (const c of Array.isArray(commands.data) ? commands.data : []) {
      if (c.kind === "stop") {
        videoRef.current?.pause();
        save(true);
        setNotice(c.text || t("player.stoppedByAdmin"));
        // Leave the message on screen for a moment, then return to the title.
        window.setTimeout(() => void navigate({ to: "/item/$itemId", params: { itemId }, replace: true }), 5000);
        return;
      }
      setNotice(c.text ?? "");
    }
  }, [commands.data]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!notice) return;
    const id = window.setTimeout(() => setNotice(null), 10_000);
    return () => window.clearTimeout(id);
  }, [notice]);
  const it = detail.data?.item;
  const isEpisode = it?.kind === "episode";
  const durationMs = info?.durationMs ?? it?.durationMs ?? 0;

  // In a playlist the next title is the playlist's next entry; otherwise the next episode.
  const playlist = useQuery({
    queryKey: ["playlist", search.playlist],
    enabled: !!search.playlist,
    queryFn: async () => unwrap(await api.GET("/playlists/{playlistId}", { params: { path: { playlistId: search.playlist! } } })),
  });
  const episodeNext = useQuery({
    queryKey: ["next", itemId],
    enabled: isEpisode && !search.playlist,
    retry: false,
    queryFn: async () => unwrap(await api.GET("/items/{itemId}/next", { params: { path: { itemId } } })),
  });
  const playlistNext = (() => {
    const items = playlist.data?.items ?? [];
    const at = items.findIndex((i) => i.id === itemId);
    return at >= 0 ? items.slice(at + 1).find((i) => i.kind === "movie" || i.kind === "episode") : undefined;
  })();
  const next = search.playlist ? { data: playlistNext } : episodeNext;

  const absolute = useCallback(() => {
    const v = videoRef.current;
    return v ? Math.round(offsetRef.current + v.currentTime * 1000) : 0;
  }, []);

  const save = useCallback((force = false) => {
    const now = Date.now();
    if (!force && now - lastSave.current < SAVE_EVERY_MS) return;
    lastSave.current = now;
    const pos = absolute();
    if (pos <= 0) return;
    void api.PUT("/items/{itemId}/progress", {
      params: { path: { itemId } },
      body: { positionMs: pos, durationMs: info?.durationMs },
    }).then(() => qc.invalidateQueries({ queryKey: ["continue"] }));
  }, [itemId, info?.durationMs, absolute, qc]);

  // Start (honours ?start= from "play from beginning"); again when moving to the next title.
  useEffect(() => {
    firstStart.current = true;
    setInfo(null);
    setSubtitle(-1);
    setAudioIndex(undefined);
    setEnded(false);
    setDismissed(false);
    setSkipped(null);
    setPosition(0);
    if (search.audio !== undefined) setAudioIndex(search.audio);
    // A bitmap track chosen on the title page is burned in by the server.
    start.mutate({ startMs: search.start, audioIndex: search.audio, subtitleIndex: search.sub });
  }, [itemId]); // eslint-disable-line react-hooks/exhaustive-deps

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
    video.playbackRate = speed;
    void video.play().catch(() => undefined); // autoplay may need a click
    return () => {
      cancelled = true;
    };
  }, [info]); // eslint-disable-line react-hooks/exhaustive-deps

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

  /** A burned-in subtitle stays in the picture when the stream restarts (seek, audio switch). */
  const burned = info?.burnedSubtitle ?? -1;

  const changeAudio = (idx: number) => {
    setAudioIndex(idx);
    save(true);
    start.mutate({ audioIndex: idx, startMs: absolute(), subtitleIndex: burned });
  };

  /** Text tracks switch in the player; bitmap tracks restart the stream with the track burned in. */
  const changeSubtitle = (value: string) => {
    const [kind, n] = [value.slice(0, 1), Number(value.slice(2))];
    if (kind === "b") {
      if (n === burned) return;
      setSubtitle(-1);
      save(true);
      start.mutate({ audioIndex, startMs: absolute(), subtitleIndex: n });
      return;
    }
    const text = kind === "t" ? n : -1;
    setSubtitle(text);
    if (burned >= 0) {
      save(true);
      start.mutate({ audioIndex, startMs: absolute(), subtitleIndex: -1 });
    }
  };
  const subtitleValue = burned >= 0 ? `b:${burned}` : subtitle >= 0 ? `t:${subtitle}` : "off";
  const hasSubtitles = !!info && (info.subtitles.length > 0 || (info.bitmapSubtitles?.length ?? 0) > 0);

  /** Seeks to an absolute position; a transcode restarts there when the target is not encoded yet. */
  const seekTo = useCallback((ms: number) => {
    const v = videoRef.current;
    if (!v || !info) return;
    // The seek bar gives fractions of a millisecond; the server takes whole ones.
    const target = Math.round(Math.max(0, Math.min(ms, durationMs || ms)));
    if (info.method === "direct") {
      v.currentTime = target / 1000;
    } else {
      const rel = (target - offsetRef.current) / 1000;
      const end = v.seekable.length ? v.seekable.end(v.seekable.length - 1) : 0;
      if (rel >= 0 && rel <= end) v.currentTime = rel;
      else start.mutate({ audioIndex, startMs: target, subtitleIndex: info.burnedSubtitle ?? -1 });
    }
    setPosition(target);
  }, [info, durationMs, audioIndex, start]);

  // Controls hide while playing and come back on any movement or key.
  const poke = useCallback(() => {
    setChrome(true);
    window.clearTimeout(hideTimer.current);
    hideTimer.current = window.setTimeout(() => {
      if (!videoRef.current?.paused && !panel) setChrome(false);
    }, HIDE_AFTER_MS);
  }, [panel]);
  useEffect(() => () => window.clearTimeout(hideTimer.current), []);

  const togglePlay = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) void v.play()?.catch(() => undefined);
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

  // Segments: skip intro/recap, and credits bring up the next title.
  const segAt = (kind: Segment["kind"]) => info?.segments?.find((s) => s.kind === kind && position >= s.startMs && position < s.endMs - 1000);
  const introMode = prefs.data?.skipIntro ?? "button";
  const creditsMode = prefs.data?.skipCredits ?? "button";
  const skippable = introMode === "off" ? undefined : segAt("intro") ?? segAt("recap");
  const inCredits = creditsMode !== "off" && !!segAt("credits");
  const autoplay = prefs.data?.autoplayNext ?? true;
  const back = (prefs.data?.skipBackSeconds ?? 10) * 1000;
  const forward = (prefs.data?.skipForwardSeconds ?? 10) * 1000;
  const upNext = prefs.data?.upNextSeconds ?? 10;
  // "Skip automatically": jump over the intro/recap once, as soon as it starts.
  useEffect(() => {
    if (introMode === "auto" && skippable && skipped !== skippable.kind + skippable.startMs) {
      setSkipped(skippable.kind + skippable.startMs);
      seekTo(skippable.endMs);
    }
  }, [introMode, skippable?.startMs]); // eslint-disable-line react-hooks/exhaustive-deps
  // Credits on "auto" go straight to the next title when there is one.
  useEffect(() => {
    if (creditsMode === "auto" && inCredits && next.data && autoplay) playNext();
  }, [creditsMode, inCredits]); // eslint-disable-line react-hooks/exhaustive-deps
  const remaining = durationMs ? durationMs - position : null;
  const showNext = !!next.data && !dismissed && (ended || inCredits || (remaining !== null && remaining <= 20_000 && position > 0));
  const [countdown, setCountdown] = useState(upNext);
  useEffect(() => {
    if (!showNext) {
      setCountdown(upNext);
      return;
    }
    if (!autoplay || upNext === 0) return;
    const timer = window.setInterval(() => setCountdown((c) => c - 1), 1000);
    return () => window.clearInterval(timer);
  }, [showNext, autoplay]);
  const playNext = useCallback(() => {
    if (!next.data) return;
    save(true);
    void navigate({ to: "/watch/$itemId", params: { itemId: next.data.id }, search: search.playlist ? { playlist: search.playlist } : {}, replace: true });
  }, [next.data, navigate, save, search.playlist]);
  useEffect(() => {
    if (showNext && autoplay && upNext > 0 && countdown <= 0) playNext();
  }, [countdown, showNext, autoplay, playNext]);

  // Keyboard and TV remote. Runs before the app's arrow-key navigation (capture phase).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const v = videoRef.current;
      if (!v || e.ctrlKey || e.metaKey || e.altKey) return;
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === "SELECT" || tag === "INPUT" || tag === "BUTTON" && (e.key === " " || e.key === "Enter")) return;
      let handled = true;
      switch (e.key) {
        case " ": case "k": case "K": case "MediaPlayPause": togglePlay(); break;
        case "ArrowLeft": case "j": case "J": case "MediaRewind": seekTo(absolute() - back); break;
        case "ArrowRight": case "l": case "L": case "MediaFastForward": seekTo(absolute() + forward); break;
        case "ArrowUp": v.volume = Math.min(1, v.volume + 0.1); v.muted = false; break;
        case "ArrowDown": v.volume = Math.max(0, v.volume - 0.1); break;
        case "m": case "M": v.muted = !v.muted; break;
        case "f": case "F": toggleFullscreen(); break;
        case "c": case "C": if (info?.subtitles.length) setSubtitle((s) => (s >= 0 ? -1 : (info.subtitleDefault ?? 0))); break;
        case "?": if (helpAllowed.current) setHelp((h) => !h); break;
        case "s": case "S": if (skippable) seekTo(skippable.endMs); else handled = false; break;
        case "n": case "N": if (next.data) playNext(); else handled = false; break;
        case ",": if (v.paused) v.currentTime = Math.max(0, v.currentTime - 1 / 24); break;
        case ".": if (v.paused) v.currentTime += 1 / 24; break;
        case "Escape": if (helpOpen.current) setHelp(false); else if (panel) setPanel(null); else handled = false; break;
        default: handled = false;
      }
      if (handled) {
        e.preventDefault();
        e.stopPropagation();
        poke();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [togglePlay, seekTo, absolute, toggleFullscreen, info, skippable, next.data, playNext, panel, poke]);

  const VolumeIcon = muted || volume === 0 ? VolumeX : volume < 0.5 ? Volume1 : Volume2;
  // Enhancements: pause screen, keyboard help, picture-in-picture when leaving the tab.
  const enh = useEnhancements();
  const [help, setHelp] = useState(false);
  helpAllowed.current = enh.shortcutsHelp;
  helpOpen.current = help;
  const [pauseScreen, setPauseScreen] = useState(false);
  useEffect(() => {
    setPauseScreen(false);
    if (!paused || ended || !enh.pauseScreen) return;
    const id = window.setTimeout(() => setPauseScreen(true), 4000);
    return () => window.clearTimeout(id);
  }, [paused, ended, enh.pauseScreen]);
  useEffect(() => {
    if (!enh.autoPip) return;
    const onVis = () => {
      const v = videoRef.current;
      if (document.visibilityState === "hidden" && v && !v.paused && !document.pictureInPictureElement) void v.requestPictureInPicture?.().catch(() => undefined);
    };
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
  }, [enh.autoPip]);
  const title = it ? (isEpisode && it.seasonNumber !== undefined && it.episodeNumber !== undefined
    ? `S${it.seasonNumber}:E${it.episodeNumber} · ${it.title}` : it.title) : "";
  const visible = chrome || paused || !!panel;

  return (
    <div ref={rootRef} className={`fixed inset-0 z-50 bg-black text-white ${visible ? "" : "cursor-none"}`} data-testid="player"
      onPointerMove={poke} onPointerDown={poke}>
      <style>{subtitleCss(prefs.data)}</style>
      <video ref={videoRef} className="absolute inset-0 size-full" playsInline autoPlay data-testid="video"
        onClick={togglePlay} onDoubleClick={toggleFullscreen}
        onTimeUpdate={(e) => {
          save();
          const v = e.currentTarget;
          setPosition(offsetRef.current + v.currentTime * 1000);
          if (v.buffered?.length) setBuffered(offsetRef.current + v.buffered.end(v.buffered.length - 1) * 1000);
        }}
        onPlay={() => { setPaused(false); setEnded(false); poke(); }}
        onPause={() => { setPaused(true); save(true); }}
        onWaiting={() => setWaiting(true)} onPlaying={() => setWaiting(false)} onCanPlay={() => setWaiting(false)}
        onVolumeChange={(e) => { setVolume(e.currentTarget.volume); setMuted(e.currentTarget.muted); }}
        onEnded={() => { save(true); setEnded(true); }}>
        {info?.subtitles.map((s) => (
          <track key={s.index} kind="subtitles" src={s.url} srcLang={s.language ?? "und"} label={s.language ?? String(s.index)} />
        ))}
      </video>

      {(start.isPending || waiting) && !start.error && (
        <p role="status" className="pointer-events-none absolute inset-0 flex items-center justify-center gap-3 text-white/80">
          <Spinner className="size-10" />{start.isPending && <span className="sr-only">{t("player.preparing")}</span>}
        </p>
      )}
      {start.error && <div className="absolute top-1/2 left-1/2 max-w-md -translate-x-1/2 -translate-y-1/2 rounded-card bg-surface p-4"><ErrorText error={start.error} /></div>}

      {/* Top bar */}
      <div className={`absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent px-4 pt-4 pb-10 transition-opacity duration-300 sm:px-8 ${visible ? "opacity-100" : "pointer-events-none opacity-0"}`}>
        <Link to="/item/$itemId" params={{ itemId }} className="inline-flex h-10 items-center gap-1.5 rounded-[6px] pr-4 pl-2.5 text-sm font-medium transition-colors hover:bg-white/10" onClick={() => save(true)}>
          <ArrowLeft className="size-5" aria-hidden />{t("player.back")}
        </Link>
        <span className="min-w-0 truncate text-lg font-semibold">{title}</span>
        {info && <span className="ml-auto hidden rounded bg-white/10 px-2 py-1 text-xs text-white/70 sm:inline" data-testid="play-method">{t(`player.method_${info.method}`)}</span>}
      </div>

      {pauseScreen && it && (
        <div className="pointer-events-none absolute inset-0 z-[5] flex items-end bg-gradient-to-r from-black/85 via-black/45 to-transparent p-8 pb-36 [animation:rise_500ms_var(--ease-out)_both] sm:p-14 sm:pb-40" data-testid="pause-screen">
          <div className="max-w-[40rem]">
            <p className="font-label text-white/60">{t("player.paused")}</p>
            <h2 className="mt-3 font-display text-5xl sm:text-7xl">{it.seriesTitle ?? it.title}</h2>
            {isEpisode && <p className="mt-3 text-xl font-semibold text-white/85">{title}</p>}
            <p className="mt-3 text-[15px] text-white/60 tabular">{[it.year, durationMs ? formatDuration(durationMs) : null].filter(Boolean).join(" · ")}</p>
            {detail.data?.overview && <p className="mt-4 line-clamp-4 text-[17px] leading-relaxed text-white/80">{detail.data.overview}</p>}
          </div>
        </div>
      )}
      {help && (
        <div role="dialog" aria-label={t("player.shortcuts")} className="absolute inset-0 z-40 grid place-items-center bg-black/70 backdrop-blur-sm" onClick={() => setHelp(false)} data-testid="shortcuts-help">
          <div className="w-[min(92vw,520px)] rounded-[8px] bg-surface p-6 ring-1 ring-white/10" onClick={(e) => e.stopPropagation()}>
            <h2 className="mb-4 font-head text-xl">{t("player.shortcuts")}</h2>
            <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5 text-[15px]">
              {([["Space / K", "player.keyPlay"], ["← / J", "player.keyBack"], ["→ / L", "player.keyForward"], ["↑ ↓", "player.keyVolume"], ["M", "player.keyMute"],
                ["F", "player.keyFullscreen"], ["C", "player.keySubtitles"], ["?", "player.keyHelp"], ["Esc", "player.keyClose"]] as const).map(([k, label]) => (
                <div key={k} className="contents">
                  <dt><kbd className="rounded-[4px] bg-white/10 px-2 py-0.5 font-mono text-[13px]">{k}</kbd></dt>
                  <dd className="text-text/80">{t(label)}</dd>
                </div>
              ))}
            </dl>
          </div>
        </div>
      )}
      {notice && (
        <div role="status" data-testid="admin-message"
          className="absolute top-20 left-1/2 z-30 flex w-[min(92vw,560px)] -translate-x-1/2 items-start gap-3 rounded-[8px] bg-black/85 p-4 text-white shadow-2xl ring-1 ring-white/15 backdrop-blur [animation:rise_300ms_var(--ease-out)_both]">
          <MessageSquare className="mt-0.5 size-5 shrink-0" aria-hidden />
          <p className="flex-1 text-[15px] leading-snug">{notice}</p>
          <button type="button" onClick={() => setNotice(null)} aria-label={t("common.close")} className="grid size-7 place-items-center rounded-[4px] hover:bg-white/15"><X className="size-4" aria-hidden /></button>
        </div>
      )}

      {/* Skip intro / recap */}
      {skippable && skipped !== skippable.kind + skippable.startMs && (
        <button type="button" data-testid="skip-segment"
          onClick={() => { setSkipped(skippable.kind + skippable.startMs); seekTo(skippable.endMs); }}
          className="tv-focus absolute right-6 bottom-28 z-10 inline-flex h-12 items-center gap-2 rounded-md border border-white/60 bg-black/60 px-5 text-base font-semibold backdrop-blur transition-colors hover:bg-white hover:text-black sm:right-10">
          <SkipForward className="size-5" aria-hidden />{t(skippable.kind === "recap" ? "player.skipRecap" : "player.skipIntro")}
        </button>
      )}

      {/* Up next */}
      {showNext && next.data && (
        <div className="absolute right-4 bottom-28 z-10 flex w-[min(92vw,380px)] gap-3 rounded-lg bg-black/85 p-3 text-white shadow-2xl ring-1 ring-white/15 backdrop-blur [animation:rise_300ms_var(--ease-out)_both] sm:right-8"
          role="dialog" aria-label={t("player.upNext")} data-testid="up-next">
          <div className="relative aspect-video w-32 shrink-0 overflow-hidden rounded-md bg-white/10">
            {next.data.hasPoster && <img src={imageUrl(next.data.id, next.data.kind === "episode" ? "still" : "poster")} alt="" className="absolute inset-0 size-full object-cover" />}
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <p className="text-xs text-white/60">{autoplay && upNext > 0 ? t(search.playlist ? "player.nextItemIn" : "player.nextIn", { seconds: Math.max(0, countdown) }) : t("player.upNext")}</p>
            <p className="truncate text-sm font-semibold">
              {next.data.seasonNumber !== undefined && next.data.episodeNumber !== undefined ? `S${next.data.seasonNumber}:E${next.data.episodeNumber} · ` : ""}{next.data.title}
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

      {/* Bottom controls */}
      <div className={`absolute inset-x-0 bottom-0 flex flex-col gap-1 bg-gradient-to-t from-black/90 via-black/50 to-transparent px-4 pt-16 pb-4 transition-opacity duration-300 sm:px-8 ${visible ? "opacity-100" : "pointer-events-none opacity-0"}`}>
        <div className="flex items-center gap-3">
          <div className="flex-1">
            <SeekBar positionMs={position} durationMs={durationMs} bufferedMs={buffered} info={info} onSeek={seekTo} />
          </div>
          <span className="w-24 text-right text-sm text-white/80 tabular">-{clock(Math.max(0, durationMs - position))}</span>
        </div>
        <div className="flex items-center gap-1">
          <button type="button" className={iconBtn} onClick={togglePlay} aria-label={paused ? t("player.play") : t("player.pause")}>
            {paused ? <Play className="size-6 fill-current" aria-hidden /> : <Pause className="size-6 fill-current" aria-hidden />}
          </button>
          <button type="button" className={iconBtn} onClick={() => seekTo(absolute() - back)} aria-label={t("player.backSeconds", { seconds: back / 1000 })}><RotateCcw className="size-5" aria-hidden /></button>
          <button type="button" className={iconBtn} onClick={() => seekTo(absolute() + forward)} aria-label={t("player.forwardSeconds", { seconds: forward / 1000 })}><RotateCw className="size-5" aria-hidden /></button>
          <div className="group/vol hidden items-center sm:flex">
            <button type="button" className={iconBtn} aria-label={muted ? t("player.unmute") : t("player.mute")}
              onClick={() => { const v = videoRef.current; if (v) v.muted = !v.muted; }}><VolumeIcon className="size-5" aria-hidden /></button>
            <input type="range" min={0} max={1} step={0.05} value={muted ? 0 : volume} aria-label={t("player.volume")}
              onChange={(e) => { const v = videoRef.current; if (v) { v.volume = Number(e.target.value); v.muted = false; } }}
              className="w-0 accent-white opacity-0 transition-[width,opacity] duration-200 group-hover/vol:w-24 group-hover/vol:opacity-100 focus:w-24 focus:opacity-100" />
          </div>
          <span className="ml-2 hidden text-sm text-white/80 tabular sm:inline">{clock(position)} / {clock(durationMs)}</span>
          <div className="ml-auto flex items-center gap-1">
            {next.data && (
              <button type="button" className={iconBtn} onClick={playNext} aria-label={t("player.upNext")}><SkipForward className="size-5" aria-hidden /></button>
            )}
            {info && (hasSubtitles || info.audio.length > 1) && (
              <button type="button" className={iconBtn} aria-expanded={panel === "tracks"} aria-label={t("player.tracks")}
                onClick={() => setPanel(panel === "tracks" ? null : "tracks")}><Captions className="size-5" aria-hidden /></button>
            )}
            <button type="button" className={iconBtn} aria-expanded={panel === "speed"} aria-label={t("player.speed")}
              onClick={() => setPanel(panel === "speed" ? null : "speed")}><Gauge className="size-5" aria-hidden /></button>
            {typeof document !== "undefined" && "pictureInPictureEnabled" in document && (
              <button type="button" className={iconBtn} aria-label={t("player.pip")}
                onClick={() => { const v = videoRef.current; if (!v) return; if (document.pictureInPictureElement) void document.exitPictureInPicture(); else void v.requestPictureInPicture?.().catch(() => undefined); }}>
                <PictureInPicture2 className="size-5" aria-hidden />
              </button>
            )}
            <button type="button" className={iconBtn} onClick={toggleFullscreen} aria-label={fullscreen ? t("player.exitFullscreen") : t("player.fullscreen")}>
              {fullscreen ? <Minimize className="size-5" aria-hidden /> : <Maximize className="size-5" aria-hidden />}
            </button>
          </div>
        </div>
      </div>

      {/* Audio & subtitles panel (kept in the DOM so labels stay addressable) */}
      <div hidden={panel !== "tracks"} className="absolute right-4 bottom-24 z-20 w-[min(92vw,520px)] rounded-lg bg-black/90 p-5 shadow-2xl ring-1 ring-white/15 backdrop-blur sm:right-8">
        <div className="grid gap-5 sm:grid-cols-2">
          {info && info.audio.length > 1 && (
            <label className="flex flex-col gap-2 text-sm">
              <span className="font-semibold">{t("library.audio")}</span>
              <select className="h-10 rounded-md bg-white/10 px-3 ring-1 ring-white/15" value={audioIndex ?? info.audioIndex ?? 0}
                onChange={(e) => changeAudio(Number(e.target.value))} aria-label={t("library.audio")}>
                {info.audio.map((a, i) => (
                  <option key={i} value={i} className="text-black">
                    {[a.language?.toUpperCase() ?? `#${i + 1}`, a.codec.toUpperCase(), a.title].filter(Boolean).join(" ")}
                  </option>
                ))}
              </select>
            </label>
          )}
          {info && hasSubtitles && (
            <label className="flex flex-col gap-2 text-sm">
              <span className="font-semibold">{t("library.subtitles")}</span>
              <select className="h-10 rounded-md bg-white/10 px-3 ring-1 ring-white/15" value={subtitleValue} onChange={(e) => changeSubtitle(e.target.value)}
                aria-label={t("library.subtitles")} disabled={start.isPending}>
                <option value="off" className="text-black">{t("player.off")}</option>
                {info.subtitles.map((s, i) => (
                  <option key={s.index} value={`t:${i}`} className="text-black">
                    {(s.language?.toUpperCase() ?? `#${i + 1}`) + (s.forced ? ` (${t("library.forced")})` : "")}
                  </option>
                ))}
                {info.bitmapSubtitles?.map((s) => (
                  <option key={s.index} value={`b:${s.index}`} className="text-black">
                    {`${s.language?.toUpperCase() ?? `#${s.index + 1}`} · ${bitmapName(s.codec)}` + (s.forced ? ` (${t("library.forced")})` : "") +
                      ` · ${t("player.burnedIn")}`}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
      </div>
      <div hidden={panel !== "speed"} className="absolute right-4 bottom-24 z-20 rounded-lg bg-black/90 p-2 shadow-2xl ring-1 ring-white/15 backdrop-blur sm:right-8" role="menu" aria-label={t("player.speed")}>
        {SPEEDS.map((s) => (
          <button key={s} type="button" role="menuitemradio" aria-checked={speed === s}
            onClick={() => { setSpeed(s); if (videoRef.current) videoRef.current.playbackRate = s; setPanel(null); }}
            className={`block w-full rounded px-4 py-2 text-left text-sm tabular hover:bg-white/10 ${speed === s ? "font-bold text-accent" : ""}`}>
            {s === 1 ? t("player.normalSpeed") : `${s}×`}
          </button>
        ))}
      </div>
    </div>
  );
}
