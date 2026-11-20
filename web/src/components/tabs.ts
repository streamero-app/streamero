import type { KeyboardEvent } from "react";

/**
 * Keyboard for a role="tablist": Left/Right move between tabs and select them,
 * Home/End jump to the ends (WAI-ARIA tabs pattern, automatic activation). Only the
 * selected tab is in the Tab order (tabIndex 0, the others -1).
 */
export function onTablistKeyDown(e: KeyboardEvent<HTMLElement>) {
  const keys = ["ArrowLeft", "ArrowRight", "Home", "End"];
  if (!keys.includes(e.key)) return;
  const tabs = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('[role="tab"]:not([disabled])'));
  const at = tabs.indexOf(document.activeElement as HTMLElement);
  if (at < 0 || !tabs.length) return;
  const next = e.key === "Home" ? 0 : e.key === "End" ? tabs.length - 1
    : (at + (e.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
  // Keeps the app's arrow-key navigation (TV remote) from also moving focus.
  e.preventDefault();
  e.stopPropagation();
  tabs[next]!.focus();
  tabs[next]!.click();
}
