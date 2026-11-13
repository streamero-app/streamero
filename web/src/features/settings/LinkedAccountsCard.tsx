import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Card, ErrorText, SuccessText } from "@/components/ui";
import { ssoErrorKey, takeQueryParam, useAuthProviders } from "@/features/auth/useSso";
import { useDateTime } from "./SettingsLayout";
import { useConfirm } from "@/components/Dialog";

export const identitiesQueryKey = ["auth", "identities"];

/** Single sign-on and directory accounts linked to the signed-in user. */
export function useIdentities() {
  return useQuery({ queryKey: identitiesQueryKey, queryFn: async () => unwrap(await api.GET("/auth/identities")) });
}

export function LinkedAccountsCard() {
  const confirmDialog = useConfirm();
  const { t } = useTranslation();
  const qc = useQueryClient();
  const fmt = useDateTime();
  const ids = useIdentities();
  const { data: methods } = useAuthProviders();
  // Back from the provider: /settings/account?linked=<id> or ?sso_error=<code>.
  const [linked] = useState(() => takeQueryParam("linked"));
  const [ssoError] = useState(() => takeQueryParam("sso_error"));
  const link = useMutation({
    mutationFn: async (providerId: string) =>
      unwrap(await api.POST("/auth/oidc/{providerId}/link", { params: { path: { providerId } } })),
    onSuccess: (res) => window.location.assign(res.authorizationUrl),
  });
  const unlink = useMutation({
    mutationFn: async (identityId: string) =>
      unwrap(await api.DELETE("/auth/identities/{identityId}", { params: { path: { identityId } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: identitiesQueryKey }),
  });
  const providers = methods?.oidc ?? [];
  const list = ids.data?.identities ?? [];
  if (providers.length === 0 && list.length === 0 && !ssoError) return null;
  const linkedIds = new Set(list.filter((i) => i.kind === "oidc").map((i) => i.providerId));

  return (
    <Card title={t("sso.linkedTitle")}>
      <p className="mb-4 text-sm text-muted">{t("sso.linkedIntro")}</p>
      {linked && <SuccessText>{t("sso.linkedOk")}</SuccessText>}
      {ssoError && <p role="alert" className="text-sm text-danger">{t(ssoErrorKey(ssoError))}</p>}
      {list.length === 0 && <p className="text-sm text-faint">{t("sso.noLinked")}</p>}
      <ul className="flex flex-col divide-y divide-border">
        {list.map((i) => (
          <li key={i.id} className="flex flex-wrap items-center gap-3 py-3">
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-2 font-medium">{i.providerLabel} <Badge>{i.kind === "ldap" ? "LDAP" : "OpenID"}</Badge></p>
              <p className="truncate text-[13px] text-text/55">{i.label}{i.lastLoginAt ? ` · ${t("account.lastSignIn", { date: fmt(i.lastLoginAt) })}` : ""}</p>
            </div>
            <Button variant="danger" disabled={unlink.isPending}
              onClick={() => void confirmDialog({ message: t("sso.confirmUnlink", { name: i.providerLabel }), danger: true }).then((ok) => ok && unlink.mutate(i.id))}>{t("sso.unlink")}</Button>
          </li>
        ))}
      </ul>
      {providers.some((p) => !linkedIds.has(p.id)) && (
        <div className="mt-4 flex flex-wrap gap-2">
          {providers.filter((p) => !linkedIds.has(p.id)).map((p) => (
            <Button key={p.id} variant="secondary" disabled={link.isPending} onClick={() => link.mutate(p.id)}>
              {t("sso.linkWith", { name: p.label })}
            </Button>
          ))}
        </div>
      )}
      <ErrorText error={ids.error ?? link.error ?? unlink.error} />
    </Card>
  );
}
