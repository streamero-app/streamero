import { useEffect, type KeyboardEvent, type RefObject } from "react";

const ITEMS = '[role="menuitem"]:not([disabled]), [role="menuitemradio"]:not([disabled])';

/**
 * Keyboard and focus for a popup menu (WAI-ARIA menu button pattern): focus moves
 * to the first item on open, arrows/Home/End move between items, Escape closes and
 * puts focus back on the button, and a click or focus outside closes it.
 */
export function useMenu(open: boolean, setOpen: (v: boolean) => void, root: RefObject<HTMLElement | null>, button: RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!open) return;
    root.current?.querySelector<HTMLElement>(ITEMS)?.focus();
    const outside = (e: Event) => { if (!root.current?.contains(e.target as Node)) setOpen(false); };
    document.addEventListener("mousedown", outside);
    document.addEventListener("focusin", outside);
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("focusin", outside);
    };
  }, [open, root, setOpen]);

  return (e: KeyboardEvent<HTMLElement>) => {
    const items = Array.from(e.currentTarget.querySelectorAll<HTMLElement>(ITEMS));
    const at = items.indexOf(document.activeElement as HTMLElement);
    let next = -1;
    if (e.key === "ArrowDown") next = at < 0 ? 0 : (at + 1) % items.length;
    else if (e.key === "ArrowUp") next = at < 0 ? items.length - 1 : (at - 1 + items.length) % items.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = items.length - 1;
    else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
      button.current?.focus();
      return;
    } else return;
    // Keeps the app's arrow-key navigation (TV remote) from also moving focus.
    e.preventDefault();
    e.stopPropagation();
    items[next]?.focus();
  };
}
