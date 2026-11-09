import type { ReactNode } from "react";

/** Standard page padding below the top bar. */
export function Page({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`px-5 pt-28 pb-6 sm:px-10 lg:px-16 ${className}`}>{children}</div>;
}
