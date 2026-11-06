import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, ApiError, deviceInfo, unwrap, type User } from "@/api/client";
import { Button, ErrorText } from "@/components/ui";
import { meQueryKey } from "./useAuth";

const POLL_MS = 3000;

/** Shows a Quick Connect code and signs in once another device approves it. */
export function QuickConnectLogin({ onCancel }: { onCancel: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const start = useMutation({
    mutationFn: async () => unwrap(await api.POST("/quickconnect/initiate", { body: deviceInfo() })),
  });
  useEffect(() => {
    start.mutate(); // once on mount
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const secret = start.data?.secret;
  const poll = useQuery({
    queryKey: ["quickconnect", secret],
    enabled: !!secret,
    refetchInterval: (q) => (q.state.data || q.state.error ? false : POLL_MS),
    retry: false,
    queryFn: async () => {
      const res = await api.POST("/quickconnect/poll", { body: { secret: secret!, useCookie: true } });
      if (res.response.status === 202) return null; // not approved yet
      return unwrap(res);
    },
  });

  useEffect(() => {
    const user = poll.data?.user;
    if (user) {
      qc.setQueryData<User | null>(meQueryKey, user);
      void navigate({ to: "/" });
    }
  }, [poll.data, qc, navigate]);

  const expired = poll.error instanceof ApiError && poll.error.code === "not_found";
  return (
    <div className="flex flex-col items-center gap-4 text-center" data-testid="quick-connect">
      <p className="text-sm text-muted">{t("quickconnect.loginIntro")}</p>
      {start.data && !expired && (
        <p className="font-mono text-4xl font-semibold tracking-[0.3em]" aria-label={t("quickconnect.code")}>
          {start.data.code}
        </p>
      )}
      {expired ? (
        <>
          <p role="alert" className="text-sm text-danger">{t("quickconnect.expired")}</p>
          <Button onClick={() => start.mutate()}>{t("quickconnect.newCode")}</Button>
        </>
      ) : (
        <p role="status" className="text-xs text-muted">{t("quickconnect.waiting")}</p>
      )}
      <ErrorText error={start.error ?? (expired ? null : poll.error)} />
      <Button variant="ghost" onClick={onCancel}>{t("quickconnect.usePassword")}</Button>
    </div>
  );
}
