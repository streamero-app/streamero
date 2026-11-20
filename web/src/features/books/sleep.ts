/** When an audiobook (or music) sleep timer pauses playback. */
export type SleepTimer = { kind: "off" } | { kind: "at"; at: number } | { kind: "chapter"; endMs: number };

/** Whether the sleep timer should pause playback now. */
export function sleepDue(timer: SleepTimer, now: number, positionMs: number): boolean {
  switch (timer.kind) {
    case "at": return now >= timer.at;
    case "chapter": return positionMs >= timer.endMs - 250;
    default: return false;
  }
}
