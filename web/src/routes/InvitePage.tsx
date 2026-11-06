import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, deviceInfo, unwrap, type User } from "@/api/client";
import { Button, ErrorText, TextField } from "@/components/ui";
import { AuthFrame } from "@/features/auth/AuthFrame";
import { meQueryKey } from "@/features/auth/useAuth";

/** Public page behind an invite link: create an account and sign in. */
export function InvitePage() {
  const { t } = useTranslation();
  const { token } = useParams({ from: "/invite/$token" });
  const qc = useQueryClient();
  const navigate = useNavigate();
  const invite = useQuery({
    queryKey: ["invite", token],
    retry: false,
    queryFn: async () => unwrap(await api.GET("/invite/{token}", { params: { path: { token } } })),
  });
  const [form, setForm] = useState({ username: "", password: "", confirm: "" });
  const [mismatch, setMismatch] = useState(false);
  const accept = useMutation({
    mutationFn: async () => unwrap(await api.POST("/invite/{token}", {
      params: { path: { token } },
      body: { username: form.username, password: form.password, useCookie: true, device: deviceInfo() },
    })),
    onSuccess: (res) => {
      qc.setQueryData<User | null>(meQueryKey, res.user);
      void navigate({ to: "/" });
    },
  });
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setMismatch(form.password !== form.confirm);
    if (form.password === form.confirm) accept.mutate();
  };

  if (invite.isError) {
    return (
      <AuthFrame title={t("invites.invalidTitle")} intro={t("invites.invalidText")}>
        <Link to="/login" className="text-sm text-accent">{t("login.title")} →</Link>
      </AuthFrame>
    );
  }
  return (
    <AuthFrame title={t("invites.joinTitle", { server: invite.data?.serverName ?? "" })} intro={t("invites.joinIntro")}>
      <form className="flex flex-col gap-4" onSubmit={onSubmit}>
        <TextField label={t("login.username")} autoComplete="username" required maxLength={64} autoFocus
          value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
        <TextField label={t("login.password")} type="password" autoComplete="new-password" required minLength={8}
          value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} hint={t("users.passwordHint")} />
        <TextField label={t("setup.confirmPassword")} type="password" autoComplete="new-password" required minLength={8}
          value={form.confirm} onChange={(e) => setForm({ ...form, confirm: e.target.value })} />
        {mismatch && <p role="alert" className="text-sm text-danger">{t("errors.password_mismatch")}</p>}
        <ErrorText error={accept.error} />
        <Button type="submit" disabled={accept.isPending || !invite.data}>{t("invites.join")}</Button>
      </form>
    </AuthFrame>
  );
}
