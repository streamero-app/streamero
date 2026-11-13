import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, TextField } from "@/components/ui";
import type { Library } from "@/features/library/format";
import { useDateTime } from "./SettingsLayout";
import { useConfirm } from "@/components/Dialog";

type Kind = Library["kind"];
type Form = { id: string | null; name: string; kind: Kind; paths: string[] };
const KINDS: Kind[] = ["movies", "shows", "music", "mixed", "books", "audiobooks", "photos"];
const busyStates = new Set(["queued", "scanning"]);

/** Navigates folders under the server's media roots and picks one. */
function FolderPicker({ onPick, onClose }: { onPick: (path: string) => void; onClose: () => void }) {
  const { t } = useTranslation();
  const [path, setPath] = useState<string | undefined>(undefined);
  const listing = useQuery({
    queryKey: ["fs", path ?? ""],
    queryFn: async () => unwrap(await api.GET("/fs/directories", { params: { query: path ? { path } : {} } })),
  });
  const short = (p: string) => p.split("/").filter(Boolean).pop() ?? p;
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-bg p-3" data-testid="folder-picker">
      <div className="flex items-center gap-2 text-sm">
        <code className="flex-1 truncate text-muted">{listing.data?.path ?? t("libraries.mediaRoots")}</code>
        {listing.data?.path && (
          <Button type="button" variant="ghost" onClick={() => setPath(listing.data?.parent || undefined)}>↑ {t("libraries.up")}</Button>
        )}
      </div>
      <ErrorText error={listing.error} />
      <ul className="max-h-64 overflow-y-auto">
        {listing.data?.directories.map((d) => (
          <li key={d}>
            <button type="button" className="w-full truncate rounded px-2 py-1.5 text-left text-sm hover:bg-surface-2" onClick={() => setPath(d)}>
              📁 {listing.data?.path ? short(d) : d}
            </button>
          </li>
        ))}
        {listing.data && listing.data.directories.length === 0 && <li className="px-2 py-1 text-sm text-muted">{t("libraries.noSubfolders")}</li>}
      </ul>
      <div className="flex gap-2">
        {listing.data?.path && <Button type="button" onClick={() => onPick(listing.data!.path!)}>{t("libraries.chooseFolder")}</Button>}
        <Button type="button" variant="ghost" onClick={onClose}>{t("common.cancel")}</Button>
      </div>
    </div>
  );
}

export function LibrariesSettings() {
  const confirmDialog = useConfirm();
  const { t } = useTranslation();
  const qc = useQueryClient();
  const fmt = useDateTime();
  const libraries = useQuery({
    queryKey: ["libraries"],
    queryFn: async () => unwrap(await api.GET("/libraries")),
    refetchInterval: (q) => (q.state.data?.some((l) => busyStates.has(l.scan.state)) ? 2000 : false),
  });
  const [form, setForm] = useState<Form | null>(null);
  const [picking, setPicking] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ["libraries"] });

  const save = useMutation({
    mutationFn: async (f: Form) => {
      const body = { name: f.name, kind: f.kind, paths: f.paths };
      return f.id
        ? unwrap(await api.PUT("/libraries/{libraryId}", { params: { path: { libraryId: f.id } }, body }))
        : unwrap(await api.POST("/libraries", { body }));
    },
    onSuccess: () => {
      setForm(null);
      void refresh();
    },
  });
  const scan = useMutation({
    mutationFn: async ({ id, confirmRemoval }: { id: string; confirmRemoval?: boolean }) =>
      unwrap(await api.POST("/libraries/{libraryId}/scan", {
        params: { path: { libraryId: id }, query: confirmRemoval ? { confirmRemoval: true } : {} },
      })),
    onSuccess: () => void refresh(),
  });
  const refreshMeta = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/libraries/{libraryId}/refresh-metadata", { params: { path: { libraryId: id } } })),
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/libraries/{libraryId}", { params: { path: { libraryId: id } } })),
    onSuccess: () => void refresh(),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (form) save.mutate(form);
  };
  const stateBadge = (l: Library) => {
    switch (l.scan.state) {
      case "queued": return <Badge tone="accent">{t("libraries.queued")}</Badge>;
      case "scanning": return <Badge tone="accent">{t("libraries.scanning")}</Badge>;
      case "failed": return <Badge tone="danger">{t("libraries.failed")}</Badge>;
      default: return null;
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("settings.libraries")}>
        <p className="mb-4 text-sm text-muted">{t("libraries.intro")}</p>
        <ErrorText error={libraries.error ?? scan.error ?? remove.error ?? refreshMeta.error} />
        {refreshMeta.isSuccess && <p role="status" className="mb-2 text-sm text-success">{t("libraries.metadataQueued")}</p>}
        {libraries.data?.length === 0 && <p className="text-sm text-muted">{t("libraries.none")}</p>}
        <ul className="divide-y divide-border">
          {libraries.data?.map((l) => (
            <li key={l.id} className="flex flex-col gap-2 py-4" data-testid="library-row">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{l.name}</span>
                <Badge>{t(`libraries.kind_${l.kind}`)}</Badge>
                {stateBadge(l)}
                <span className="ml-auto text-xs text-muted">
                  {t("libraries.meta", { count: l.itemCount, files: l.scan.filesSeen ?? 0, when: fmt(l.scan.lastScanAt) })}
                </span>
              </div>
              <ul className="text-xs text-muted">{l.paths.map((p) => <li key={p}><code>{p}</code></li>)}</ul>
              {l.scan.state === "failed" && l.scan.error && <p className="text-xs text-danger">{l.scan.error}</p>}
              {l.scan.state !== "failed" && (l.scan.heldRemovals ?? 0) > 0 && (
                <p className="text-xs text-danger" data-testid="held-removals">{t("libraries.held", { count: l.scan.heldRemovals })}</p>
              )}
              {l.scan.state !== "failed" && !l.scan.heldRemovals && l.scan.error && <p className="text-xs text-muted">{l.scan.error}</p>}
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" disabled={busyStates.has(l.scan.state) || scan.isPending} onClick={() => scan.mutate({ id: l.id })}>
                  {t("libraries.scan")}
                </Button>
                {(l.scan.heldRemovals ?? 0) > 0 && (
                  <Button variant="danger" disabled={busyStates.has(l.scan.state) || scan.isPending}
                    onClick={() => window.confirm(t("libraries.confirmRemoveMissing", { count: l.scan.heldRemovals, name: l.name })) &&
                      scan.mutate({ id: l.id, confirmRemoval: true })}>
                    {t("libraries.removeMissing")}
                  </Button>
                )}
                <Button variant="secondary" disabled={refreshMeta.isPending} onClick={() => refreshMeta.mutate(l.id)}>
                  {t("libraries.refreshMetadata")}
                </Button>
                <Button variant="secondary" onClick={() => setForm({ id: l.id, name: l.name, kind: l.kind, paths: l.paths })}>
                  {t("profiles.edit")}
                </Button>
                <Button variant="danger" disabled={remove.isPending}
                  onClick={() => void confirmDialog({ message: t("libraries.confirmDelete", { name: l.name }), danger: true }).then((ok) => ok && remove.mutate(l.id))}>
                  {t("libraries.remove")}
                </Button>
              </div>
            </li>
          ))}
        </ul>
        {!form && (
          <Button className="mt-4" onClick={() => setForm({ id: null, name: "", kind: "movies", paths: [] })}>{t("libraries.add")}</Button>
        )}
      </Card>

      {form && (
        <Card title={form.id ? t("libraries.editTitle") : t("libraries.add")}>
          <form className="flex flex-col gap-4" onSubmit={onSubmit}>
            <TextField label={t("libraries.name")} required maxLength={100} autoFocus placeholder={t("libraries.namePlaceholder")}
              value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            <label className="flex max-w-xs flex-col gap-1 text-sm">
              {t("libraries.kind")}
              <select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as Kind })}
                className="rounded-md border border-border bg-bg px-3 py-2 text-base text-text">
                {KINDS.map((k) => <option key={k} value={k}>{t(`libraries.kind_${k}`)}</option>)}
              </select>
            </label>
            <div className="flex flex-col gap-2 text-sm">
              <span>{t("libraries.folders")}</span>
              {form.paths.length === 0 && <span className="text-xs text-muted">{t("libraries.noFolders")}</span>}
              <ul className="flex flex-col gap-1">
                {form.paths.map((p) => (
                  <li key={p} className="flex items-center gap-2" data-testid="library-folder">
                    <code className="flex-1 truncate rounded bg-bg px-2 py-1">{p}</code>
                    <Button type="button" variant="ghost" onClick={() => setForm({ ...form, paths: form.paths.filter((x) => x !== p) })}>✕</Button>
                  </li>
                ))}
              </ul>
              {picking ? (
                <FolderPicker onClose={() => setPicking(false)} onPick={(p) => {
                  if (!form.paths.includes(p)) setForm({ ...form, paths: [...form.paths, p] });
                  setPicking(false);
                }} />
              ) : (
                <Button type="button" variant="secondary" className="self-start" onClick={() => setPicking(true)}>{t("libraries.addFolder")}</Button>
              )}
              <p className="text-xs text-muted">{t("libraries.foldersHint")}</p>
            </div>
            <ErrorText error={save.error} />
            <div className="flex gap-2">
              <Button type="submit" disabled={save.isPending || form.paths.length === 0}>{t("common.save")}</Button>
              <Button type="button" variant="ghost" onClick={() => { setForm(null); setPicking(false); }}>{t("common.cancel")}</Button>
            </div>
          </form>
        </Card>
      )}
    </div>
  );
}
