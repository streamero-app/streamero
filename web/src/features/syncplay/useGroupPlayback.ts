import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { currentEntry, plan } from "./engine";
import { useSyncPlay } from "./SyncPlay";

const REPORT_EVERY_MS = 1000;

function clockText(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

/**
 * Makes the player follow its watch-together group: local controls become
 * group commands, the group's state drives the <video>, and the player reports
 * readiness, buffering and its position (for the server's drift correction).
 */
export function useGroupPlayback({ video, itemId, loaded, absolute, seekLocal }: {
  video: RefObject<HTMLVideoElement | null>;
  itemId: string;
  /** The stream is attached (not being prepared). */
  loaded: boolean;
  absolute: () => number;
  seekLocal: (ms: number) => void;
}) {
  const { t } = useTranslation();
  const sync = useSyncPlay();
  const navigate = useNavigate();
  const g = sync.group;
  const entry = currentEntry(g);
  const active = !!g && !!entry && entry.itemId === itemId;
  const startTimer = useRef<number | undefined>(undefined);
  const rateTimer = useRef<number | undefined>(undefined);
  const correcting = useRef(false);
  const readySent = useRef<string>("");
  const groupRef = useRef(g);
  groupRef.current = g;
  const [notice, setNotice] = useState<string | null>(null);

  // The group moved to another title: open it.
  useEffect(() => {
    if (g && entry && entry.itemId !== itemId) {
      void navigate({ to: "/watch/$itemId", params: { itemId: entry.itemId }, search: { together: 1 }, replace: true });
    }
  }, [g?.version, entry?.itemId, itemId]); // eslint-disable-line react-hooks/exhaustive-deps

  /** Sends "ready" when the group waits for this player and it can play. */
  const checkReady = useCallback(() => {
    const v = video.current;
    const cur = groupRef.current;
    const e = currentEntry(cur);
    if (!v || !cur || !e || e.itemId !== itemId) return;
    const p = plan(cur, { itemId, positionMs: absolute(), canPlay: loaded && v.readyState >= 3, seeking: v.seeking }, sync.clock.serverNow());
    const key = `${cur.version}:${e.id}`;
    if (p.sendReady && readySent.current !== key) {
      readySent.current = key;
      sync.send({ type: "ready", entryId: e.id, positionMs: Math.round(absolute()), at: Math.round(sync.clock.serverNow()) });
    }
  }, [video, itemId, loaded, absolute, sync]);

  // Follow every new group state.
  useEffect(() => {
    const v = video.current;
    if (!active || !g || !v || !loaded) return;
    const now = sync.clock.serverNow();
    const p = plan(g, { itemId, positionMs: absolute(), canPlay: v.readyState >= 3, seeking: v.seeking }, now);
    if (p.seekTo !== undefined) seekLocal(p.seekTo);
    if (!correcting.current) v.playbackRate = p.rate;
    window.clearTimeout(startTimer.current);
    if (p.startAt !== undefined) {
      if (!v.paused) v.pause();
      startTimer.current = window.setTimeout(() => void v.play()?.catch(() => undefined), Math.max(0, p.startAt - now));
    } else if (p.play && v.paused) {
      void v.play()?.catch(() => undefined);
    } else if (!p.play && !v.paused) {
      v.pause();
    }
    checkReady();
  }, [g?.version, active, loaded]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => { window.clearTimeout(startTimer.current); window.clearTimeout(rateTimer.current); }, []);

  // Readiness after seeks and loads, and position reports while playing.
  useEffect(() => {
    if (!active) return;
    const id = window.setInterval(() => {
      checkReady();
      const v = video.current;
      const cur = groupRef.current;
      const e = currentEntry(cur);
      const me = cur?.members.find((m) => m.id === cur.selfId);
      const now = sync.clock.serverNow();
      if (v && cur && e && me?.ready && cur.state === "playing" && !v.paused && !v.seeking && now > cur.anchorAt && sync.clock.ready) {
        sync.send({ type: "position", entryId: e.id, positionMs: Math.round(absolute()), at: Math.round(now), playing: true });
      }
    }, REPORT_EVERY_MS);
    return () => window.clearInterval(id);
  }, [active, checkReady, absolute, sync, video]);

  // Stalls and the end of the title.
  useEffect(() => {
    const v = video.current;
    if (!active || !v) return;
    const onWaiting = () => {
      const cur = groupRef.current;
      const e = currentEntry(cur);
      const me = cur?.members.find((m) => m.id === cur.selfId);
      if (cur && e && me?.ready && cur.state === "playing" && !v.seeking) {
        sync.send({ type: "buffering", entryId: e.id, positionMs: Math.round(absolute()), at: Math.round(sync.clock.serverNow()) });
      }
    };
    const onEnded = () => {
      const e = currentEntry(groupRef.current);
      if (e) sync.send({ type: "next", entryId: e.id });
    };
    v.addEventListener("waiting", onWaiting);
    v.addEventListener("ended", onEnded);
    return () => { v.removeEventListener("waiting", onWaiting); v.removeEventListener("ended", onEnded); };
  }, [active, video, sync, absolute]);

  // Drift corrections from the server.
  useEffect(() => {
    if (!active) return;
    return sync.onCorrection((c, serverTime) => {
      const v = video.current;
      const cur = groupRef.current;
      if (!v || !cur) return;
      if (c.mode === "rate" && c.rate && c.durationMs) {
        correcting.current = true;
        v.playbackRate = c.rate;
        window.clearTimeout(rateTimer.current);
        rateTimer.current = window.setTimeout(() => {
          correcting.current = false;
          v.playbackRate = groupRef.current?.speed ?? 1;
        }, c.durationMs);
      } else if (c.mode === "seek" && c.positionMs !== undefined) {
        seekLocal(c.positionMs + Math.max(0, sync.clock.serverNow() - serverTime) * cur.speed);
      }
    });
  }, [active, sync, video, seekLocal]);

  // What others did, as a short notice.
  const lastVersion = useRef(0);
  useEffect(() => {
    if (!g || g.version === lastVersion.current) return;
    const first = lastVersion.current === 0;
    lastVersion.current = g.version;
    const me = g.members.find((m) => m.id === g.selfId);
    if (first || !g.actor || g.actor === me?.name) return;
    const name = g.actor;
    const text: Record<string, string> = {
      play: t("together.action.play", { name }), pause: t("together.action.pause", { name }),
      seek: t("together.action.seek", { name, time: clockText(g.positionMs) }), speed: t("together.action.speed", { name, speed: g.speed }),
      join: t("together.action.join", { name }), leave: t("together.action.leave", { name }), next: t("together.action.next", { name }),
    };
    if (g.reason && text[g.reason]) setNotice(text[g.reason]!);
  }, [g?.version]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!notice) return;
    const id = window.setTimeout(() => setNotice(null), 3500);
    return () => window.clearTimeout(id);
  }, [notice]);

  const send = sync.send;
  const play = useCallback(() => send({ type: "play" }), [send]);
  const pause = useCallback(() => send({ type: "pause" }), [send]);
  const seek = useCallback((ms: number) => send({ type: "seek", positionMs: Math.max(0, Math.round(ms)) }), [send]);
  const setSpeed = useCallback((speed: number) => send({ type: "speed", speed }), [send]);
  const next = useCallback(() => {
    const e = currentEntry(groupRef.current);
    if (e) send({ type: "next", entryId: e.id });
  }, [send]);
  const shown = active ? g : null;
  const error = active ? sync.error : null;
  return useMemo(() => ({ active, group: shown, notice, error, play, pause, seek, setSpeed, next }),
    [active, shown, notice, error, play, pause, seek, setSpeed, next]);
}
