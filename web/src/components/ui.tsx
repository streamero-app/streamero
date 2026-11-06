import { useId, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/api/client";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "danger" | "ghost" };

const variants = {
  primary: "bg-accent text-accent-contrast hover:opacity-90",
  secondary: "bg-surface-2 text-text hover:bg-border",
  danger: "bg-transparent text-danger border border-danger/50 hover:bg-danger/10",
  ghost: "bg-transparent text-muted hover:text-text",
};

export function Button({ variant = "primary", className = "", ...props }: ButtonProps) {
  return (
    <button
      {...props}
      className={`rounded-md px-4 py-2 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50 ${variants[variant]} ${className}`}
    />
  );
}

export function TextField({ label, hint, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: ReactNode }) {
  const id = useId();
  // The hint is outside the <label> so it is announced as a description, not as part of the name.
  return (
    <div className="flex flex-col gap-1 text-sm">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        aria-describedby={hint ? `${id}-hint` : undefined}
        {...props}
        className="rounded-md border border-border bg-bg px-3 py-2 text-base text-text outline-none focus:border-accent"
      />
      {hint && <span id={`${id}-hint`} className="text-xs text-muted">{hint}</span>}
    </div>
  );
}

export function Card({ title, children, className = "" }: { title?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`rounded-card border border-border bg-surface p-5 sm:p-6 ${className}`}>
      {title && <h2 className="mb-4 text-lg font-semibold">{title}</h2>}
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
    muted: "bg-surface-2 text-muted",
    accent: "bg-accent/15 text-accent",
    danger: "bg-danger/15 text-danger",
    success: "bg-success/15 text-success",
  };
  return <span className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${tones[tone]}`}>{children}</span>;
}
