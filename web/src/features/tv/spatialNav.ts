/**
 * Arrow-key (TV remote / keyboard) navigation: moves focus to the nearest focusable
 * element in the pressed direction. Text fields keep their arrow keys; Backspace
 * outside a field goes back, like the back button on a remote.
 */
const FOCUSABLE = 'a[href], button:not([disabled]), select, input:not([type="hidden"]), [tabindex="0"]';

type Dir = "ArrowLeft" | "ArrowRight" | "ArrowUp" | "ArrowDown";

function visible(el: HTMLElement): boolean {
  const r = el.getBoundingClientRect();
  if (r.width === 0 || r.height === 0) return false;
  const s = getComputedStyle(el);
  return s.visibility !== "hidden" && s.display !== "none" && !el.closest("[inert],[aria-hidden='true']");
}

function isTyping(el: Element | null): boolean {
  if (!el) return false;
  if (el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) return true;
  if (el instanceof HTMLInputElement) return !["checkbox", "radio", "button", "submit"].includes(el.type);
  return (el as HTMLElement).isContentEditable;
}

/** Picks the best candidate: in the right half-plane, closest along the axis, aligned across it. */
export function nextInDirection(from: DOMRect, candidates: { el: HTMLElement; r: DOMRect }[], dir: Dir): HTMLElement | null {
  const cx = from.left + from.width / 2;
  const cy = from.top + from.height / 2;
  let best: HTMLElement | null = null;
  let bestScore = Infinity;
  for (const { el, r } of candidates) {
    const x = r.left + r.width / 2;
    const y = r.top + r.height / 2;
    let main: number;
    let cross: number;
    switch (dir) {
      case "ArrowRight": main = r.left - from.right; cross = Math.abs(y - cy); if (x <= cx + 1) continue; break;
      case "ArrowLeft": main = from.left - r.right; cross = Math.abs(y - cy); if (x >= cx - 1) continue; break;
      case "ArrowDown": main = r.top - from.bottom; cross = Math.abs(x - cx); if (y <= cy + 1) continue; break;
      case "ArrowUp": main = from.top - r.bottom; cross = Math.abs(x - cx); if (y >= cy - 1) continue; break;
    }
    // Overlap along the axis counts as zero distance; misalignment costs more than distance.
    const score = Math.max(0, main) + cross * 2.2;
    if (score < bestScore) {
      bestScore = score;
      best = el;
    }
  }
  return best;
}

export function installSpatialNav(onBack: () => void): () => void {
  const handler = (e: KeyboardEvent) => {
    if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
    const active = document.activeElement as HTMLElement | null;
    if (e.key === "Backspace" || e.key === "BrowserBack" || e.key === "GoBack") {
      if (isTyping(active)) return;
      e.preventDefault();
      onBack();
      return;
    }
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) return;
    if (isTyping(active) || active?.closest("[role=menu],[role=dialog] [role=listbox],video")) return;
    const scope = (active?.closest("[role=dialog],[role=menu]") as HTMLElement | null) ?? document.body;
    const all = Array.from(scope.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el !== active && visible(el));
    if (!all.length) return;
    e.preventDefault();
    if (!active || active === document.body) {
      all[0]!.focus();
      return;
    }
    const target = nextInDirection(active.getBoundingClientRect(), all.map((el) => ({ el, r: el.getBoundingClientRect() })), e.key as Dir);
    if (target) {
      target.focus({ preventScroll: true });
      target.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "smooth" });
      // Rows near the top bring the whole hero back into view.
      if (target.getBoundingClientRect().top < 120) window.scrollBy({ top: -140, behavior: "smooth" });
    }
  };
  window.addEventListener("keydown", handler);
  return () => window.removeEventListener("keydown", handler);
}
