import { useEffect, type RefObject } from "react";

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * A full-screen layer that behaves as a modal dialog for the keyboard: focus moves
 * into it when it opens, Tab and Shift+Tab stay inside, and focus returns to where it
 * was when it closes.
 */
export function useFocusTrap(ref: RefObject<HTMLElement | null>, initial?: string) {
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const before = document.activeElement as HTMLElement | null;
    (initial ? el.querySelector<HTMLElement>(initial) : null)?.focus() ?? el.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const items = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((x) => !x.closest("[hidden],[inert]"));
      if (!items.length) return;
      const first = items[0]!;
      const last = items[items.length - 1]!;
      if (!el.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
      else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      if (before?.isConnected) before.focus();
    };
  }, [ref, initial]);
}
