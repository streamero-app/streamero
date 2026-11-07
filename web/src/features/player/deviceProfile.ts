import type { components } from "@/api/schema.gen";

export type DeviceProfile = components["schemas"]["DeviceProfile"];

type Probe = (mime: string) => boolean;

/** What this browser can decode natively, as ffprobe codec names. */
export function detectDeviceProfile(probe?: Probe, nativeHls?: boolean): DeviceProfile {
  const ms = typeof MediaSource !== "undefined" ? MediaSource : undefined;
  const video = typeof document !== "undefined" ? document.createElement("video") : undefined;
  const can: Probe = probe ?? ((mime) => !!ms?.isTypeSupported?.(mime) || (video?.canPlayType(mime) ?? "") !== "");
  const videoCodecs: string[] = [];
  if (can('video/mp4; codecs="avc1.640028"')) videoCodecs.push("h264");
  if (can('video/mp4; codecs="hvc1.1.6.L120.90"') || can('video/mp4; codecs="hev1.1.6.L120.90"')) videoCodecs.push("hevc");
  if (can('video/mp4; codecs="av01.0.08M.08"')) videoCodecs.push("av1");
  if (can('video/webm; codecs="vp9"')) videoCodecs.push("vp9");
  const audioCodecs: string[] = [];
  if (can('audio/mp4; codecs="mp4a.40.2"')) audioCodecs.push("aac");
  if (can("audio/mpeg")) audioCodecs.push("mp3");
  if (can('audio/webm; codecs="opus"')) audioCodecs.push("opus");
  if (can('audio/mp4; codecs="flac"')) audioCodecs.push("flac");
  if (can('audio/mp4; codecs="ac-3"')) audioCodecs.push("ac3");
  if (can('audio/mp4; codecs="ec-3"')) audioCodecs.push("eac3");
  const containers = ["mp4", "m4v"];
  if (can('video/webm; codecs="vp9"')) containers.push("webm");
  const hls = nativeHls ?? (video?.canPlayType("application/vnd.apple.mpegurl") ?? "") !== "";
  return { containers, videoCodecs, audioCodecs, hls: hls || !!ms };
}
