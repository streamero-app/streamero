import { createContext, useCallback, useContext, useEffect, useMemo, useRef, type ReactNode } from "react";

/**
 * The ambient room: the interface is lit by the title in focus. Colours come from the
 * server (palette of the artwork) and are written to CSS custom properties that are
 * registered with @property, so the browser animates the light between titles.
 */
export const BRAND: [string, string] = ["#ffaa45", "#2f8f9d"];

type Ctx = { light: (colors?: string[] | null) => void; lightSoon: (colors?: string[] | null) => void };
const AmbientContext = createContext<Ctx>({ light: () => undefined, lightSoon: () => undefined });

function luminance(hex: string): number {
  const v = parseInt(hex.slice(1), 16);
  const ch = [(v >> 16) & 255, (v >> 8) & 255, v & 255].map((c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0]! + 0.7152 * ch[1]! + 0.0722 * ch[2]!;
}

export function apply(colors?: string[] | null) {
  const [a, b] = colors && colors.length && /^#[0-9a-f]{6}$/i.test(colors[0]!) ? [colors[0]!, colors[1] ?? colors[0]!] : BRAND;
  const root = document.documentElement.style;
  root.setProperty("--amb-a", a);
  root.setProperty("--amb-b", b);
  // Text on the light colour: black on bright light, white on deep light.
  root.setProperty("--amb-ink", luminance(a) > 0.42 ? "#0b0b0c" : "#ffffff");
}

export function AmbientProvider({ children }: { children: ReactNode }) {
  const timer = useRef<number | undefined>(undefined);
  const light = useCallback((colors?: string[] | null) => {
    window.clearTimeout(timer.current);
    apply(colors);
  }, []);
  // Hovering across a row should not flicker the whole room: settle first.
  const lightSoon = useCallback((colors?: string[] | null) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => apply(colors), 220);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const value = useMemo(() => ({ light, lightSoon }), [light, lightSoon]);
  return (
    <AmbientContext.Provider value={value}>
      <div className="ambient-room" aria-hidden />
      {children}
    </AmbientContext.Provider>
  );
}

export function useAmbient() {
  return useContext(AmbientContext);
}

/** Lights the room with these colours while the calling page is shown. */
export function useAmbientLight(colors?: string[] | null) {
  const { light } = useAmbient();
  const key = colors?.join(",") ?? "";
  useEffect(() => {
    light(colors);
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
}
