import { useId } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import type { Permissions } from "./PermissionsEditor";

type Library = components["schemas"]["Library"];

/** Library access of an account or invite (D-054): every library, or a chosen list. */
export type LibraryAccess = { all: boolean; ids: string[]; newLibraries: boolean };

/** Server defaults for new accounts (mirrors auth.DefaultPermissions). */
export const DEFAULT_PERMISSIONS: Permissions = {
  allLibraries: true, download: true, videoTranscoding: true, audioTranscoding: true, remux: true, liveTv: true,
  manageLiveTv: false, syncPlay: true, deleteMedia: false, manageCollections: false, editSubtitles: false, remoteControl: true, maxSessions: 0,
};

export const ALL_LIBRARIES: LibraryAccess = { all: true, ids: [], newLibraries: false };

/** Every library as an administrator sees it (the same query the settings pages share). */
export function useAllLibraries() {
  return useQuery({ queryKey: ["libraries", "all"], queryFn: async () => unwrap(await api.GET("/libraries")) });
}

/** Libraries that can be part of a chosen list: adult libraries have their own grant (D-053). */
export function pickable(libraries: Library[] | undefined): Library[] {
  return (Array.isArray(libraries) ? libraries : []).filter((l) => l.kind !== "adult");
}

/**
 * "All libraries" or a chosen list with one checkbox per (non-adult) library.
 * `withNewLibraries` adds the "also libraries created later" switch.
 */
export function LibraryAccessPicker({ value, onChange, withNewLibraries = true, allLabel, chosenLabel, emptyHint, adultNote, disabled = false }: {
  value: LibraryAccess; onChange: (v: LibraryAccess) => void; withNewLibraries?: boolean;
  allLabel?: string; chosenLabel?: string; emptyHint?: string; adultNote?: string; disabled?: boolean;
}) {
  const { t } = useTranslation();
  const name = useId();
  const libraries = useAllLibraries();
  const list = pickable(libraries.data);
  const hasAdult = Array.isArray(libraries.data) && libraries.data.some((l) => l.kind === "adult");
  const toggle = (id: string, on: boolean) =>
    onChange({ ...value, ids: on ? [...value.ids.filter((x) => x !== id), id] : value.ids.filter((x) => x !== id) });
  // Ids of libraries that no longer exist (or are adult) are not counted or shown.
  const chosen = value.ids.filter((id) => list.some((l) => l.id === id));
  return (
    <fieldset className="flex flex-col gap-3 text-sm" disabled={disabled} data-testid="library-access">
      <div className="flex flex-wrap gap-x-5 gap-y-2">
        <label className="flex items-center gap-2">
          <input type="radio" name={name} className="accent-[var(--color-accent)]" checked={value.all}
            onChange={() => onChange({ ...value, all: true })} />
          {allLabel ?? t("libraryAccess.all")}
        </label>
        <label className="flex items-center gap-2">
          <input type="radio" name={name} className="accent-[var(--color-accent)]" checked={!value.all}
            onChange={() => onChange({ ...value, all: false })} />
          {chosenLabel ?? t("libraryAccess.chosen")}
        </label>
      </div>
      {!value.all && (
        <div className="flex flex-col gap-3 rounded-md border border-border bg-white/[0.02] p-3">
          {list.length === 0 && libraries.data && <p className="text-muted">{t("libraryAccess.noLibraries")}</p>}
          <div className="grid gap-2 sm:grid-cols-2">
            {list.map((l) => (
              <label key={l.id} className="flex min-w-0 items-center gap-2">
                <input type="checkbox" className="accent-[var(--color-accent)]" checked={value.ids.includes(l.id)}
                  onChange={(e) => toggle(l.id, e.target.checked)} />
                <span className="truncate">{l.name}</span>
                <span className="shrink-0 text-xs text-muted">{t(`libraries.kind_${l.kind}`)}</span>
              </label>
            ))}
          </div>
          {chosen.length === 0 && list.length > 0 && <p className="text-xs text-muted">{emptyHint ?? t("libraryAccess.noneChosen")}</p>}
          {withNewLibraries && (
            <label className="flex items-start gap-2">
              <input type="checkbox" className="mt-0.5 accent-[var(--color-accent)]" checked={value.newLibraries}
                onChange={(e) => onChange({ ...value, newLibraries: e.target.checked })} />
              <span>
                <span className="block">{t("libraryAccess.newLibraries")}</span>
                <span className="text-xs text-muted">{t("libraryAccess.newLibrariesHint")}</span>
              </span>
            </label>
          )}
        </div>
      )}
      {hasAdult && <p className="text-xs text-muted" data-testid="library-access-adult-note">{adultNote ?? t("libraryAccess.adultNote")}</p>}
    </fieldset>
  );
}

/** Short description of a library choice, e.g. "All libraries" or "Filmovi, Serije". */
export function describeAccess(t: TFunction, access: LibraryAccess, libraries: Library[] | undefined): string {
  if (access.all) return t("libraryAccess.all");
  const names = pickable(libraries).filter((l) => access.ids.includes(l.id)).map((l) => l.name);
  if (names.length === 0) return t("libraryAccess.noneShort");
  const shown = names.length > 3 ? t("libraryAccess.someMore", { names: names.slice(0, 3).join(", "), count: names.length - 3 }) : names.join(", ");
  return access.newLibraries ? t("libraryAccess.withNew", { names: shown }) : shown;
}
