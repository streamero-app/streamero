/** Guards for links and navigation targets built from data the page did not write itself. */

const origin = () => globalThis.location?.origin ?? "http://localhost";

/**
 * A link target taken from API data, or undefined when it is not one of the allowed
 * schemes (so `javascript:`, `data:` and friends never reach an `href`). Relative
 * links stay relative; absolute ones are returned normalised.
 */
export function safeHref(value: string | null | undefined, schemes: readonly string[] = ["http:", "https:"]): string | undefined {
  if (!value) return undefined;
  const raw = value.trim();
  if (!raw) return undefined;
  let url: URL;
  try {
    url = new URL(raw, origin());
  } catch {
    return undefined;
  }
  if (!schemes.includes(url.protocol)) return undefined;
  // A path on this server keeps its relative form; anything else is spelled out in full.
  return raw.startsWith("/") && !raw.startsWith("//") && url.origin === origin() ? raw : url.href;
}

/**
 * A place inside this app to go back to (after signing in), or undefined. Only
 * same-origin paths are accepted, so a crafted value can never send the visitor
 * to another site.
 */
export function internalPath(value: string | null | undefined): string | undefined {
  if (!value || !value.startsWith("/") || value.startsWith("//") || value.includes("\\")) return undefined;
  // Control characters are dropped by URL parsers, which would turn "/\t/host" into "//host".
  if (/[\u0000-\u001f\u007f]/.test(value)) return undefined;
  let url: URL;
  try {
    url = new URL(value, origin());
  } catch {
    return undefined;
  }
  if (url.origin !== origin()) return undefined;
  const path = url.pathname + url.search + url.hash;
  // Never bounce back to the pages that only make sense signed out.
  if (/^\/(login|setup|invite|who)(\/|$|\?)/.test(path)) return undefined;
  return path;
}

const RETURN_KEY = "streamero.returnTo";
let signingOut = false;

/** A deliberate sign-out: the next person to sign in starts at home, not on this page. */
export function forgetReturnTo() {
  signingOut = true;
  try {
    sessionStorage.removeItem(RETURN_KEY);
  } catch {
    /* storage unavailable */
  }
}

/** Remembers where an anonymous visitor wanted to go (this tab only). */
export function rememberReturnTo(path: string) {
  if (signingOut) return;
  const safe = internalPath(path);
  try {
    if (safe && safe !== "/") sessionStorage.setItem(RETURN_KEY, safe);
  } catch {
    /* storage unavailable: sign-in simply lands on home */
  }
}

/** The remembered place, read once. */
export function takeReturnTo(): string | undefined {
  signingOut = false;
  try {
    const v = sessionStorage.getItem(RETURN_KEY);
    sessionStorage.removeItem(RETURN_KEY);
    return internalPath(v);
  } catch {
    return undefined;
  }
}
