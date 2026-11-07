import { useTranslation } from "react-i18next";
import { useMe } from "@/features/auth/useAuth";
import { ContinueShelf, FavoritesShelf, HomeHero, LatestShelf, LibraryShelf } from "@/features/library/LibraryPages";

export function HomePage() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const name = me?.currentProfile?.name ?? me?.username;
  return (
    <div className="flex flex-col gap-10 pb-6">
      <HomeHero />
      <h1 className="enter px-4 pt-20 font-display text-3xl text-text/90 first:pt-20 sm:px-6 lg:px-10 [section+&]:pt-0">
        {t("home.welcome", { name })}
      </h1>
      <ContinueShelf />
      <LatestShelf />
      <FavoritesShelf />
      <LibraryShelf />
    </div>
  );
}
