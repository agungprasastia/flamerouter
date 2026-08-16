"use client";

import React, { ReactNode, ButtonHTMLAttributes } from "react";
import { cn } from "@/shared/utils/cn";

const variants = {
  primary:
    "bg-brand-600 hover:bg-brand-700 dark:bg-brand-700 dark:hover:bg-brand-600 text-white disabled:bg-surface-3 disabled:text-text-muted",
  secondary:
    "bg-surface-2 hover:bg-surface-3 text-text-main border border-border disabled:opacity-50",
  outline:
    "border border-border text-text-main hover:bg-surface-2 hover:border-brand-500/40",
  ghost: "text-text-muted hover:bg-surface-2 hover:text-text-main",
  danger:
    "bg-red-500 hover:bg-red-600 text-white shadow-sm disabled:bg-surface-3 disabled:text-text-muted",
  success:
    "bg-green-600 hover:bg-green-700 text-white shadow-sm disabled:bg-surface-3 disabled:text-text-muted",
};

const sizes = {
  sm: "h-7 px-3 text-xs rounded-[8px]",
  md: "h-9 px-4 text-sm rounded-[10px]",
  lg: "h-11 px-6 text-sm rounded-[10px]",
};

const renderIcon = (value: ReactNode | string) =>
  typeof value === "string" ? (
    <span className="material-symbols-outlined text-[18px]">{value}</span>
  ) : (
    value
  );

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  children?: ReactNode;
  variant?: keyof typeof variants | string;
  size?: keyof typeof sizes | string;
  icon?: ReactNode | string;
  iconRight?: ReactNode | string;
  disabled?: boolean;
  loading?: boolean;
  fullWidth?: boolean;
  className?: string;
}

export default function Button({
  children,
  variant = "primary",
  size = "md",
  icon,
  iconRight,
  disabled = false,
  loading = false,
  fullWidth = false,
  className,
  ...props
}: ButtonProps) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-2 font-semibold transition-all duration-150 ease-out cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/35 focus-visible:ring-offset-2 focus-visible:ring-offset-bg",
        "active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed disabled:active:scale-100",
        variants[variant as keyof typeof variants] || variants.primary,
        sizes[size as keyof typeof sizes] || sizes.md,
        fullWidth && "w-full",
        className,
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? (
        <span className="material-symbols-outlined animate-spin text-[18px]">
          progress_activity
        </span>
      ) : icon ? (
        renderIcon(icon)
      ) : null}
      {children}
      {iconRight && !loading ? renderIcon(iconRight) : null}
    </button>
  );
}
