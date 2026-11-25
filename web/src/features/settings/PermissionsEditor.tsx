import { useTranslation } from "react-i18next";
import type { components } from "@/api/schema.gen";
import { TextField } from "@/components/ui";

export type Permissions = components["schemas"]["UserPermissions"];
type Flag = Exclude<keyof Permissions, "maxSessions">;

const GROUPS: { title: string; flags: Flag[] }[] = [
  { title: "permissions.groupLibrary", flags: ["allLibraries", "download", "deleteMedia", "manageCollections", "editSubtitles"] },
  { title: "permissions.groupPlayback", flags: ["videoTranscoding", "audioTranscoding", "remux", "syncPlay", "remoteControl"] },
  { title: "permissions.groupLiveTv", flags: ["liveTv", "manageLiveTv"] },
];

/**
 * The permission checkboxes and the device limit, controlled. `hide` leaves out flags
 * another control owns (the user editor sets allLibraries through library access).
 */
export function PermissionsFields({ value, onChange, hide = [] }: { value: Permissions; onChange: (p: Permissions) => void; hide?: Flag[] }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-4" data-testid="permissions-fields">
      <p className="text-xs text-muted">{t("permissions.note")}</p>
      <div className="grid gap-4 sm:grid-cols-3">
        {GROUPS.map((g) => (
          <fieldset key={g.title} className="flex flex-col gap-2">
            <legend className="mb-1 text-sm font-medium">{t(g.title)}</legend>
            {g.flags.filter((f) => !hide.includes(f)).map((f) => (
              <label key={f} className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="accent-[var(--color-accent)]" checked={value[f]}
                  onChange={(e) => onChange({ ...value, [f]: e.target.checked })} />
                {t(`permissions.${f}`)}
              </label>
            ))}
          </fieldset>
        ))}
      </div>
      <div className="max-w-xs">
        <TextField label={t("permissions.maxSessions")} type="number" min={0} max={100} value={value.maxSessions}
          onChange={(e) => onChange({ ...value, maxSessions: Math.max(0, Math.min(100, Number(e.target.value) || 0)) })}
          hint={t("permissions.maxSessionsHint")} />
      </div>
    </div>
  );
}
