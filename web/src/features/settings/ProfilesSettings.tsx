import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, TextField } from "@/components/ui";
import { Avatar, type Profile } from "@/features/profiles/ProfileSwitcher";

const COLORS = ["#e5484d", "#f76b15", "#ffc53d", "#3dd68c", "#0090ff", "#8e4ec6", "#d6409f", "#6e6e80"];
const RATINGS = [0, 6, 7, 12, 13, 15, 16, 18];

type Form = { name: string; color: string; isKid: boolean; maxRating: number | ""; pin: string; removePin: boolean };
const empty: Form = { name: "", color: COLORS[0]!, isKid: false, maxRating: "", pin: "", removePin: false };

export function ProfilesSettings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const profiles = useQuery({ queryKey: ["profiles"], queryFn: async () => unwrap(await api.GET("/profiles")) });
  const [editing, setEditing] = useState<{ id: string | null; form: Form } | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["profiles"] });

  const save = useMutation({
    mutationFn: async (v: { id: string | null; form: Form }) => {
      const f = v.form;
      const body = {
        name: f.name, color: f.color, isKid: f.isKid,
        maxRating: f.maxRating === "" ? undefined : f.maxRating,
        pin: f.pin || undefined, removePin: f.removePin || undefined,
      };
      return v.id
        ? unwrap(await api.PUT("/profiles/{profileId}", { params: { path: { profileId: v.id } }, body }))
        : unwrap(await api.POST("/profiles", { body }));
    },
    onSuccess: () => {
      setEditing(null);
      void refresh();
    },
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/profiles/{profileId}", { params: { path: { profileId: id } } })),
    onSuccess: () => void refresh(),
  });

  const edit = (p: Profile) =>
    setEditing({ id: p.id, form: { name: p.name, color: p.color, isKid: p.isKid, maxRating: p.maxRating ?? "", pin: "", removePin: false } });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (editing) save.mutate(editing);
  };
  const f = editing?.form;
  const set = (patch: Partial<Form>) => editing && setEditing({ ...editing, form: { ...editing.form, ...patch } });

  return (
    <Card title={t("settings.profiles")}>
      <p className="mb-4 text-sm text-muted">{t("profiles.intro")}</p>
      <ErrorText error={profiles.error ?? remove.error} />
      <ul className="divide-y divide-border">
        {profiles.data?.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-3 py-3" data-testid="profile-row">
            <Avatar name={p.name} color={p.color} size={36} />
            <div className="min-w-0 flex-1">
              <p className="font-medium">{p.name}</p>
              <p className="flex gap-1">
                {p.isKid && <Badge tone="accent">{t("profiles.kid")}</Badge>}
                {p.hasPin && <Badge>{t("profiles.pinProtected")}</Badge>}
                {p.maxRating !== undefined && <Badge>{t("profiles.upTo", { rating: p.maxRating })}</Badge>}
              </p>
            </div>
            <Button variant="secondary" onClick={() => edit(p)}>{t("profiles.edit")}</Button>
            <Button variant="danger" disabled={remove.isPending}
              onClick={() => window.confirm(t("profiles.confirmDelete", { name: p.name })) && remove.mutate(p.id)}>
              {t("users.delete")}
            </Button>
          </li>
        ))}
      </ul>
      {!editing && (profiles.data?.length ?? 0) < 6 && (
        <Button className="mt-4" onClick={() => setEditing({ id: null, form: empty })}>{t("profiles.add")}</Button>
      )}
      {editing && f && (
        <form className="mt-4 flex flex-col gap-4 rounded-md border border-border p-4" onSubmit={onSubmit}>
          <TextField label={t("profiles.name")} required maxLength={40} autoFocus value={f.name} onChange={(e) => set({ name: e.target.value })} />
          <fieldset className="flex flex-col gap-2 text-sm">
            <legend className="mb-1">{t("profiles.color")}</legend>
            <div className="flex flex-wrap gap-2">
              {COLORS.map((c) => (
                <button key={c} type="button" aria-label={c} aria-pressed={f.color === c} onClick={() => set({ color: c })}
                  className={`h-8 w-8 rounded-md ${f.color === c ? "ring-2 ring-text ring-offset-2 ring-offset-surface" : ""}`}
                  style={{ background: c }} />
              ))}
            </div>
          </fieldset>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={f.isKid} onChange={(e) => set({ isKid: e.target.checked })} />
            {t("profiles.kidProfile")}
          </label>
          <label className="flex max-w-xs flex-col gap-1 text-sm">
            {t("profiles.maxRating")}
            <select value={f.maxRating} onChange={(e) => set({ maxRating: e.target.value === "" ? "" : Number(e.target.value) })}
              className="rounded-md border border-border bg-bg px-3 py-2 text-base text-text">
              <option value="">{t("profiles.noLimit")}</option>
              {RATINGS.map((r) => <option key={r} value={r}>{t("profiles.upTo", { rating: r })}</option>)}
            </select>
          </label>
          <div className="max-w-xs">
            <TextField label={t("profiles.pinOptional")} type="password" inputMode="numeric" pattern="[0-9]{4,6}" maxLength={6}
              autoComplete="new-password" value={f.pin} onChange={(e) => set({ pin: e.target.value, removePin: false })} hint={t("profiles.pinHint")} />
          </div>
          {editing.id && profiles.data?.find((p) => p.id === editing.id)?.hasPin && (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={f.removePin} onChange={(e) => set({ removePin: e.target.checked, pin: "" })} />
              {t("profiles.removePin")}
            </label>
          )}
          <ErrorText error={save.error} />
          <div className="flex gap-2">
            <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
            <Button type="button" variant="ghost" onClick={() => setEditing(null)}>{t("common.cancel")}</Button>
          </div>
        </form>
      )}
    </Card>
  );
}
