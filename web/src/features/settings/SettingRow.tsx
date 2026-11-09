import { useId, type ReactNode } from "react";

/** One setting: what it is and what it does on the left, the control on the right. */
export function SettingRow({ label, hint, children }: { label: string; hint?: ReactNode; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="grid gap-x-8 gap-y-2 border-t border-border py-4 first:border-t-0 first:pt-0 sm:grid-cols-[minmax(0,1fr)_minmax(0,18rem)] sm:items-center">
      <div className="min-w-0">
        <label htmlFor={id} className="text-[15px] font-medium text-text">{label}</label>
        {hint && <p className="mt-0.5 text-[13px] leading-snug text-text/55">{hint}</p>}
      </div>
      <div className="min-w-0">{children(id)}</div>
    </div>
  );
}

/** An on/off switch with the semantics of a checkbox. */
export function Toggle({ id, checked, onChange, label }: { id?: string; checked: boolean; onChange: (v: boolean) => void; label?: string }) {
  return (
    <button id={id} type="button" role="switch" aria-checked={checked} aria-label={label} onClick={() => onChange(!checked)}
      className={`relative inline-flex h-7 w-12 shrink-0 items-center rounded-full transition-colors duration-200 ${checked ? "bg-text" : "bg-white/15"}`}>
      <span className={`inline-block size-5 rounded-full shadow transition-transform duration-200 ${checked ? "translate-x-6 bg-bg" : "translate-x-1 bg-text/80"}`} />
    </button>
  );
}
