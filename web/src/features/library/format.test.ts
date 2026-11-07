import { channelsLabel, formatDuration, formatSize, resolutionLabel } from "./format";

describe("library formatting", () => {
  it("labels resolutions including scope formats", () => {
    expect(resolutionLabel(3840, 2160)).toBe("4K");
    expect(resolutionLabel(3840, 1600)).toBe("4K");
    expect(resolutionLabel(1920, 800)).toBe("1080p");
    expect(resolutionLabel(1280, 720)).toBe("720p");
    expect(resolutionLabel(720, 576)).toBe("SD");
    expect(resolutionLabel()).toBe("");
  });
  it("formats durations, sizes and channels", () => {
    expect(formatDuration(12_000)).toBe("1 min");
    expect(formatDuration(7_260_000)).toBe("2 h 1 min");
    expect(formatSize(1536)).toBe("1.5 KB");
    expect(formatSize(5 * 1024 ** 3)).toBe("5.0 GB");
    expect(channelsLabel(6)).toBe("5.1");
    expect(channelsLabel(2)).toBe("stereo");
  });
});
