"use client";

import React, { ReactNode } from "react";
import { cn } from "@/shared/utils/cn";

export interface ToggleProps {
  checked?: boolean;
  onChange?: (checked: boolean) => void;
  label?: ReactNode | string;
  description?: ReactNode | string;
  disabled?: boolean;
  size?: "sm" | "md" | "lg" | string;
  className?: string;
  "aria-label"?: string;
  title?: string;
}

export default function Toggle({
  checked = false,
  onChange,
  label,
  description,
  disabled = false,
  size = "md",
  className,
  "aria-label": ariaLabel,
}: ToggleProps) {
  const sizes: Record<string, { track: string; thumb: string; translate: string }> = {
    sm: { track: "w-8 h-4", thumb: "size-3", translate: "translate-x-4" },
    md: { track: "w-11 h-6", thumb: "size-5", translate: "translate-x-5" },
    lg: { track: "w-14 h-7", thumb: "size-6", translate: "translate-x-7" },
  };

  const currentSize = sizes[size] || sizes.md;

  const handleClick = () => {
    if (!disabled && onChange) onChange(!checked);
  };

  return (
    <div
      className={cn(
        "flex items-center gap-3",
        disabled && "opacity-50 cursor-not-allowed",
        className,
      )}
    >
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={handleClick}
        className={cn(
          "inline-flex min-h-10 min-w-10 shrink-0 cursor-pointer items-center justify-center rounded-[4px]",
          "focus:outline-none focus:ring-2 focus:ring-brand-500/30",
          disabled && "cursor-not-allowed",
        )}
      >
        <span
          className={cn(
            "pointer-events-none relative inline-flex rounded-[4px] transition-colors duration-200 ease-in-out",
            checked ? "bg-brand-500" : "bg-surface-3",
            currentSize.track,
          )}
        >
          <span
            className={cn(
              "inline-block rounded-[3px] bg-white shadow-sm",
              "transform transition duration-200 ease-in-out",
              checked ? currentSize.translate : "translate-x-0.5",
              currentSize.thumb,
              "mt-0.5",
            )}
          />
        </span>
      </button>
      {(label || description) && (
        <div className="flex flex-col">
          {label && (
            <span className="text-sm font-medium text-text-main">{label}</span>
          )}
          {description && (
            <span className="text-xs text-text-muted">{description}</span>
          )}
        </div>
      )}
    </div>
  );
}
