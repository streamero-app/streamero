import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type Hls from "hls.js";
import { api, unwrap } from "@/api/client";
import { imageUrl, type ItemSummary } from "@/features/library/format";
import { detectAudioProfile } from "@/features/player/deviceProfile";
import { sleepDue, type SleepTimer } from "@/features/books/logic";

/**
 * One song in the queue (a track item and where its cover comes from). Audiobook
 * parts are queue entries too: itemId is the book, fileId the part, offsetMs where
 * the part starts in the book, and startMs where playback of the part begins.
 */
export type QueueTrack = ItemSummary & {
  coverId?: string;
  itemId?: string;
  fileId?: string;
  offsetMs?: number;
  startMs?: number;
  bookDurationMs?: number;
};

type Ctx = {
  queue: QueueTrack[];
  index: number;
  current?: QueueTrack;
  playing: boolean;
  position: number; // ms
  duration: number; // ms
  volume: number;
  shuffle: boolean;
  repeat: "off" | "all" | "one";
  play: (tracks: QueueTrack[], start?: number) => void;
  enqueue: (tracks: QueueTrack[]) => void;
  toggle: () => void;
  next: () => void;
  prev: () => void;
  jump: (i: number) => void;
  seek: (ms: number) => void;
  setVolume: (v: number) => void;
  setShuffle: (v: boolean) => void;
  setRepeat: (v: "off" | "all" | "one") => void;
  remove: (i: number) => void;
  stop: () => void;
  /** Playback speed (audiobooks; kept for songs too). */
  rate: number;
  setRate: (r: number) => void;
  sleep: SleepTimer;
  setSleep: (s: SleepTimer) => void;
};

const MusicContext = createContext<Ctx | null>(null);

export function useMusic(): Ctx {
  const ctx = useContext(MusicContext);
  if (!ctx) throw new Error("useMusic outside MusicProvider");
  return ctx;
}

/** The cover of a track: its own picture, else its album's. */
export function coverUrl(t: QueueTrack): string | null {
  if (t.coverId) return imageUrl(t.coverId, "poster");
  return t.hasPoster ? imageUrl(t.id, "poster") : null;
}

function shuffled<T>(list: T[], first: number): T[] {
  const rest = list.filter((_, i) => i !== first);
  for (let i = rest.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [rest[i], rest[j]] = [rest[j]!, rest[i]!];
  }
  return [list[first]!, ...rest];
}

/**
 * Music playback for the whole app: one <audio> element that keeps playing while
 * the viewer browses, a queue, and lock-screen / media-key controls.
 */
export function MusicProvider({ children }: { children: ReactNode }) {
  const audio = useRef<HTMLAudioElement | null>(null);
  const hls = useRef<Hls | null>(null);
  const [queue, setQueue] = useState<QueueTrack[]>([]);
  const [index, setIndex] = useState(-1);
  const [playing, setPlaying] = useState(false);
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [volume, setVol] = useState(() => {
    try { return Number(localStorage.getItem("music-volume") ?? "1") || 1; } catch { return 1; }
  });
  const [shuffle, setShuffle] = useState(false);
  const [repeat, setRepeat] = useState<"off" | "all" | "one">("off");
  const [rate, setRateState] = useState(() => {
    try { return Number(localStorage.getItem("audio-rate") ?? "1") || 1; } catch { return 1; }
  });
  const [sleep, setSleep] = useState<SleepTimer>({ kind: "off" });
  const current = index >= 0 ? queue[index] : undefined;

  if (typeof document !== "undefined" && !audio.current) {
    audio.current = document.createElement("audio");
    audio.current.preload = "auto";
  }

  // Load the current track: ask the server how to play it (direct file, or HLS when this browser cannot).
  useEffect(() => {
    const el = audio.current;
    if (!el || !current) return;
    let cancelled = false;
    hls.current?.destroy();
    hls.current = null;
    void (async () => {
      try {
        const info = unwrap(await api.POST("/items/{itemId}/playback", {
          params: { path: { itemId: current.itemId ?? current.id } },
          body: { device: detectAudioProfile(), fileId: current.fileId, startMs: current.fileId ? 0 : undefined },
        }));
        if (cancelled) return;
        setDuration(info.durationMs ?? current.durationMs ?? 0);
        if (info.method === "direct" || el.canPlayType("application/vnd.apple.mpegurl")) {
          el.src = info.url;
        } else {
          const { default: HlsCtor } = await import("hls.js");
          if (cancelled) return;
          const h = new HlsCtor();
          h.loadSource(info.url);
          h.attachMedia(el);
          hls.current = h;
        }
        el.volume = volume;
        el.defaultPlaybackRate = rate;
        el.playbackRate = rate;
        if (current.startMs) {
          const at = current.startMs / 1000;
          el.addEventListener("loadedmetadata", () => { el.currentTime = at; el.playbackRate = rate; }, { once: true });
        }
        await el.play().catch(() => setPlaying(false));
      } catch {
        if (!cancelled) setPlaying(false);
      }
    })();
    return () => { cancelled = true; };
  }, [current?.id, index]); // eslint-disable-line react-hooks/exhaustive-deps

  const advance = useCallback((dir: 1 | -1, auto = false) => {
    setIndex((i) => {
      if (auto && repeat === "one") {
        if (audio.current) { audio.current.currentTime = 0; void audio.current.play(); }
        return i;
      }
      const n = i + dir;
      if (n >= queue.length) return repeat === "all" ? 0 : auto ? i : i;
      if (n < 0) return 0;
      return n;
    });
  }, [queue.length, repeat]);

  useEffect(() => {
    const el = audio.current;
    if (!el) return;
    const onTime = () => setPosition(el.currentTime * 1000);
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onMeta = () => { if (Number.isFinite(el.duration)) setDuration(el.duration * 1000); };
    const onEnded = () => {
      if (repeat === "one") { el.currentTime = 0; void el.play(); return; }
      if (index + 1 < queue.length) setIndex(index + 1);
      else if (repeat === "all" && queue.length) setIndex(0);
      else setPlaying(false);
    };
    el.addEventListener("timeupdate", onTime);
    el.addEventListener("play", onPlay);
    el.addEventListener("pause", onPause);
    el.addEventListener("loadedmetadata", onMeta);
    el.addEventListener("ended", onEnded);
    return () => {
      el.removeEventListener("timeupdate", onTime);
      el.removeEventListener("play", onPlay);
      el.removeEventListener("pause", onPause);
      el.removeEventListener("loadedmetadata", onMeta);
      el.removeEventListener("ended", onEnded);
    };
  }, [index, queue.length, repeat]);

  const prev = useCallback(() => {
    const el = audio.current;
    // Like every music player: the first press restarts the song.
    if (el && el.currentTime > 3) { el.currentTime = 0; return; }
    advance(-1);
  }, [advance]);
  const next = useCallback(() => advance(1), [advance]);
  const toggle = useCallback(() => {
    const el = audio.current;
    if (!el || !current) return;
    if (el.paused) void el.play(); else el.pause();
  }, [current]);
  const seek = useCallback((ms: number) => { if (audio.current) audio.current.currentTime = Math.max(0, Math.round(ms) / 1000); }, []);

  // Lock screen, headphones and keyboard media keys.
  useEffect(() => {
    const ms = typeof navigator !== "undefined" ? navigator.mediaSession : undefined;
    if (!ms || !current) return;
    const art = coverUrl(current);
    ms.metadata = new MediaMetadata({ title: current.title, artist: current.artist ?? "", album: current.album ?? "",
      artwork: art ? [{ src: art, sizes: "512x512", type: "image/jpeg" }] : [] });
    ms.setActionHandler("play", () => void audio.current?.play());
    ms.setActionHandler("pause", () => audio.current?.pause());
    ms.setActionHandler("previoustrack", prev);
    ms.setActionHandler("nexttrack", next);
    ms.setActionHandler("seekto", (d) => { if (d.seekTime !== undefined) seek(d.seekTime * 1000); });
  }, [current, prev, next, seek]);

  // Audiobooks: save the position in the whole book every 15 s and on pause.
  const bookPos = useRef<{ itemId: string; ms: number; total?: number } | null>(null);
  useEffect(() => {
    if (current?.itemId && current.fileId) {
      bookPos.current = { itemId: current.itemId, ms: (current.offsetMs ?? 0) + position, total: current.bookDurationMs };
    }
  }, [current, position]);
  useEffect(() => {
    const save = () => {
      const b = bookPos.current;
      if (!b || b.ms <= 0) return;
      void api.PUT("/items/{itemId}/progress", { params: { path: { itemId: b.itemId } },
        body: { positionMs: Math.round(b.ms), durationMs: b.total } });
    };
    if (!playing) { save(); return; }
    const id = window.setInterval(save, 15000);
    return () => window.clearInterval(id);
  }, [playing, current?.itemId]);

  // Sleep timer: pause at a time or at the end of the chapter.
  useEffect(() => {
    if (sleep.kind === "off" || !playing) return;
    const id = window.setInterval(() => {
      const ms = (current?.offsetMs ?? 0) + (audio.current?.currentTime ?? 0) * 1000;
      if (sleepDue(sleep, Date.now(), ms)) {
        audio.current?.pause();
        setSleep({ kind: "off" });
      }
    }, 500);
    return () => window.clearInterval(id);
  }, [sleep, playing, current?.offsetMs]);

  const setRate = useCallback((r: number) => {
    setRateState(r);
    if (audio.current) { audio.current.playbackRate = r; audio.current.defaultPlaybackRate = r; }
    try { localStorage.setItem("audio-rate", String(r)); } catch { /* private mode */ }
  }, []);

  const value = useMemo<Ctx>(() => ({
    queue, index, current, playing, position, duration, volume, shuffle, repeat, rate, setRate, sleep, setSleep,
    play: (tracks, start = 0) => {
      if (!tracks.length) return;
      if (tracks[start]?.id === current?.id && audio.current) {
        // The same song again: start it over instead of reloading.
        audio.current.currentTime = 0;
        void audio.current.play();
      }
      const list = shuffle ? shuffled(tracks, start) : tracks;
      setQueue(list);
      setIndex(shuffle ? 0 : start);
      setPosition(0);
    },
    enqueue: (tracks) => {
      setQueue((q) => [...q, ...tracks]);
      setIndex((i) => (i < 0 ? 0 : i));
    },
    toggle, next, prev, seek,
    jump: (i) => setIndex(i),
    setVolume: (v) => {
      setVol(v);
      if (audio.current) audio.current.volume = v;
      try { localStorage.setItem("music-volume", String(v)); } catch { /* private mode */ }
    },
    setShuffle: (v) => {
      setShuffle(v);
      if (v && index >= 0) { setQueue((q) => shuffled(q, index)); setIndex(0); }
    },
    setRepeat,
    remove: (i) => {
      setQueue((q) => q.filter((_, k) => k !== i));
      setIndex((cur) => (i < cur ? cur - 1 : cur));
    },
    stop: () => {
      audio.current?.pause();
      hls.current?.destroy();
      hls.current = null;
      setQueue([]);
      setIndex(-1);
      setPlaying(false);
    },
  }), [queue, index, current, playing, position, duration, volume, shuffle, repeat, rate, setRate, sleep, toggle, next, prev, seek]);

  return <MusicContext.Provider value={value}>{children}</MusicContext.Provider>;
}
