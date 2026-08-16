"use client";

import { useTheme } from "@/shared/hooks/useTheme";
import { cn } from "@/shared/utils/cn";
import { Moon, Sun } from "lucide-react";

export interface ThemeToggleProps {
  className?: string;
  variant?: "default" | "card" | string;
}

export default function ThemeToggle({ className, variant = "default" }: ThemeToggleProps) {
  const { isDark, toggleTheme } = useTheme();
  const Icon = isDark ? Sun : Moon;

  const variants: Record<string, string> = {
    default: cn(
      "flex items-center justify-center size-9 rounded-[6px]",
      "text-text-muted hover:text-text-main",
      "hover:bg-surface-2 transition-colors",
    ),
    card: cn(
      "flex items-center justify-center size-11 rounded-full",
      "bg-surface/60 hover:bg-surface",
      "border border-border",
      "backdrop-blur-md shadow-sm hover:shadow-[var(--shadow-warm)]",
      "text-text-muted hover:text-brand-500",
      "transition-all group",
    ),
  };

  return (
    <button
      onClick={toggleTheme}
      className={cn(
        variants[variant] || variants.default,
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/35",
        className,
      )}
      aria-label={`Switch to ${isDark ? "light" : "dark"} mode`}
      title={`Switch to ${isDark ? "light" : "dark"} mode`}
    >
      <Icon
        size={20}
        strokeWidth={1.75}
        className={cn(
          variant === "card" &&
            "transition-transform duration-300 group-hover:rotate-12",
        )}
        aria-hidden="true"
      />
    </button>
  );
}
