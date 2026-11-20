import type { QueryClient } from "@tanstack/react-query";

/**
 * Queries whose rows carry the profile's own state (watched, progress, favourite,
 * reading position). After any change to that state they are all out of date: a
 * title marked watched on its page must not still show as unwatched in a row.
 */
export const USER_DATA_KEYS = [
  "item", "library-items", "favorites", "continue", "continue-reading", "next-up", "latest", "row",
  "books", "books-related", "photos", "search", "collection", "playlist", "listen", "next",
] as const;

const keys = new Set<string>(USER_DATA_KEYS);

/**
 * Marks every row with per-profile state stale. After an explicit action ("mark
 * watched") the rows on screen refetch at once; background saves from a player or
 * reader pass `quiet`, so nothing refetches until those rows are shown again.
 */
export function invalidateUserData(qc: QueryClient, { quiet = false } = {}): Promise<void> {
  return qc.invalidateQueries({ predicate: (q) => keys.has(String(q.queryKey[0])), refetchType: quiet ? "none" : "active" });
}
