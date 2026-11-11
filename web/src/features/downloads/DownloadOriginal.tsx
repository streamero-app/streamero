import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Download } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Button, ErrorText } from "@/components/ui";
import { useMe } from "@/features/auth/useAuth";

/** Whether the signed-in account may download original files (D-040). */
export function useCanDownload(): boolean {
  const { data: me } = useMe();
  return !!me && (me.isAdmin || me.permissions.download);
}

/**
 * "Download original" for one file of a movie or episode. The server checks the
 * permission, library access and the profile's parental limit, then hands out a
 * short-lived signed link that the browser saves as an attachment.
 */
export function DownloadOriginalButton({ itemId, fileId }: { itemId: string; fileId?: string }) {
  const { t } = useTranslation();
  const allowed = useCanDownload();
  const link = useMutation({
    mutationFn: async () => unwrap(await api.POST("/items/{itemId}/original", { params: { path: { itemId } }, body: { fileId } })),
    onSuccess: (l) => {
      const a = document.createElement("a");
      a.href = l.url;
      a.download = l.fileName;
      a.rel = "noopener";
      document.body.appendChild(a);
      a.click();
      a.remove();
    },
  });
  if (!allowed) return null;
  return (
    <span className="inline-flex flex-col gap-1">
      <Button variant="secondary" disabled={link.isPending} onClick={() => link.mutate()} data-testid="download-original"
        title={t("downloads.originalHint")}>
        <Download className="size-4" aria-hidden />{t("downloads.original")}
      </Button>
      <ErrorText error={link.error} />
    </span>
  );
}
