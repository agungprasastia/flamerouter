"use client";

import { TriangleAlert } from "lucide-react";

export interface SecurityWarningAction {
  href: string;
  label: string;
}

export interface SecurityWarningProps {
  message: string;
  action?: SecurityWarningAction;
}

/** Security warning banner with optional action link */
export default function SecurityWarning({ message, action }: SecurityWarningProps) {
  return (
    <div className="flex items-start gap-2 rounded-[6px_1px_6px_1px] border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-amber-700 dark:text-amber-400">
      <TriangleAlert
        size={16}
        strokeWidth={1.75}
        aria-hidden="true"
        className="mt-0.5 shrink-0"
      />
      <p className="flex-1 text-xs">{message}</p>
      {action && (
        <a
          href={action.href}
          className="shrink-0 text-xs font-medium underline hover:opacity-80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-amber-500/50"
          onClick={
            action.href.startsWith("#")
              ? (e) => {
                  e.preventDefault();
                  document
                    .getElementById(action.href.slice(1))
                    ?.scrollIntoView({ behavior: "smooth" });
                }
              : undefined
          }
        >
          {action.label}
        </a>
      )}
    </div>
  );
}
