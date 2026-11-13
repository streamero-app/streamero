import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button, TextField } from "./ui";

type ConfirmOptions = { message: string; title?: string; confirmLabel?: string; danger?: boolean };
type PromptOptions = { message: string; label?: string; initial?: string; confirmLabel?: string };
type Request =
  | ({ kind: "confirm"; resolve: (ok: boolean) => void } & ConfirmOptions)
  | ({ kind: "prompt"; resolve: (value: string | null) => void } & PromptOptions);

type Ctx = { confirm: (o: ConfirmOptions) => Promise<boolean>; prompt: (o: PromptOptions) => Promise<string | null> };
const DialogContext = createContext<Ctx | null>(null);

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Themed replacements for window.confirm / window.prompt: one modal at a time,
 * focus kept inside it, Esc cancels, focus returns to where it was.
 */
export function DialogProvider({ children }: { children: ReactNode }) {
  const [req, setReq] = useState<Request | null>(null);
  const current = useRef<Request | null>(null);
  current.current = req;

  const settle = useCallback((value: boolean | string | null) => {
    const r = current.current;
    if (!r) return;
    current.current = null;
    setReq(null);
    if (r.kind === "confirm") r.resolve(value === true);
    else r.resolve(typeof value === "string" ? value : null);
  }, []);

  const confirm = useCallback((o: ConfirmOptions) => new Promise<boolean>((resolve) => {
    settle(null); // a newer question replaces an unanswered one
    setReq({ kind: "confirm", resolve, ...o });
  }), [settle]);
  const prompt = useCallback((o: PromptOptions) => new Promise<string | null>((resolve) => {
    settle(null);
    setReq({ kind: "prompt", resolve, ...o });
  }), [settle]);

  return (
    <DialogContext.Provider value={{ confirm, prompt }}>
      {children}
      {req && <DialogView req={req} onDone={settle} />}
    </DialogContext.Provider>
  );
}

function DialogView({ req, onDone }: { req: Request; onDone: (value: boolean | string | null) => void }) {
  const { t } = useTranslation();
  const panel = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const messageId = useId();
  const [value, setValue] = useState(req.kind === "prompt" ? req.initial ?? "" : "");

  // Focus the safe choice (Cancel) or the text field; give focus back afterwards.
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const first = req.kind === "prompt" ? panel.current?.querySelector<HTMLElement>("input") : panel.current?.querySelector<HTMLElement>("[data-cancel]");
    first?.focus();
    return () => { if (before?.isConnected) before.focus(); };
  }, [req]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    // Keys stay in the dialog (the player and TV navigation listen on window).
    e.stopPropagation();
    if (e.key === "Escape") {
      e.preventDefault();
      onDone(null);
    } else if (e.key === "Tab") {
      const items = [...(panel.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])];
      if (!items.length) return;
      const first = items[0]!;
      const last = items[items.length - 1]!;
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
  };
  const submit = () => {
    if (req.kind === "confirm") onDone(true);
    else if (value.trim()) onDone(value.trim());
  };
  const title = req.kind === "confirm" ? req.title : undefined;

  return (
    <div className="fixed inset-0 z-[100] grid place-items-center bg-black/70 p-4 backdrop-blur-sm [animation:fade_160ms_ease-out_both]"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onDone(null); }}>
      <div ref={panel} role={req.kind === "confirm" ? "alertdialog" : "dialog"} aria-modal="true"
        aria-labelledby={title ? titleId : messageId} aria-describedby={title ? messageId : undefined} onKeyDown={onKeyDown}
        className="w-[min(92vw,440px)] rounded-card border border-border bg-surface p-6 shadow-2xl [animation:rise_220ms_var(--ease-out)_both]"
        data-testid="dialog">
        <form onSubmit={(e) => { e.preventDefault(); submit(); }} className="flex flex-col gap-5">
          {title && <h2 id={titleId} className="font-head text-[17px]">{title}</h2>}
          {req.kind === "confirm"
            ? <p id={messageId} className="text-[15px] leading-relaxed text-text/85">{req.message}</p>
            : (
              <div id={messageId}>
                <TextField label={req.label ?? req.message} value={value} onChange={(e) => setValue(e.target.value)} maxLength={200} />
              </div>
            )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" data-cancel onClick={() => onDone(null)}>{t("common.cancel")}</Button>
            <Button type="submit" variant={req.kind === "confirm" && req.danger ? "danger" : "primary"}
              disabled={req.kind === "prompt" && !value.trim()}>
              {req.confirmLabel ?? (req.kind === "confirm" ? t("dialog.confirm") : t("dialog.ok"))}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

function useDialogs(): Ctx {
  const ctx = useContext(DialogContext);
  if (!ctx) throw new Error("useConfirm/usePrompt outside DialogProvider");
  return ctx;
}

/** `await confirm({ message, danger })` → true when the viewer confirmed. */
export function useConfirm() {
  return useDialogs().confirm;
}

/** `await prompt({ message })` → the trimmed text, or null when cancelled. */
export function usePrompt() {
  return useDialogs().prompt;
}
