import type { ReactNode } from "react";

/** Standard page padding below the floating top bar. */
export function Page({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`px-4 pt-24 pb-6 sm:px-8 lg:px-14 ${className}`}>{children}</div>;
}
