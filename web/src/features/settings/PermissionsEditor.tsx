import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { components } from "@/api/schema.gen";
import { Button, TextField } from "@/components/ui";

export type Permissions = components["schemas"]["UserPermissions"];
type Flag = Exclude<keyof Permissions, "maxSessions">;

const GROUPS: { title: string; flags: Flag[] }[] = [
  { title: "permissions.groupLibrary", flags: ["allLibraries", "download", "deleteMedia", "manageCollections", "editSubtitles"] },
  { title: "permissions.groupPlayback", flags: ["videoTranscoding", "audioTranscoding", "remux", "syncPlay", "remoteControl"] },
  { title: "permissions.groupLiveTv", flags: ["liveTv", "manageLiveTv"] },
];

/** Checkbox editor for a user's permissions; saves the whole object. */
export function PermissionsEditor({ value, busy, onSave }: { value: Permissions; busy: boolean; onSave: (p: Permissions) => void }) {
  const { t } = useTranslation();
  const [p, setP] = useState<Permissions>(value);
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSave(p);
  };
  return (
    <form className="flex flex-col gap-4 rounded-md border border-border p-4" onSubmit={onSubmit} data-testid="permissions-editor">
      <p className="text-xs text-muted">{t("permissions.note")}</p>
      <div className="grid gap-4 sm:grid-cols-3">
        {GROUPS.map((g) => (
          <fieldset key={g.title} className="flex flex-col gap-2">
            <legend className="mb-1 text-sm font-medium">{t(g.title)}</legend>
            {g.flags.map((f) => (
              <label key={f} className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={p[f]} onChange={(e) => setP({ ...p, [f]: e.target.checked })} />
                {t(`permissions.${f}`)}
              </label>
            ))}
          </fieldset>
        ))}
      </div>
      <div className="max-w-xs">
        <TextField label={t("permissions.maxSessions")} type="number" min={0} max={100} value={p.maxSessions}
          onChange={(e) => setP({ ...p, maxSessions: Math.max(0, Math.min(100, Number(e.target.value) || 0)) })}
          hint={t("permissions.maxSessionsHint")} />
      </div>
      <Button type="submit" disabled={busy} className="self-start">{t("common.save")}</Button>
    </form>
  );
}
