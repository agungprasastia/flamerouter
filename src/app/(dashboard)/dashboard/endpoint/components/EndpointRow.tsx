"use client";

import type { ReactNode } from "react";
import { Check, Copy } from "lucide-react";

export interface EndpointRowProps {
  label: string;
  url: string;
  copyId: string;
  copied: string | null;
  onCopy: (text: string, id: string) => void;
  badge?: ReactNode;
  actions?: ReactNode;
}

/** Reusable endpoint row component */
export default function EndpointRow({
  label,
  url,
  copyId,
  copied,
  onCopy,
  badge,
  actions,
}: EndpointRowProps) {
  const isCopied = copied === copyId;

  return (
    <div className="grid min-w-0 grid-cols-1 gap-2 py-4 sm:grid-cols-[7rem_minmax(0,1fr)_auto] sm:items-center">
      <span className="font-mono text-xs text-text-muted">
        {label}
      </span>
      <input
        aria-label={`${label} endpoint`}
        value={url}
        readOnly
        className="min-w-0 w-full border-0 bg-transparent p-0 font-mono text-sm text-text-main outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      />
      <button
        onClick={() => onCopy(url, copyId)}
        aria-label={`Copy ${label}`}
        className="inline-flex min-h-10 items-center justify-center gap-2 rounded px-3 text-xs font-medium text-text-muted transition-colors hover:bg-black/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:hover:bg-white/5 sm:min-h-0 sm:justify-self-end sm:py-2"
      >
        {isCopied ? (
          <Check size={16} strokeWidth={1.75} aria-hidden="true" />
        ) : (
          <Copy size={16} strokeWidth={1.75} aria-hidden="true" />
        )}
        <span>{isCopied ? "Copied" : "Copy"}</span>
      </button>
      {actions ? (
        <div className="flex justify-end sm:col-start-2 sm:col-span-2">
          {actions}
        </div>
      ) : null}
    </div>
  );
}
