import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4">
      <h1 className="text-2xl font-semibold">{t("notFound.title")}</h1>
      <Link to="/" className="text-accent">{t("notFound.back")}</Link>
    </div>
  );
}
