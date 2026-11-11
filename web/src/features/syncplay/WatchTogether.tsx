import { useState } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Check, Copy, LogOut, Users } from "lucide-react";
import { ApiError, api, unwrap } from "@/api/client";
import { Badge, Button, Spinner, TextField, buttonVariants, pressable } from "@/components/ui";

const bigButton = "tv-focus h-13 rounded-[6px] px-7 text-base font-semibold";
import { currentEntry, inviteLink, parseInvite, waitingFor, type SyncGroup, type SyncMember } from "./engine";
import { useSyncPlay } from "./SyncPlay";

/** A refused watch-together action, in words. */
export function TogetherError({ error }: { error: unknown }) {
  const { t } = useTranslation();
  if (!error) return null;
  const code = error instanceof ApiError ? error.code : typeof error === "string" ? error : "generic";
  return <p role="alert" className="text-sm text-danger">{t(`together.errors.${code}`, { defaultValue: t("errors.generic") })}</p>;
}

/** A member's status as a short label. */
function memberStatus(m: SyncMember, t: (k: string) => string): { label: string; tone: "muted" | "accent" | "danger" | "success" } {
  if (!m.connected) return { label: t("together.offline"), tone: "muted" };
  if (m.buffering) return { label: t("together.buffering"), tone: "danger" };
  if (m.lagging) return { label: t("together.lagging"), tone: "accent" };
  if (m.ready) return { label: t("together.ready"), tone: "success" };
  return { label: t("together.loading"), tone: "muted" };
}

export function MemberList({ group }: { group: SyncGroup }) {
  const { t } = useTranslation();
  return (
    <ul className="flex flex-col gap-1.5" data-testid="together-members">
      {group.members.map((m) => {
        const s = memberStatus(m, t);
        return (
          <li key={m.id} className="flex items-center gap-3 rounded-md bg-white/[0.04] px-3 py-2 text-sm">
            <span className="grid size-7 shrink-0 place-items-center rounded-full bg-white/10 text-xs font-bold uppercase">{m.name.slice(0, 1)}</span>
            <span className="min-w-0 flex-1 truncate">
              {m.name}{m.id === group.selfId ? ` (${t("together.you")})` : ""}
              {m.device && <span className="block truncate text-xs text-white/50">{m.device}</span>}
            </span>
            <Badge tone={s.tone}>{s.label}</Badge>
          </li>
        );
      })}
    </ul>
  );
}

export function CopyInvite({ groupId }: { groupId: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const link = inviteLink(groupId);
  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-semibold">{t("together.invite")}</span>
      <div className="flex gap-2">
        <input readOnly value={link} aria-label={t("together.invite")} data-testid="together-link"
          className="h-10 min-w-0 flex-1 rounded-md bg-white/10 px-3 text-sm ring-1 ring-white/15" onFocus={(e) => e.currentTarget.select()} />
        <button type="button" className="inline-flex h-10 items-center gap-1.5 rounded-md bg-white px-3 text-sm font-bold text-black hover:bg-white/85"
          onClick={() => {
            void navigator.clipboard?.writeText(link).then(() => setCopied(true), () => undefined);
            window.setTimeout(() => setCopied(false), 2000);
          }}>
          {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}{copied ? t("together.copied") : t("together.copy")}
        </button>
      </div>
    </div>
  );
}

/** "Waiting for Ana, Ben" while the group waits for buffering members. */
export function WaitingBanner({ group }: { group: SyncGroup }) {
  const { t } = useTranslation();
  const names = waitingFor(group).map((m) => m.name);
  if (names.length === 0) return null;
  return (
    <p role="status" data-testid="together-waiting"
      className="absolute top-20 left-1/2 z-30 -translate-x-1/2 rounded-full bg-black/80 px-4 py-2 text-sm font-semibold text-white ring-1 ring-white/15 backdrop-blur">
      {t("together.waitingFor", { names: names.join(", ") })}
    </p>
  );
}

/** The player's watch-together panel: start a group, invite, members, leave. */
export function TogetherPanel({ itemId, positionMs, onClose }: { itemId: string; positionMs: number; onClose: () => void }) {
  const { t } = useTranslation();
  const sync = useSyncPlay();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const g = sync.group;
  return (
    <div className="flex flex-col gap-4" data-testid="together-panel">
      <h2 className="flex items-center gap-2 font-head text-lg"><Users className="size-5" aria-hidden />{t("together.title")}</h2>
      {!g ? (
        <>
          <p className="text-sm text-white/70">{t("together.intro")}</p>
          <Button disabled={busy} data-testid="together-start" onClick={() => {
            setBusy(true);
            sync.create([itemId], positionMs).catch(setError).finally(() => setBusy(false));
          }}>{t("together.startHere")}</Button>
        </>
      ) : (
        <>
          {!sync.connected && <p className="text-sm text-white/60">{t("together.reconnecting")}</p>}
          <CopyInvite groupId={g.id} />
          <MemberList group={g} />
          <button type="button" data-testid="together-leave"
            className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-white/10 text-sm font-semibold hover:bg-white/20"
            onClick={() => { void sync.leave(); onClose(); }}>
            <LogOut className="size-4" aria-hidden />{t("together.leave")}
          </button>
        </>
      )}
      {error ? <TogetherError error={error} /> : null}
    </div>
  );
}

/** "Watch together" on a title page: starts a group with the title and opens the player. */
export function WatchTogetherButton({ itemId }: { itemId: string }) {
  const { t } = useTranslation();
  const sync = useSyncPlay();
  const navigate = useNavigate();
  const [error, setError] = useState<unknown>(null);
  if (!sync.allowed) return null;
  return (
    <>
      <button type="button" data-testid="watch-together" className={`${pressable} ${buttonVariants.glass} ${bigButton}`}
        onClick={() => {
          sync.create([itemId]).then(() => navigate({ to: "/watch/$itemId", params: { itemId }, search: { together: 1 } }), setError);
        }}>
        <Users className="size-5" aria-hidden />{t("together.title")}
      </button>
      {error ? <TogetherError error={error} /> : null}
    </>
  );
}

function GroupCard({ g, onJoin, busy }: { g: SyncGroup; onJoin: () => void; busy: boolean }) {
  const { t } = useTranslation();
  const entry = currentEntry(g);
  return (
    <div className="flex flex-col gap-3 rounded-[8px] bg-surface p-5 ring-1 ring-border" data-testid="together-group">
      <div>
        <p className="font-head text-lg">{t("together.groupOf", { name: g.name })}</p>
        <p className="text-sm text-text/65">{entry ? entry.title : t("together.nothingYet")}</p>
      </div>
      <p className="text-sm text-text/65">{g.members.map((m) => m.name).join(", ")}</p>
      <Button disabled={busy} onClick={onJoin}>{t("together.join")}</Button>
    </div>
  );
}

function useJoin() {
  const sync = useSyncPlay();
  const navigate = useNavigate();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const join = (id: string) => {
    setBusy(true);
    setError(null);
    sync.join(id).then((g) => {
      const entry = currentEntry(g);
      if (entry) void navigate({ to: "/watch/$itemId", params: { itemId: entry.itemId }, search: { together: 1 } });
      else void navigate({ to: "/" });
    }, setError).finally(() => setBusy(false));
  };
  return { join, error, busy };
}

const page = "mx-auto flex w-[min(100%-2.5rem,44rem)] flex-col gap-6 pt-28 pb-16";

/** /together: groups to join, or paste an invite link. */
export function TogetherListPage() {
  const { t } = useTranslation();
  const sync = useSyncPlay();
  const { join, error, busy } = useJoin();
  const [code, setCode] = useState("");
  const groups = useQuery({
    queryKey: ["syncplay", "groups"],
    queryFn: async () => unwrap(await api.GET("/syncplay/groups")),
    refetchInterval: 5000,
    enabled: sync.allowed,
  });
  const id = parseInvite(code);
  return (
    <div className={page}>
      <h1 className="font-display text-5xl">{t("together.title")}</h1>
      {!sync.allowed ? <p className="text-text/70">{t("together.notAllowed")}</p> : (
        <>
          <form className="flex items-end gap-3" onSubmit={(e) => { e.preventDefault(); if (id) join(id); }}>
            <div className="flex-1"><TextField label={t("together.pasteLink")} value={code} onChange={(e) => setCode(e.target.value)} /></div>
            <Button type="submit" disabled={!id || busy}>{t("together.join")}</Button>
          </form>
          {error ? <TogetherError error={error} /> : null}
          <h2 className="font-head text-xl">{t("together.groups")}</h2>
          {groups.isPending ? <Spinner /> : (groups.data ?? []).length === 0
            ? <p className="text-text/65">{t("together.noGroups")}</p>
            : (groups.data ?? []).map((g) => <GroupCard key={g.id} g={g} busy={busy} onJoin={() => join(g.id)} />)}
        </>
      )}
    </div>
  );
}

/** /together/:groupId — the invite link. */
export function TogetherInvitePage() {
  const { t } = useTranslation();
  const { groupId } = useParams({ from: "/app/together/$groupId" });
  const sync = useSyncPlay();
  const { join, error, busy } = useJoin();
  const g = useQuery({
    queryKey: ["syncplay", "group", groupId],
    queryFn: async () => unwrap(await api.GET("/syncplay/groups/{groupId}", { params: { path: { groupId } } })),
    enabled: sync.allowed,
    retry: false,
  });
  return (
    <div className={page}>
      <h1 className="font-display text-5xl">{t("together.title")}</h1>
      {!sync.allowed ? <p className="text-text/70">{t("together.notAllowed")}</p>
        : g.isPending ? <Spinner />
        : g.error || !g.data ? (
          <div className="flex flex-col gap-4">
            <p className="text-text/70" data-testid="together-gone">{t("together.gone")}</p>
            <Link to="/together" className="text-sm underline">{t("together.groups")}</Link>
          </div>
        ) : <GroupCard g={g.data} busy={busy} onJoin={() => join(groupId)} />}
      {error ? <TogetherError error={error} /> : null}
    </div>
  );
}
