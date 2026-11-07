import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Card } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";
import { ContinueShelf, LibraryShelf } from "@/features/library/LibraryPages";

export function HomePage() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-3xl font-semibold">{t("home.welcome", { name: me?.username })}</h1>
      <ContinueShelf />
      <LibraryShelf />
      <Card title={t("home.settingsTitle")}>
        <p className="mb-3 text-sm text-muted">{t("home.settingsText")}</p>
        <Link to="/settings" className="text-sm font-medium text-accent">{t("home.openSettings")} →</Link>
      </Card>
    </div>
  );
}
