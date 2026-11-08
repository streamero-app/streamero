import { useId, useState, type ButtonHTMLAttributes, type ImgHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/api/client";

type Variant = "primary" | "secondary" | "danger" | "ghost" | "play" | "muted";
type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: "md" | "lg" };

/** Shared look of every pressable thing: steps on hover, presses on click. */
export const pressable =
  "inline-flex items-center justify-center gap-2 font-medium select-none transition-[background-color,color,border-color,transform,opacity] duration-150 ease-out active:scale-[0.97] disabled:pointer-events-none disabled:opacity-45";

export const buttonVariants: Record<Variant, string> = {
  primary: "bg-accent text-accent-contrast hover:bg-[#ffb85f] shadow-[0_8px_24px_-12px_rgb(255_170_69/0.6)]",
  secondary: "bg-white/[0.07] text-text ring-1 ring-inset ring-border hover:bg-white/[0.11]",
  danger: "bg-transparent text-danger ring-1 ring-inset ring-danger/40 hover:bg-danger/10",
  ghost: "bg-transparent text-muted hover:bg-white/[0.06] hover:text-text",
  play: "bg-white text-black hover:bg-white/80",
  muted: "bg-white/20 text-white backdrop-blur-md hover:bg-white/30",
};

const sizes = { md: "h-10 rounded-[10px] px-4 text-sm", lg: "h-12 rounded-xl px-6 text-[15px]" };

export function Button({ variant = "primary", size = "md", className = "", ...props }: ButtonProps) {
  return <button {...props} className={`${pressable} ${sizes[size]} ${buttonVariants[variant]} ${className}`} />;
}

/** Round icon-only button; the label is required for screen readers. */
export function IconButton({ label, className = "", active = false, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; active?: boolean }) {
  return (
    <button {...props} aria-label={label} title={label}
      className={`${pressable} size-10 rounded-full ring-1 ring-inset ${active ? "bg-accent/15 text-accent ring-accent/40" : "bg-white/[0.07] text-text ring-border hover:bg-white/[0.12]"} ${className}`} />
  );
}

const fieldClass =
  "h-10 rounded-[10px] border border-border bg-black/30 px-3 text-[15px] text-text placeholder:text-faint transition-colors duration-150 hover:border-border-strong focus:border-accent/70 focus:bg-black/40 focus-visible:outline-none";

export function TextField({ label, hint, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: ReactNode }) {
  const id = useId();
  // The hint is outside the <label> so it is announced as a description, not as part of the name.
  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <label htmlFor={id} className="font-medium text-text/90">{label}</label>
      <input id={id} aria-describedby={hint ? `${id}-hint` : undefined} {...props} className={fieldClass} />
      {hint && <span id={`${id}-hint`} className="text-xs text-muted">{hint}</span>}
    </div>
  );
}

/** Native select in the field style (keeps the platform picker on phones). */
export function Select({ className = "", children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select {...props}
      className={`h-9 cursor-pointer appearance-none rounded-[10px] border border-border bg-white/[0.05] bg-[length:14px] bg-[right_10px_center] bg-no-repeat pr-8 pl-3 text-sm text-text transition-colors hover:border-border-strong focus:border-accent/70 ${className}`}
      style={{ backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23a89f93' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E")` }}>
      {children}
    </select>
  );
}

export function Card({ title, children, className = "", actions }: { title?: ReactNode; children: ReactNode; className?: string; actions?: ReactNode }) {
  return (
    <section className={`rounded-card border border-border bg-surface/70 p-5 sm:p-6 ${className}`}>
      {(title || actions) && (
        <div className="mb-4 flex items-center gap-3">
          {title && <h2 className="text-[17px] font-semibold tracking-tight">{title}</h2>}
          {actions && <div className="ml-auto">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

/** Shows a translated message for an API error (falls back to a generic one). */
export function ErrorText({ error }: { error: unknown }) {
  const { t } = useTranslation();
  if (!error) return null;
  const code = error instanceof ApiError ? error.code : "generic";
  return (
    <p role="alert" className="text-sm text-danger">
      {t(`errors.${code}`, { defaultValue: t("errors.generic") })}
    </p>
  );
}

export function SuccessText({ children }: { children: ReactNode }) {
  return (
    <p role="status" className="text-sm text-success">
      {children}
    </p>
  );
}

export function Badge({ children, tone = "muted" }: { children: ReactNode; tone?: "muted" | "accent" | "danger" | "success" }) {
  const tones = {
    muted: "bg-white/[0.07] text-text/80 ring-border",
    accent: "bg-accent/12 text-accent ring-accent/30",
    danger: "bg-danger/12 text-danger ring-danger/30",
    success: "bg-success/12 text-success ring-success/30",
  };
  return <span className={`inline-flex h-6 items-center gap-1 rounded-md px-2 text-xs font-medium tabular ring-1 ring-inset ${tones[tone]}`}>{children}</span>;
}

/** Artwork that fades in when decoded and reports failures so callers can fall back. */
export function Art({ className = "", onError, ...props }: ImgHTMLAttributes<HTMLImageElement>) {
  const [loaded, setLoaded] = useState(false);
  return (
    <img alt="" loading="lazy" decoding="async" draggable={false} {...props} data-loaded={loaded}
      onLoad={() => setLoaded(true)} onError={onError} className={`art ${className}`} />
  );
}

export function Spinner({ className = "" }: { className?: string }) {
  return <span aria-hidden className={`inline-block size-5 animate-spin rounded-full border-2 border-white/15 border-t-accent ${className}`} />;
}
