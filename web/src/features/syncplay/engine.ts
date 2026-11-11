import type { components } from "@/api/schema.gen";

export type SyncGroup = components["schemas"]["SyncGroup"];
export type SyncMember = components["schemas"]["SyncMember"];
export type SyncEntry = components["schemas"]["SyncQueueEntry"];
export type SyncClientMessage = components["schemas"]["SyncClientMessage"];
export type SyncServerMessage = components["schemas"]["SyncServerMessage"];
export type SyncCorrection = components["schemas"]["SyncCorrection"];

/** The entry the group is on, if any. */
export function currentEntry(g: SyncGroup | null | undefined): SyncEntry | undefined {
  return g && g.index >= 0 ? g.queue[g.index] : undefined;
}

/** The shared position at server time `serverNow` (ms). */
export function sharedPosition(g: SyncGroup, serverNow: number): number {
  if (g.state !== "playing" || serverNow <= g.anchorAt) return g.positionMs;
  return g.positionMs + (serverNow - g.anchorAt) * g.speed;
}

/** Whether a value is a group (guards against unexpected answers). */
export function isGroup(v: unknown): v is SyncGroup {
  const g = v as SyncGroup | null;
  return !!g && typeof g.id === "string" && Array.isArray(g.members) && Array.isArray(g.queue) && typeof g.state === "string";
}

/**
 * Estimates the offset between this device's clock and the server's from
 * ping/pong round trips; the sample with the shortest round trip wins, since
 * its one-way delays are the most symmetric.
 */
export class ClockSync {
  private samples: { offset: number; rtt: number }[] = [];

  add(sentAt: number, receivedAt: number, serverTime: number) {
    const rtt = Math.max(0, receivedAt - sentAt);
    this.samples.push({ offset: serverTime - (sentAt + receivedAt) / 2, rtt });
    if (this.samples.length > 8) this.samples.shift();
  }

  get ready(): boolean {
    return this.samples.length > 0;
  }

  /** Server minus local clock, in ms. */
  get offset(): number {
    let best: { offset: number; rtt: number } | undefined;
    for (const s of this.samples) if (!best || s.rtt < best.rtt) best = s;
    return best?.offset ?? 0;
  }

  serverNow(local = Date.now()): number {
    return local + this.offset;
  }
}

/** What the local player knows about itself. */
export type LocalPlayer = {
  itemId: string;
  positionMs: number;
  /** Media is loaded and can play at the current position. */
  canPlay: boolean;
  seeking: boolean;
};

/** What the local player should do to follow the group. */
export type Plan = {
  /** Open another item (the group moved on). */
  open?: string;
  /** Seek to this absolute position. */
  seekTo?: number;
  play: boolean;
  /** Start playing at this server time (a scheduled start). */
  startAt?: number;
  rate: number;
  /** Tell the server this player is ready. */
  sendReady: boolean;
};

/** Beyond this, a playing player seeks on a new state; smaller drift is the server's corrections' job. */
export const FOLLOW_SEEK_MS = 2500;
/** A paused player is moved when it is further than this from the shared position. */
export const PAUSED_SEEK_MS = 500;

/** Decides how the local player follows a group state. */
export function plan(g: SyncGroup, local: LocalPlayer, serverNow: number): Plan {
  const entry = currentEntry(g);
  const rate = g.speed;
  if (!entry) return { play: false, rate, sendReady: false };
  if (entry.itemId !== local.itemId) return { open: entry.itemId, play: false, rate, sendReady: false };
  const me = g.members.find((m) => m.id === g.selfId);
  let out: Plan;
  if (g.state === "playing" && g.anchorAt > serverNow) {
    out = { play: false, startAt: g.anchorAt, rate, sendReady: false };
    if (Math.abs(local.positionMs - g.positionMs) > 300) out.seekTo = g.positionMs;
  } else if (g.state === "playing") {
    const target = sharedPosition(g, serverNow);
    out = { play: true, rate, sendReady: false };
    if (Math.abs(local.positionMs - target) > FOLLOW_SEEK_MS) out.seekTo = target;
  } else {
    out = { play: false, rate, sendReady: false };
    if (Math.abs(local.positionMs - g.positionMs) > PAUSED_SEEK_MS) out.seekTo = g.positionMs;
  }
  out.sendReady = !!me && !me.ready && local.canPlay && !local.seeking && out.seekTo === undefined;
  return out;
}

/** Members everyone is waiting for (connected, not ready, not left behind). */
export function waitingFor(g: SyncGroup): SyncMember[] {
  if (g.state !== "waiting") return [];
  return g.members.filter((m) => m.connected && !m.ready && !m.lagging);
}

/** The invite link for a group. */
export function inviteLink(groupId: string, origin = globalThis.location?.origin ?? ""): string {
  return `${origin}/together/${groupId}`;
}

/** Reads a group id from an invite link or a bare id. */
export function parseInvite(text: string): string | null {
  const m = text.trim().match(/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/i);
  return m ? m[1]!.toLowerCase() : null;
}
