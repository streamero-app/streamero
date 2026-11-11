import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, unwrap } from "@/api/client";
import { useMe } from "@/features/auth/useAuth";
import { ClockSync, isGroup, type SyncClientMessage, type SyncCorrection, type SyncGroup, type SyncServerMessage } from "./engine";

type CorrectionListener = (c: SyncCorrection, serverTime: number) => void;

type SyncPlayContext = {
  /** Watch-together is allowed for this account. */
  allowed: boolean;
  group: SyncGroup | null;
  connected: boolean;
  clock: ClockSync;
  /** The last refused command (error code), cleared after a while. */
  error: string | null;
  send: (m: SyncClientMessage) => void;
  create: (itemIds: string[], startMs?: number) => Promise<SyncGroup>;
  join: (groupId: string) => Promise<SyncGroup>;
  leave: () => Promise<void>;
  onCorrection: (fn: CorrectionListener) => () => void;
};

const Ctx = createContext<SyncPlayContext | null>(null);

const PING_EVERY_MS = 5000;

function socketUrl(): string {
  const loc = globalThis.location;
  return `${loc.protocol === "https:" ? "wss" : "ws"}://${loc.host}/api/v1/syncplay/socket`;
}

/**
 * Keeps the watch-together socket for the signed-in session while it is in a
 * group (D-036). Membership belongs to the session, so after a reload the
 * provider finds the group again and reconnects.
 */
export function SyncPlayProvider({ children }: { children: ReactNode }) {
  const { data: me } = useMe();
  const allowed = !!me && (me.isAdmin || me.permissions.syncPlay);
  const [group, setGroup] = useState<SyncGroup | null>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const clock = useMemo(() => new ClockSync(), []);
  const ws = useRef<WebSocket | null>(null);
  const want = useRef(false); // stay connected (in a group)
  const retry = useRef(0);
  const timers = useRef<{ ping?: number; reconnect?: number }>({});
  const listeners = useRef(new Set<CorrectionListener>());

  const disconnect = useCallback(() => {
    want.current = false;
    window.clearInterval(timers.current.ping);
    window.clearTimeout(timers.current.reconnect);
    const s = ws.current;
    ws.current = null;
    if (s && s.readyState <= 1) s.close();
    setConnected(false);
  }, []);

  const connect = useCallback(() => {
    want.current = true;
    if (ws.current && ws.current.readyState <= 1) return;
    let s: WebSocket;
    try {
      s = new WebSocket(socketUrl());
    } catch {
      return;
    }
    ws.current = s;
    const ping = () => {
      if (s.readyState === 1) s.send(JSON.stringify({ type: "ping", clientTime: Date.now() }));
    };
    s.onopen = () => {
      retry.current = 0;
      setConnected(true);
      ping();
      window.clearInterval(timers.current.ping);
      timers.current.ping = window.setInterval(ping, PING_EVERY_MS);
    };
    s.onmessage = (ev) => {
      let m: SyncServerMessage;
      try {
        m = JSON.parse(String(ev.data)) as SyncServerMessage;
      } catch {
        return;
      }
      switch (m.type) {
        case "pong":
          if (m.clientTime) clock.add(m.clientTime, Date.now(), m.serverTime);
          break;
        case "state":
          if (m.group && isGroup(m.group)) setGroup(m.group);
          else {
            setGroup(null);
            disconnect();
          }
          break;
        case "correction":
          if (m.correction) for (const fn of listeners.current) fn(m.correction, m.serverTime);
          break;
        case "error":
          setError(m.code ?? "invalid");
          break;
      }
    };
    s.onclose = () => {
      if (ws.current !== s) return;
      ws.current = null;
      setConnected(false);
      window.clearInterval(timers.current.ping);
      if (!want.current) return;
      // Back off 1, 2, 4 … 15 s; the server keeps the membership meanwhile.
      const delay = Math.min(15_000, 1000 * 2 ** retry.current++);
      timers.current.reconnect = window.setTimeout(connect, delay);
    };
  }, [clock, disconnect]);

  // After a reload: is this session still in a group?
  useEffect(() => {
    if (!allowed) return;
    let cancelled = false;
    void api.GET("/syncplay/me").then((res) => {
      if (cancelled || res.response.status !== 200 || !isGroup(res.data)) return;
      setGroup(res.data);
      connect();
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, [allowed, connect]);
  useEffect(() => () => disconnect(), [disconnect]);
  useEffect(() => {
    if (!error) return;
    const id = window.setTimeout(() => setError(null), 4000);
    return () => window.clearTimeout(id);
  }, [error]);

  const send = useCallback((m: SyncClientMessage) => {
    const s = ws.current;
    if (s && s.readyState === 1) s.send(JSON.stringify(m));
  }, []);
  const create = useCallback(async (itemIds: string[], startMs?: number) => {
    const g = unwrap(await api.POST("/syncplay/groups", { body: { itemIds, startMs: startMs === undefined ? undefined : Math.max(0, Math.round(startMs)) } }));
    setGroup(g);
    connect();
    return g;
  }, [connect]);
  const join = useCallback(async (groupId: string) => {
    const g = unwrap(await api.POST("/syncplay/groups/{groupId}/join", { params: { path: { groupId } } }));
    setGroup(g);
    connect();
    return g;
  }, [connect]);
  const leave = useCallback(async () => {
    disconnect();
    setGroup(null);
    await api.POST("/syncplay/leave");
  }, [disconnect]);
  const onCorrection = useCallback((fn: CorrectionListener) => {
    listeners.current.add(fn);
    return () => { listeners.current.delete(fn); };
  }, []);

  const value = useMemo(() => ({ allowed, group, connected, clock, error, send, create, join, leave, onCorrection }),
    [allowed, group, connected, clock, error, send, create, join, leave, onCorrection]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

const outside: SyncPlayContext = {
  allowed: false, group: null, connected: false, clock: new ClockSync(), error: null,
  send: () => undefined, create: () => Promise.reject(new Error("no provider")), join: () => Promise.reject(new Error("no provider")),
  leave: () => Promise.resolve(), onCorrection: () => () => undefined,
};

export function useSyncPlay(): SyncPlayContext {
  return useContext(Ctx) ?? outside;
}
