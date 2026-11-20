/** Pure helpers of the video players (kept out of the components so they can be unit-tested). */

/**
 * The WebVTT link of a text track for the stream that is playing. A transcoded
 * (HLS) stream that starts at startMs plays from time 0, so the server moves its
 * cues startMs earlier (`offsetMs`, outside the signature); direct play keeps
 * the file's own times.
 */
export function subtitleSrc(url: string, method: string, startMs: number): string {
  if (method === "direct" || !(startMs > 0)) return url;
  return `${url}${url.includes("?") ? "&" : "?"}offsetMs=${Math.round(startMs)}`;
}

/**
 * Whether to hand an HLS stream to the browser itself instead of hls.js. Apple's
 * WebKit (Safari, every iPhone browser) plays HLS natively and best. Chrome now
 * also answers "maybe" for HLS, but its own player cannot parse the server's
 * transcoded streams (DEMUXER_ERROR_COULD_NOT_PARSE), so wherever Media Source
 * Extensions exist and the browser is not Apple's, hls.js plays the stream.
 */
export function prefersNativeHls(env: { canPlayHls: boolean; mediaSource: boolean; vendor: string }): boolean {
  if (!env.canPlayHls) return false;
  return !env.mediaSource || /apple/i.test(env.vendor);
}

/** prefersNativeHls for this browser and video element. */
export function nativeHls(video: HTMLMediaElement): boolean {
  const w = typeof window !== "undefined" ? (window as unknown as { MediaSource?: unknown; ManagedMediaSource?: unknown }) : undefined;
  return prefersNativeHls({
    canPlayHls: video.canPlayType("application/vnd.apple.mpegurl") !== "",
    mediaSource: !!w && (typeof w.MediaSource !== "undefined" || typeof w.ManagedMediaSource !== "undefined"),
    vendor: typeof navigator !== "undefined" ? navigator.vendor ?? "" : "",
  });
}

/**
 * Numbers playback requests so that only the latest answer is used: rapid seeks
 * or a seek during an audio switch send several, and they may come back out of order.
 */
export function requestSequence() {
  let last = 0;
  return { next: () => ++last, isLatest: (n: number) => n === last, bump: () => void ++last };
}

/**
 * Whether Space/Enter belongs to the focused element instead of the player:
 * form fields always, buttons and links only when the viewer moved focus there
 * with the keyboard or remote (`:focus-visible`), not when a click left it there.
 */
export function keyBelongsToControl(target: EventTarget | null, key: string): boolean {
  const el = target as HTMLElement | null;
  const tag = el?.tagName;
  if (tag === "SELECT" || tag === "INPUT" || tag === "TEXTAREA" || el?.isContentEditable) return true;
  if ((tag !== "BUTTON" && tag !== "A") || (key !== " " && key !== "Enter")) return false;
  try {
    return el!.matches(":focus-visible");
  } catch {
    return false; // no :focus-visible support: Space is the player's
  }
}

type WebkitVideo = HTMLVideoElement & { webkitEnterFullscreen?: () => void; webkitExitFullscreen?: () => void; webkitDisplayingFullscreen?: boolean };

/** Whether the player is full screen, as an element (most browsers) or as iPhone's native video view. */
export function isFullscreen(video: HTMLVideoElement | null): boolean {
  return !!document.fullscreenElement || !!(video as WebkitVideo | null)?.webkitDisplayingFullscreen;
}

/**
 * Enters or leaves full screen. iPhone Safari has no element full screen, only the
 * video's own (`webkitEnterFullscreen`), which shows the system controls.
 */
export function toggleFullscreen(root: HTMLElement | null, video: HTMLVideoElement | null): void {
  const v = video as WebkitVideo | null;
  if (document.fullscreenElement) {
    void document.exitFullscreen().catch(() => undefined);
    return;
  }
  if (v?.webkitDisplayingFullscreen) {
    v.webkitExitFullscreen?.();
    return;
  }
  if (root?.requestFullscreen && document.fullscreenEnabled !== false) {
    void root.requestFullscreen().catch(() => v?.webkitEnterFullscreen?.());
    return;
  }
  v?.webkitEnterFullscreen?.();
}

/**
 * Stops a media element for good: no more downloading, decoding or buffered
 * memory (a detached element with a src keeps its connection open until GC).
 */
export function releaseMedia(el: HTMLMediaElement | null | undefined): void {
  if (!el) return;
  try {
    el.pause();
    el.removeAttribute("src");
    el.load();
  } catch {
    /* not implemented in test DOMs */
  }
}

/**
 * play() with the outcome the viewer needs to see: true when the browser refused
 * to start without a gesture (show a big play button), false when it played or
 * the attempt was merely interrupted by a newer load.
 */
export async function tryPlay(video: HTMLMediaElement): Promise<boolean> {
  try {
    await video.play();
    return false;
  } catch (e) {
    return (e as { name?: string } | null)?.name === "NotAllowedError";
  }
}
