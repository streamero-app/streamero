import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Card } from "@/components/ui";

function Field({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="text-muted">{label}</dt>
      <dd><code className="rounded bg-bg px-2 py-0.5 select-all">{value}</code></dd>
    </>
  );
}

/** Ready-to-copy connection values for Seerr/Jellyseerr and the *arr apps. */
export function IntegrationsSettings() {
  const { t } = useTranslation();
  const https = window.location.protocol === "https:";
  const host = window.location.hostname;
  const port = window.location.port || (https ? "443" : "80");
  const common = (
    <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
      <Field label={t("integrations.host")} value={host} />
      <Field label={t("integrations.port")} value={port} />
      <Field label={t("integrations.ssl")} value={https ? t("integrations.on") : t("integrations.off")} />
      <Field label={t("integrations.urlBase")} value="/compat" />
    </dl>
  );
  return (
    <div className="flex flex-col gap-6" data-testid="integrations">
      <Card title="Seerr / Jellyseerr">
        <p className="mb-3 text-sm text-muted">{t("integrations.seerrIntro")}</p>
        {common}
        <p className="mt-3 text-sm text-muted">{t("integrations.seerrAccount")}</p>
      </Card>
      <Card title="Radarr / Sonarr / Lidarr">
        <p className="mb-3 text-sm text-muted">{t("integrations.arrIntro")}</p>
        {common}
        <p className="mt-3 text-sm text-muted">
          {t("integrations.arrKey")} <Link to="/settings/apikeys" className="text-accent">{t("settings.apiKeys")} →</Link>
        </p>
      </Card>
      <Card title="Bazarr">
        <p className="text-sm text-muted">{t("integrations.bazarr")}</p>
      </Card>
    </div>
  );
}
